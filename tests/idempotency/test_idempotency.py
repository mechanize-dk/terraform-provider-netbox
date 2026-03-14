#!/usr/bin/env python3
"""
Idempotency integration tests for mechanize-dk/terraform-provider-netbox.

For each supported resource type, this script verifies:
  1. Normal CRUD: create → update → destroy via Terraform works correctly.
  2. Idempotency:  create the same object directly in NetBox via the REST API,
                  then run Terraform apply — the provider should adopt the
                  existing object into state rather than failing.
                  After adoption: update and destroy must also succeed.

Usage:
    python3 test_idempotency.py \\
        --netbox-url      http://localhost:8000 \\
        --netbox-username admin \\
        --netbox-password admin \\
        --terraform-path  /usr/local/bin/terraform \\
        [--provider-dir   /path/to/dir/with/terraform-provider-netbox] \\
        [--filter         netbox_tag] \\
        [--verbose] \\
        [--stop-on-failure]

Prerequisites:
    pip install requests

    Build the provider binary first:
        go build -o terraform-provider-netbox .
    Then either pass --provider-dir pointing to that directory,
    or run this script from the project root (auto-detected).
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import random
import string
import traceback
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Tuple

try:
    import requests
    from requests.packages.urllib3.exceptions import InsecureRequestWarning
    requests.packages.urllib3.disable_warnings(InsecureRequestWarning)
except ImportError:
    print("ERROR: 'requests' library is required. Install with:  pip install requests")
    sys.exit(1)


# ─── Terminal colours ────────────────────────────────────────────────────────

GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
CYAN   = "\033[36m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

def _ok(msg):     return f"  {GREEN}✓{RESET} {msg}"
def _fail(msg):   return f"  {RED}✗{RESET} {msg}"
def _skip(msg):   return f"  {YELLOW}○{RESET} {msg}"
def _info(msg):   return f"  {CYAN}·{RESET} {msg}"
def _bold(msg):   return f"{BOLD}{msg}{RESET}"
def _section(msg): print(f"\n{BOLD}{CYAN}{msg}{RESET}")
def _banner(msg):  print(f"\n{BOLD}{'='*60}\n{msg}\n{'='*60}{RESET}")


# ─── NetBox REST client ───────────────────────────────────────────────────────

class NetBoxClient:
    """Minimal NetBox REST API client using basic auth to acquire a token."""

    def __init__(self, base_url: str, username: str, password: str):
        self.base_url = base_url.rstrip("/")
        self._session = requests.Session()
        self._session.auth = (username, password)
        self._session.verify = False  # allow self-signed certificates
        self._session.headers.update({
            "Content-Type": "application/json",
            "Accept":       "application/json",
        })
        self.token:    Optional[str] = None
        self._token_id: Optional[int] = None

    # ── auth ──────────────────────────────────────────────────────────────────

    def connect(self) -> str:
        """Provision (or retrieve) an API token and return it."""
        username, password = self._session.auth

        # Try the newer "provision" endpoint (credentials go in body)
        r = self._session.post(
            f"{self.base_url}/api/users/tokens/provision/",
            json={"username": username, "password": password},
        )
        if r.status_code == 201:
            data = r.json()
            self.token     = data["key"]
            self._token_id = data["id"]
            self._use_token()
            return self.token

        # Fall back to listing existing tokens for this user (still using basic auth)
        r = self._session.get(f"{self.base_url}/api/users/tokens/")
        if r.ok:
            results = r.json().get("results", [])
            if results:
                self.token     = results[0]["key"]
                self._token_id = results[0]["id"]
                self._use_token()
                return self.token

        raise RuntimeError(
            "Could not get or create a NetBox API token. "
            f"Provision status: {r.status_code}. "
            "Verify the credentials and that the user has token permissions."
        )

    def _use_token(self):
        """Switch the session from basic auth to token auth."""
        self._session.auth = None
        self._session.headers["Authorization"] = f"Token {self.token}"

    def disconnect(self):
        """Delete the token that was created by connect() (if any)."""
        if self._token_id:
            self._session.delete(
                f"{self.base_url}/api/users/tokens/{self._token_id}/"
            )

    # ── CRUD helpers ──────────────────────────────────────────────────────────

    def create(self, path: str, payload: Dict) -> Dict:
        url = f"{self.base_url}/api/{path.strip('/')}/"
        r = self._session.post(url, json=payload)
        if r.status_code not in (200, 201):
            raise RuntimeError(
                f"POST {url} → {r.status_code}\n{r.text[:400]}"
            )
        return r.json()

    def delete(self, path: str, obj_id: int) -> None:
        url = f"{self.base_url}/api/{path.strip('/')}/{obj_id}/"
        r = self._session.delete(url)
        if r.status_code not in (200, 204, 404):
            print(f"    Warning: DELETE {url} → {r.status_code}")


# ─── Terraform runner ─────────────────────────────────────────────────────────

class TerraformRunner:
    """Executes Terraform commands inside isolated working directories."""

    def __init__(self, terraform_bin: str, provider_dir: str):
        self.terraform_bin = terraform_bin
        self.provider_dir  = provider_dir
        self._rc_path:     Optional[str] = None

    def setup_dev_overrides(self, base_tmp_dir: str):
        """Write a terraform.rc that points to the local provider binary."""
        rc = os.path.join(base_tmp_dir, "terraform.rc")
        with open(rc, "w") as f:
            f.write(
                'provider_installation {\n'
                '  dev_overrides {\n'
                f'    "mechanize-dk/netbox" = "{self.provider_dir}"\n'
                '  }\n'
                '  direct {}\n'
                '}\n'
            )
        self._rc_path = rc

    def _env(self) -> Dict[str, str]:
        e = os.environ.copy()
        if self._rc_path:
            e["TF_CLI_CONFIG_FILE"] = self._rc_path
        e["TF_INPUT"]         = "0"
        e["TF_IN_AUTOMATION"] = "1"
        e["NO_COLOR"]         = "1"
        return e

    def _run(self, work_dir: str, *args, timeout: int = 180) -> Tuple[int, str]:
        try:
            r = subprocess.run(
                [self.terraform_bin] + list(args),
                cwd=work_dir,
                env=self._env(),
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                timeout=timeout,
            )
            return r.returncode, r.stdout
        except subprocess.TimeoutExpired:
            return 1, f"ERROR: terraform {args[0]} timed out after {timeout}s"

    def init(self, work_dir: str)    -> Tuple[bool, str]:
        code, out = self._run(work_dir, "init", "-upgrade", "-no-color")
        return code == 0, out

    def apply(self, work_dir: str)   -> Tuple[bool, str]:
        code, out = self._run(work_dir, "apply", "-auto-approve", "-no-color")
        return code == 0, out

    def destroy(self, work_dir: str) -> Tuple[bool, str]:
        code, out = self._run(work_dir, "destroy", "-auto-approve", "-no-color")
        return code == 0, out


# ─── Terraform config helpers ─────────────────────────────────────────────────

def provider_hcl(netbox_url: str, token: str) -> str:
    return f"""\
terraform {{
  required_providers {{
    netbox = {{
      source  = "mechanize-dk/netbox"
    }}
  }}
}}

provider "netbox" {{
  server_url           = "{netbox_url}"
  api_token            = "{token}"
  allow_insecure_https = true
  skip_version_check   = true
}}
"""

def write_tf(work_dir: str, provider: str, resource_hcl: str):
    with open(os.path.join(work_dir, "main.tf"), "w") as f:
        f.write(provider + "\n" + resource_hcl + "\n")


# ─── Prerequisites ────────────────────────────────────────────────────────────

def setup_prereqs(nb: NetBoxClient, run_id: str) -> Tuple[Dict, List]:
    """
    Create the resources that other test cases depend on (RIR, manufacturer,
    cluster type, site, tenant).  Returns (prereq_ids dict, cleanup list).
    """
    prereqs   = {}
    to_delete = []   # list of (api_path, object_id) — deleted in reverse order

    def mk(path: str, payload: Dict, key: str):
        obj = nb.create(path, payload)
        prereqs[key] = obj["id"]
        to_delete.append((path, obj["id"]))
        print(_info(f"Created {key} = {obj['id']}  ({path})"))

    mk("ipam/rirs",
       {"name": f"prereq-rir-{run_id}", "slug": f"prereq-rir-{run_id}"},
       "rir_id")

    mk("dcim/manufacturers",
       {"name": f"prereq-mfr-{run_id}", "slug": f"prereq-mfr-{run_id}"},
       "manufacturer_id")

    mk("virtualization/cluster-types",
       {"name": f"prereq-cltype-{run_id}", "slug": f"prereq-cltype-{run_id}"},
       "cluster_type_id")

    mk("dcim/sites",
       {"name": f"prereq-site-{run_id}", "slug": f"prereq-site-{run_id}",
        "status": "active"},
       "site_id")

    mk("tenancy/tenants",
       {"name": f"prereq-tenant-{run_id}", "slug": f"prereq-tenant-{run_id}"},
       "tenant_id")

    # Derive numeric values that must be unique across test-runs
    base = int(run_id[:2], 16)           # 0 – 255
    prereqs["asn_num"]     = 64512 + base          # 64512 – 64767
    prereqs["asn_num_ext"] = 64512 + 256 + base    # 64768 – 65023  (different range)
    prereqs["vlan_vid"]    = 1000 + (base * 12) % 3000  # 1000 – 3999

    return prereqs, to_delete


def cleanup_prereqs(nb: NetBoxClient, to_delete: List):
    """Delete prerequisites in reverse creation order."""
    for path, obj_id in reversed(to_delete):
        nb.delete(path, obj_id)
        print(_info(f"Deleted prereq  {path}/{obj_id}"))


# ─── Test case definitions ────────────────────────────────────────────────────
#
# Each entry is a dict with:
#   name           Terraform resource type (also the test name)
#   create_tf      HCL string; placeholders: {uid}, {prereq_key}, …
#   update_tf      HCL string; same placeholders
#   api_path       NetBox API path  (e.g. "extras/tags")
#   api_payload    callable(uid: str, prereqs: dict) → dict
#
# Placeholders in create_tf / update_tf are filled via str.format(**ctx)
# where ctx = {"uid": uid, **prereqs}.
# HCL literal braces must be doubled {{ }}.

TEST_CASES: List[Dict] = [

    # ── Extras ──────────────────────────────────────────────────────────────

    {
        "name": "netbox_tag",
        "create_tf": """\
resource "netbox_tag" "test" {{
  name      = "idem-tag-{uid}"
  color_hex = "aa1409"
}}""",
        "update_tf": """\
resource "netbox_tag" "test" {{
  name        = "idem-tag-{uid}"
  color_hex   = "4caf50"
  description = "updated"
}}""",
        "api_path":    "extras/tags",
        "api_payload": lambda uid, p: {
            "name":  f"idem-tag-{uid}",
            "slug":  f"idem-tag-{uid}",
            "color": "aa1409",
        },
    },

    # ── Tenancy ─────────────────────────────────────────────────────────────

    {
        "name": "netbox_tenant_group",
        "create_tf": """\
resource "netbox_tenant_group" "test" {{
  name = "idem-tengrp-{uid}"
}}""",
        "update_tf": """\
resource "netbox_tenant_group" "test" {{
  name        = "idem-tengrp-{uid}"
  description = "updated"
}}""",
        "api_path":    "tenancy/tenant-groups",
        "api_payload": lambda uid, p: {
            "name": f"idem-tengrp-{uid}",
            "slug": f"idem-tengrp-{uid}",
        },
    },

    {
        "name": "netbox_tenant",
        "create_tf": """\
resource "netbox_tenant" "test" {{
  name = "idem-tenant-{uid}"
}}""",
        "update_tf": """\
resource "netbox_tenant" "test" {{
  name        = "idem-tenant-{uid}"
  description = "updated"
}}""",
        "api_path":    "tenancy/tenants",
        "api_payload": lambda uid, p: {
            "name": f"idem-tenant-{uid}",
            "slug": f"idem-tenant-{uid}",
        },
    },

    {
        "name": "netbox_contact_role",
        "create_tf": """\
resource "netbox_contact_role" "test" {{
  name = "idem-crole-{uid}"
}}""",
        "update_tf": """\
resource "netbox_contact_role" "test" {{
  name = "idem-crole-{uid}"
  slug = "idem-crole-{uid}-v2"
}}""",
        "api_path":    "tenancy/contact-roles",
        "api_payload": lambda uid, p: {
            "name": f"idem-crole-{uid}",
            "slug": f"idem-crole-{uid}",
        },
    },

    {
        "name": "netbox_contact_group",
        "create_tf": """\
resource "netbox_contact_group" "test" {{
  name = "idem-cgrp-{uid}"
}}""",
        "update_tf": """\
resource "netbox_contact_group" "test" {{
  name        = "idem-cgrp-{uid}"
  description = "updated"
}}""",
        "api_path":    "tenancy/contact-groups",
        "api_payload": lambda uid, p: {
            "name": f"idem-cgrp-{uid}",
            "slug": f"idem-cgrp-{uid}",
        },
    },

    # ── DCIM ─────────────────────────────────────────────────────────────────

    {
        "name": "netbox_region",
        "create_tf": """\
resource "netbox_region" "test" {{
  name = "idem-region-{uid}"
}}""",
        "update_tf": """\
resource "netbox_region" "test" {{
  name        = "idem-region-{uid}"
  description = "updated"
}}""",
        "api_path":    "dcim/regions",
        "api_payload": lambda uid, p: {
            "name": f"idem-region-{uid}",
            "slug": f"idem-region-{uid}",
        },
    },

    {
        "name": "netbox_site_group",
        "create_tf": """\
resource "netbox_site_group" "test" {{
  name = "idem-sitegrp-{uid}"
}}""",
        "update_tf": """\
resource "netbox_site_group" "test" {{
  name        = "idem-sitegrp-{uid}"
  description = "updated"
}}""",
        "api_path":    "dcim/site-groups",
        "api_payload": lambda uid, p: {
            "name": f"idem-sitegrp-{uid}",
            "slug": f"idem-sitegrp-{uid}",
        },
    },

    {
        "name": "netbox_site",
        "create_tf": """\
resource "netbox_site" "test" {{
  name   = "idem-site-{uid}"
  status = "active"
}}""",
        "update_tf": """\
resource "netbox_site" "test" {{
  name        = "idem-site-{uid}"
  status      = "active"
  description = "updated"
}}""",
        "api_path":    "dcim/sites",
        "api_payload": lambda uid, p: {
            "name":   f"idem-site-{uid}",
            "slug":   f"idem-site-{uid}",
            "status": "active",
        },
    },

    {
        "name": "netbox_location",
        "create_tf": """\
resource "netbox_location" "test" {{
  name    = "idem-loc-{uid}"
  site_id = {site_id}
}}""",
        "update_tf": """\
resource "netbox_location" "test" {{
  name        = "idem-loc-{uid}"
  site_id     = {site_id}
  description = "updated"
}}""",
        "api_path":    "dcim/locations",
        "api_payload": lambda uid, p: {
            "name": f"idem-loc-{uid}",
            "slug": f"idem-loc-{uid}",
            "site": p["site_id"],
        },
    },

    {
        "name": "netbox_platform",
        "create_tf": """\
resource "netbox_platform" "test" {{
  name = "idem-platform-{uid}"
}}""",
        "update_tf": """\
resource "netbox_platform" "test" {{
  name = "idem-platform-{uid}"
  slug = "idem-platform-{uid}-v2"
}}""",
        "api_path":    "dcim/platforms",
        "api_payload": lambda uid, p: {
            "name": f"idem-platform-{uid}",
            "slug": f"idem-platform-{uid}",
        },
    },

    {
        "name": "netbox_device_role",
        "create_tf": """\
resource "netbox_device_role" "test" {{
  name      = "idem-devrole-{uid}"
  color_hex = "aa1409"
}}""",
        "update_tf": """\
resource "netbox_device_role" "test" {{
  name        = "idem-devrole-{uid}"
  color_hex   = "4caf50"
  description = "updated"
}}""",
        "api_path":    "dcim/device-roles",
        "api_payload": lambda uid, p: {
            "name":  f"idem-devrole-{uid}",
            "slug":  f"idem-devrole-{uid}",
            "color": "aa1409",
        },
    },

    {
        "name": "netbox_rack_role",
        "create_tf": """\
resource "netbox_rack_role" "test" {{
  name      = "idem-rrole-{uid}"
  color_hex = "aa1409"
}}""",
        "update_tf": """\
resource "netbox_rack_role" "test" {{
  name        = "idem-rrole-{uid}"
  color_hex   = "4caf50"
  description = "updated"
}}""",
        "api_path":    "dcim/rack-roles",
        "api_payload": lambda uid, p: {
            "name":  f"idem-rrole-{uid}",
            "slug":  f"idem-rrole-{uid}",
            "color": "aa1409",
        },
    },

    {
        "name": "netbox_manufacturer",
        "create_tf": """\
resource "netbox_manufacturer" "test" {{
  name = "idem-mfr-{uid}"
}}""",
        "update_tf": """\
resource "netbox_manufacturer" "test" {{
  name = "idem-mfr-{uid}"
  slug = "idem-mfr-{uid}-v2"
}}""",
        "api_path":    "dcim/manufacturers",
        "api_payload": lambda uid, p: {
            "name": f"idem-mfr-{uid}",
            "slug": f"idem-mfr-{uid}",
        },
    },

    {
        "name": "netbox_device_type",
        "create_tf": """\
resource "netbox_device_type" "test" {{
  model           = "idem-dt-{uid}"
  manufacturer_id = {manufacturer_id}
}}""",
        "update_tf": """\
resource "netbox_device_type" "test" {{
  model           = "idem-dt-{uid}"
  manufacturer_id = {manufacturer_id}
  part_number     = "PN-{uid}"
}}""",
        "api_path":    "dcim/device-types",
        "api_payload": lambda uid, p: {
            "model":        f"idem-dt-{uid}",
            "slug":         f"idem-dt-{uid}",
            "manufacturer": p["manufacturer_id"],
        },
    },

    # ── Virtualization ───────────────────────────────────────────────────────

    {
        "name": "netbox_cluster_type",
        "create_tf": """\
resource "netbox_cluster_type" "test" {{
  name = "idem-cltype-{uid}"
}}""",
        "update_tf": """\
resource "netbox_cluster_type" "test" {{
  name = "idem-cltype-{uid}"
  slug = "idem-cltype-{uid}-v2"
}}""",
        "api_path":    "virtualization/cluster-types",
        "api_payload": lambda uid, p: {
            "name": f"idem-cltype-{uid}",
            "slug": f"idem-cltype-{uid}",
        },
    },

    {
        "name": "netbox_cluster_group",
        "create_tf": """\
resource "netbox_cluster_group" "test" {{
  name = "idem-clgrp-{uid}"
}}""",
        "update_tf": """\
resource "netbox_cluster_group" "test" {{
  name        = "idem-clgrp-{uid}"
  description = "updated"
}}""",
        "api_path":    "virtualization/cluster-groups",
        "api_payload": lambda uid, p: {
            "name": f"idem-clgrp-{uid}",
            "slug": f"idem-clgrp-{uid}",
        },
    },

    {
        "name": "netbox_cluster",
        "create_tf": """\
resource "netbox_cluster" "test" {{
  name            = "idem-cluster-{uid}"
  cluster_type_id = {cluster_type_id}
}}""",
        "update_tf": """\
resource "netbox_cluster" "test" {{
  name            = "idem-cluster-{uid}"
  cluster_type_id = {cluster_type_id}
  description     = "updated"
}}""",
        "api_path":    "virtualization/clusters",
        "api_payload": lambda uid, p: {
            "name": f"idem-cluster-{uid}",
            "type": p["cluster_type_id"],
        },
    },

    # ── IPAM ─────────────────────────────────────────────────────────────────

    {
        "name": "netbox_rir",
        "create_tf": """\
resource "netbox_rir" "test" {{
  name = "idem-rir-{uid}"
}}""",
        "update_tf": """\
resource "netbox_rir" "test" {{
  name        = "idem-rir-{uid}"
  description = "updated"
}}""",
        "api_path":    "ipam/rirs",
        "api_payload": lambda uid, p: {
            "name": f"idem-rir-{uid}",
            "slug": f"idem-rir-{uid}",
        },
    },

    {
        "name": "netbox_asn",
        "create_tf": """\
resource "netbox_asn" "test" {{
  asn    = {asn_num}
  rir_id = {rir_id}
}}""",
        "update_tf": """\
resource "netbox_asn" "test" {{
  asn         = {asn_num}
  rir_id      = {rir_id}
  description = "updated"
}}""",
        "api_path":    "ipam/asns",
        "api_payload": lambda uid, p: {
            "asn": p["asn_num"],
            "rir": p["rir_id"],
        },
    },

    {
        "name": "netbox_asn_extended",
        "create_tf": """\
resource "netbox_asn_extended" "test" {{
  asn    = {asn_num_ext}
  rir_id = {rir_id}
}}""",
        "update_tf": """\
resource "netbox_asn_extended" "test" {{
  asn         = {asn_num_ext}
  rir_id      = {rir_id}
  tenant_id   = {tenant_id}
  description = "updated with tenant"
}}""",
        "api_path":    "ipam/asns",
        "api_payload": lambda uid, p: {
            "asn": p["asn_num_ext"],
            "rir": p["rir_id"],
        },
    },

    {
        "name": "netbox_vlan_group",
        "create_tf": """\
resource "netbox_vlan_group" "test" {{
  name       = "idem-vlangrp-{uid}"
  slug       = "idem-vlangrp-{uid}"
  vid_ranges = [[1, 4094]]
}}""",
        "update_tf": """\
resource "netbox_vlan_group" "test" {{
  name        = "idem-vlangrp-{uid}"
  slug        = "idem-vlangrp-{uid}"
  vid_ranges  = [[1, 4094]]
  description = "updated"
}}""",
        "api_path":    "ipam/vlan-groups",
        "api_payload": lambda uid, p: {
            "name":       f"idem-vlangrp-{uid}",
            "slug":       f"idem-vlangrp-{uid}",
            "vid_ranges": [[1, 4094]],
        },
    },

    {
        "name": "netbox_vlan",
        "create_tf": """\
resource "netbox_vlan" "test" {{
  name = "idem-vlan-{uid}"
  vid  = {vlan_vid}
}}""",
        "update_tf": """\
resource "netbox_vlan" "test" {{
  name        = "idem-vlan-{uid}"
  vid         = {vlan_vid}
  description = "updated"
}}""",
        "api_path":    "ipam/vlans",
        "api_payload": lambda uid, p: {
            "name":   f"idem-vlan-{uid}",
            "vid":    p["vlan_vid"],
            "status": "active",
        },
    },

    {
        "name": "netbox_ipam_role",
        "create_tf": """\
resource "netbox_ipam_role" "test" {{
  name = "idem-ipamrole-{uid}"
}}""",
        "update_tf": """\
resource "netbox_ipam_role" "test" {{
  name        = "idem-ipamrole-{uid}"
  description = "updated"
}}""",
        "api_path":    "ipam/roles",
        "api_payload": lambda uid, p: {
            "name": f"idem-ipamrole-{uid}",
            "slug": f"idem-ipamrole-{uid}",
        },
    },

    {
        "name": "netbox_route_target",
        "create_tf": """\
resource "netbox_route_target" "test" {{
  name = "idem-rt-{uid}"
}}""",
        "update_tf": """\
resource "netbox_route_target" "test" {{
  name        = "idem-rt-{uid}"
  description = "updated"
}}""",
        "api_path":    "ipam/route-targets",
        "api_payload": lambda uid, p: {
            "name": f"idem-rt-{uid}",
        },
    },

    {
        "name": "netbox_vrf",
        "create_tf": """\
resource "netbox_vrf" "test" {{
  name = "idem-vrf-{uid}"
}}""",
        "update_tf": """\
resource "netbox_vrf" "test" {{
  name        = "idem-vrf-{uid}"
  description = "updated"
}}""",
        "api_path":    "ipam/vrfs",
        "api_payload": lambda uid, p: {
            "name": f"idem-vrf-{uid}",
        },
    },

    # ── Circuits ─────────────────────────────────────────────────────────────

    {
        "name": "netbox_circuit_type",
        "create_tf": """\
resource "netbox_circuit_type" "test" {{
  name = "idem-ctype-{uid}"
}}""",
        "update_tf": """\
resource "netbox_circuit_type" "test" {{
  name        = "idem-ctype-{uid}"
  description = "updated"
}}""",
        "api_path":    "circuits/circuit-types",
        "api_payload": lambda uid, p: {
            "name": f"idem-ctype-{uid}",
            "slug": f"idem-ctype-{uid}",
        },
    },

    {
        "name": "netbox_circuit_provider",
        "create_tf": """\
resource "netbox_circuit_provider" "test" {{
  name = "idem-cprov-{uid}"
}}""",
        "update_tf": """\
resource "netbox_circuit_provider" "test" {{
  name        = "idem-cprov-{uid}"
  description = "updated"
}}""",
        "api_path":    "circuits/providers",
        "api_payload": lambda uid, p: {
            "name": f"idem-cprov-{uid}",
            "slug": f"idem-cprov-{uid}",
        },
    },

    # ── VPN ──────────────────────────────────────────────────────────────────

    {
        "name": "netbox_vpn_tunnel_group",
        "create_tf": """\
resource "netbox_vpn_tunnel_group" "test" {{
  name = "idem-vpntgrp-{uid}"
}}""",
        "update_tf": """\
resource "netbox_vpn_tunnel_group" "test" {{
  name        = "idem-vpntgrp-{uid}"
  description = "updated"
}}""",
        "api_path":    "vpn/tunnel-groups",
        "api_payload": lambda uid, p: {
            "name": f"idem-vpntgrp-{uid}",
            "slug": f"idem-vpntgrp-{uid}",
        },
    },
]


# ─── Single test runner ───────────────────────────────────────────────────────

class TestResult:
    def __init__(self, name: str):
        self.name       = name
        self.crud_ok    = False
        self.idem_ok    = False
        self.error      = ""
        self.skipped    = False

    @property
    def passed(self) -> bool:
        return self.crud_ok and self.idem_ok

    def __str__(self) -> str:
        if self.skipped:
            return _skip(f"{self.name}  (skipped)")
        if self.passed:
            return _ok(f"{self.name}")
        crud = "✓" if self.crud_ok else "✗"
        idem = "✓" if self.idem_ok else "✗"
        return _fail(f"{self.name}  [crud:{crud} idempotency:{idem}]")


def run_test(
    tc: Dict,
    run_id: str,
    prereqs: Dict,
    tf: TerraformRunner,
    nb_url: str,
    nb_token: str,
    nb: NetBoxClient,
    verbose: bool,
) -> TestResult:
    result = TestResult(tc["name"])
    # Unique suffix for all objects in this test
    uid = f"{tc['name'][:6].replace('_','')}{run_id}"
    ctx = {"uid": uid, **prereqs}

    try:
        create_hcl = tc["create_tf"].format(**ctx)
        update_hcl = tc["update_tf"].format(**ctx)
    except KeyError as e:
        result.error = f"Template placeholder missing: {e}"
        result.skipped = True
        return result

    provider = provider_hcl(nb_url, nb_token)

    with tempfile.TemporaryDirectory(prefix=f"idem_{tc['name']}_") as work_dir:

        # ── 1. Normal CRUD ───────────────────────────────────────────────────
        _section(f"  [{tc['name']}] 1/2 normal CRUD")

        write_tf(work_dir, provider, create_hcl)
        # NOTE: do NOT call terraform init when using dev_overrides — Terraform
        # itself warns: "Skip terraform init when using provider development
        # overrides. It is not necessary and may error unexpectedly."
        ok_apply, out_apply = tf.apply(work_dir)
        if verbose: print(out_apply)
        if not ok_apply:
            result.error = f"create apply failed:\n{out_apply[-2000:]}"
            _try_destroy(tf, work_dir, verbose)
            return result
        print(_ok("create"))

        write_tf(work_dir, provider, update_hcl)
        ok_upd, out_upd = tf.apply(work_dir)
        if verbose: print(out_upd)
        if not ok_upd:
            result.error = f"update apply failed:\n{out_upd[-2000:]}"
            _try_destroy(tf, work_dir, verbose)
            return result
        print(_ok("update"))

        ok_destroy, out_destroy = tf.destroy(work_dir)
        if verbose: print(out_destroy)
        if not ok_destroy:
            result.error = f"destroy failed:\n{out_destroy[-2000:]}"
            return result
        print(_ok("destroy"))
        result.crud_ok = True

        # ── 2. Idempotency ──────────────────────────────────────────────────
        _section(f"  [{tc['name']}] 2/2 idempotency (pre-existing NetBox object)")

        # Create the object directly in NetBox via API
        api_payload = tc["api_payload"](uid, prereqs)
        try:
            obj = nb.create(tc["api_path"], api_payload)
        except RuntimeError as e:
            result.error = f"pre-create via API failed: {e}"
            return result
        obj_id = obj["id"]
        print(_info(f"Created {tc['api_path']} id={obj_id} directly in NetBox"))

        # Clear Terraform state so it has no knowledge of the object
        state_file = os.path.join(work_dir, "terraform.tfstate")
        if os.path.exists(state_file):
            os.remove(state_file)

        # Terraform apply should adopt the existing object
        write_tf(work_dir, provider, create_hcl)
        ok_idem_apply, out_idem = tf.apply(work_dir)
        if verbose: print(out_idem)
        if not ok_idem_apply:
            result.error = f"idempotency apply failed:\n{out_idem[-2000:]}"
            # Clean up directly since Terraform state may be empty
            nb.delete(tc["api_path"], obj_id)
            return result
        print(_ok("apply on pre-existing object (adopted into state)"))

        write_tf(work_dir, provider, update_hcl)
        ok_idem_upd, out_idem_upd = tf.apply(work_dir)
        if verbose: print(out_idem_upd)
        if not ok_idem_upd:
            result.error = f"idempotency update failed:\n{out_idem_upd[-2000:]}"
            _try_destroy(tf, work_dir, verbose)
            return result
        print(_ok("update after adoption"))

        ok_idem_destroy, out_idem_destroy = tf.destroy(work_dir)
        if verbose: print(out_idem_destroy)
        if not ok_idem_destroy:
            result.error = f"idempotency destroy failed:\n{out_idem_destroy[-2000:]}"
            return result
        print(_ok("destroy after adoption"))
        result.idem_ok = True

    return result


def _try_destroy(tf: TerraformRunner, work_dir: str, verbose: bool):
    """Best-effort destroy for cleanup on test failure."""
    ok, out = tf.destroy(work_dir)
    if verbose or not ok:
        print(f"    [cleanup destroy] {'ok' if ok else 'FAILED'}\n{out[-500:]}")


# ─── Provider binary detection ────────────────────────────────────────────────

def find_provider_dir(hint: Optional[str]) -> str:
    if hint:
        return os.path.abspath(hint)
    # Walk up from this file to find the project root (go.mod)
    here = Path(__file__).resolve()
    for parent in [here.parent, here.parent.parent, here.parent.parent.parent]:
        if (parent / "go.mod").exists():
            # Check if a provider binary exists there
            for name in ["terraform-provider-netbox",
                         "terraform-provider-netbox_linux_amd64",
                         "terraform-provider-netbox_darwin_amd64",
                         "terraform-provider-netbox_darwin_arm64"]:
                if (parent / name).exists():
                    return str(parent)
            return str(parent)  # Return root even if binary isn't there yet
    raise RuntimeError(
        "Could not auto-detect provider directory. "
        "Use --provider-dir to point to the directory containing "
        "the terraform-provider-netbox binary."
    )


# ─── Main ─────────────────────────────────────────────────────────────────────

def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Idempotency integration tests for terraform-provider-netbox",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    p.add_argument("--netbox-url",      required=True,
                   help="NetBox base URL, e.g. http://localhost:8000")
    p.add_argument("--netbox-username", required=True,
                   help="NetBox admin username")
    p.add_argument("--netbox-password", required=True,
                   help="NetBox admin password")
    p.add_argument("--terraform-path",  required=True,
                   help="Path to the Terraform CLI binary")
    p.add_argument("--provider-dir",    default=None,
                   help="Directory containing the built terraform-provider-netbox "
                        "binary (default: auto-detect from project root)")
    p.add_argument("--filter",          default=None,
                   help="Only run tests whose name contains this substring")
    p.add_argument("--verbose",         action="store_true",
                   help="Print full Terraform output for every step")
    p.add_argument("--stop-on-failure", action="store_true",
                   help="Abort after the first test failure")
    return p.parse_args()


def main():
    args = parse_args()

    # ── Resolve provider binary directory ────────────────────────────────────
    try:
        provider_dir = find_provider_dir(args.provider_dir)
    except RuntimeError as e:
        print(f"ERROR: {e}")
        sys.exit(1)

    print(_info(f"Provider directory : {provider_dir}"))
    print(_info(f"Terraform binary   : {args.terraform_path}"))
    print(_info(f"NetBox URL         : {args.netbox_url}"))

    # ── Connect to NetBox ─────────────────────────────────────────────────────
    nb = NetBoxClient(args.netbox_url, args.netbox_username, args.netbox_password)
    try:
        token = nb.connect()
    except Exception as e:
        print(f"\nERROR: Could not connect to NetBox: {e}")
        sys.exit(1)
    print(_ok(f"Connected to NetBox (token acquired)"))

    # ── Set up Terraform runner ───────────────────────────────────────────────
    tf = TerraformRunner(args.terraform_path, provider_dir)
    with tempfile.TemporaryDirectory(prefix="idem_suite_") as suite_tmp:
        tf.setup_dev_overrides(suite_tmp)

        # ── Generate run ID (4 hex chars = unique across runs) ───────────────
        run_id = "".join(random.choices("0123456789abcdef", k=4))
        print(_info(f"Run ID             : {run_id}"))

        # ── Create prerequisites ─────────────────────────────────────────────
        _banner("Setting up prerequisites")
        prereqs, prereq_cleanup = setup_prereqs(nb, run_id)

        # ── Filter test cases ────────────────────────────────────────────────
        cases = TEST_CASES
        if args.filter:
            cases = [tc for tc in cases if args.filter in tc["name"]]
            print(_info(f"Filter: '{args.filter}'  →  {len(cases)} test(s)"))

        # ── List resources under test ────────────────────────────────────────
        _banner(f"Resources under test ({len(cases)})")
        for i, tc in enumerate(cases, 1):
            print(f"  {i:2}. {tc['name']}")

        # ── Run tests ────────────────────────────────────────────────────────
        _banner(f"Running {len(cases)} test(s)")
        results: List[TestResult] = []
        for i, tc in enumerate(cases, 1):
            print(f"\n{BOLD}[{i}/{len(cases)}] {tc['name']}{RESET}")
            try:
                r = run_test(
                    tc, run_id, prereqs, tf,
                    args.netbox_url, token, nb,
                    args.verbose,
                )
            except Exception as exc:
                r = TestResult(tc["name"])
                r.error = f"Unexpected exception:\n{traceback.format_exc()}"
                print(_fail(f"UNEXPECTED ERROR: {exc}"))
            results.append(r)

            # Per-test summary line
            if r.passed:
                print(_ok(f"{tc['name']} — PASSED"))
            elif r.skipped:
                print(_skip(f"{tc['name']} — SKIPPED: {r.error}"))
            else:
                print(_fail(f"{tc['name']} — FAILED: {r.error[:200]}"))
                if not args.verbose:
                    print("    (re-run with --verbose to see Terraform output)")
                if args.stop_on_failure:
                    print("\nStopping after first failure (--stop-on-failure).")
                    break

        # ── Clean up prerequisites ───────────────────────────────────────────
        _banner("Cleaning up prerequisites")
        cleanup_prereqs(nb, prereq_cleanup)

    # ── Release token ────────────────────────────────────────────────────────
    nb.disconnect()

    # ── Final report ─────────────────────────────────────────────────────────
    _banner("Results")
    passed  = sum(1 for r in results if r.passed)
    failed  = sum(1 for r in results if not r.passed and not r.skipped)
    skipped = sum(1 for r in results if r.skipped)

    for r in results:
        print(str(r))
        if not r.passed and not r.skipped and r.error:
            for line in r.error.splitlines()[-5:]:
                print(f"      {line}")

    print()
    print(_bold(f"Total:   {len(results)}"))
    print(_ok   (f"Passed:  {passed}") if passed  else f"  Passed:  {passed}")
    print(_fail (f"Failed:  {failed}") if failed  else f"  Failed:  {failed}")
    if skipped:
        print(_skip(f"Skipped: {skipped}"))

    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
