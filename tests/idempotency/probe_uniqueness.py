#!/usr/bin/env python3
"""Empirically probe which NetBox endpoints enforce uniqueness on which fields.

Background
----------
The provider's idempotency layer (netbox/mechanize_idempotency.go) adopts an
existing object when a Create fails, looking it up by a single field -- almost
always `name`. That is only correct where NetBox actually enforces uniqueness on
that field. NetBox enforces uniqueness on `slug` (often scoped) and on various
natural keys, NOT reliably on `name`. NetBox does not expose its uniqueness
constraints via OPTIONS or the OpenAPI schema, so the only trustworthy way to
learn them is to ask the running instance: create an object, then create a
second one that collides on exactly one candidate field, and see whether NetBox
returns 400 (unique) or 201 (duplicate allowed).

This script does that for every endpoint the idempotency layer covers, prints a
table, and writes a JSON summary. It creates throwaway objects and deletes
everything it created (in reverse order) on the way out, even on error.

Usage
-----
    export NETBOX_URL=https://netbox-host
    export NETBOX_TOKEN=...            # or NETBOX_TOKEN_FILE=/path/to/token
    python3 tests/idempotency/probe_uniqueness.py [--json out.json] [--insecure]

The token is read from the environment (or a file path) at runtime and is never
logged.
"""

import argparse
import json
import os
import random
import string
import sys
import urllib.request
import urllib.error
import ssl

# --- transport ---------------------------------------------------------------


class NetBox:
    def __init__(self, base_url, token, insecure=False, timeout=20):
        self.base = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout
        self.ctx = ssl.create_default_context()
        if insecure:
            self.ctx.check_hostname = False
            self.ctx.verify_mode = ssl.CERT_NONE

    def _req(self, method, path, payload=None):
        url = f"{self.base}/api/{path.strip('/')}/"
        data = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Authorization", f"Token {self.token}")
        req.add_header("Content-Type", "application/json")
        req.add_header("Accept", "application/json")
        # Some deployments sit behind Cloudflare, which bans the default
        # urllib user-agent (error 1010). Present a conventional UA.
        req.add_header("User-Agent", "netbox-uniqueness-probe/1.0")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout, context=self.ctx) as resp:
                body = resp.read().decode()
                return resp.status, (json.loads(body) if body else {})
        except urllib.error.HTTPError as e:
            body = e.read().decode()
            try:
                parsed = json.loads(body)
            except json.JSONDecodeError:
                parsed = {"_raw": body[:500]}
            return e.code, parsed
        except (urllib.error.URLError, TimeoutError, ssl.SSLError) as e:
            return 0, {"_error": str(e)}

    def create(self, path, payload):
        return self._req("POST", path, payload)

    def delete(self, path, obj_id):
        return self._req("DELETE", f"{path}/{obj_id}")


# --- helpers -----------------------------------------------------------------


def rand(n=6):
    return "".join(random.choices(string.ascii_lowercase + string.digits, k=n))


class Probe:
    """Drives creation/cleanup and records every object it makes."""

    def __init__(self, nb):
        self.nb = nb
        self.created = []  # (path, id) in creation order

    def make(self, path, payload):
        status, body = self.nb.create(path, payload)
        if status == 201 and isinstance(body, dict) and "id" in body:
            self.created.append((path, body["id"]))
        return status, body

    def cleanup(self):
        errors = 0
        for path, obj_id in reversed(self.created):
            status, _ = self.nb.delete(path, obj_id)
            if status not in (204, 200, 404):
                errors += 1
        return errors


# --- endpoint catalogue ------------------------------------------------------
# Each endpoint maps a lookup in mechanize_idempotency.go. `candidates` are the
# fields we test for uniqueness. `base(suffix, fks)` returns a fully valid,
# fresh payload; `fks` holds ids of prerequisites created up front.


def build_catalogue(fks):
    def named(suffix, **extra):
        p = {"name": f"probe-{suffix}", "slug": f"probe-{suffix}"}
        p.update(extra)
        return p

    return [
        # key, path, candidates, base builder
        ("tag", "extras/tags", ["name", "slug"], lambda s: named(s)),
        ("tenant", "tenancy/tenants", ["name", "slug"], lambda s: named(s)),
        ("tenant_group", "tenancy/tenant-groups", ["name", "slug"], lambda s: named(s)),
        ("contact_role", "tenancy/contact-roles", ["name", "slug"], lambda s: named(s)),
        ("contact_group", "tenancy/contact-groups", ["name", "slug"], lambda s: named(s)),
        ("site", "dcim/sites", ["name", "slug"], lambda s: named(s, status="active")),
        ("site_group", "dcim/site-groups", ["name", "slug"], lambda s: named(s)),
        ("region", "dcim/regions", ["name", "slug"], lambda s: named(s)),
        ("location", "dcim/locations", ["name", "slug"],
         lambda s: named(s, site=fks["site"], status="active")),
        ("platform", "dcim/platforms", ["name", "slug"], lambda s: named(s)),
        ("device_role", "dcim/device-roles", ["name", "slug"], lambda s: named(s)),
        ("manufacturer", "dcim/manufacturers", ["name", "slug"], lambda s: named(s)),
        ("device_type", "dcim/device-types", ["model", "slug"],
         lambda s: {"manufacturer": fks["manufacturer"], "model": f"probe-{s}", "slug": f"probe-{s}"}),
        ("rack_role", "dcim/rack-roles", ["name", "slug"], lambda s: named(s)),
        ("cluster_type", "virtualization/cluster-types", ["name", "slug"], lambda s: named(s)),
        ("cluster_group", "virtualization/cluster-groups", ["name", "slug"], lambda s: named(s)),
        ("cluster", "virtualization/clusters", ["name"],
         lambda s: {"name": f"probe-{s}", "type": fks["cluster_type"], "status": "active"}),
        ("vlan_group", "ipam/vlan-groups", ["name", "slug"], lambda s: named(s)),
        ("vlan", "ipam/vlans", ["vid", "name"],
         lambda s: {"name": f"probe-{s}", "vid": random.randint(2, 4094), "status": "active"}),
        ("ipam_role", "ipam/roles", ["name", "slug"], lambda s: named(s)),
        ("route_target", "ipam/route-targets", ["name"],
         lambda s: {"name": f"{random.randint(1, 65000)}:{random.randint(1, 65000)}"}),
        ("vrf", "ipam/vrfs", ["name"], lambda s: {"name": f"probe-{s}"}),
        ("asn", "ipam/asns", ["asn"],
         lambda s: {"asn": random.randint(64512, 65534), "rir": fks["rir"]}),
        ("rir", "ipam/rirs", ["name", "slug"], lambda s: named(s)),
        ("circuit_type", "circuits/circuit-types", ["name", "slug"], lambda s: named(s)),
        ("circuit_provider", "circuits/providers", ["name", "slug"], lambda s: named(s)),
        ("vpn_tunnel_group", "vpn/tunnel-groups", ["name", "slug"], lambda s: named(s)),
    ]


def setup_prerequisites(probe):
    """Create the FK objects some endpoints require. Returns {name: id}."""
    fks = {}
    specs = [
        ("manufacturer", "dcim/manufacturers", {"name": f"probe-fk-{rand()}", "slug": f"probe-fk-{rand()}"}),
        ("site", "dcim/sites", {"name": f"probe-fk-{rand()}", "slug": f"probe-fk-{rand()}", "status": "active"}),
        ("cluster_type", "virtualization/cluster-types", {"name": f"probe-fk-{rand()}", "slug": f"probe-fk-{rand()}"}),
        ("rir", "ipam/rirs", {"name": f"probe-fk-{rand()}", "slug": f"probe-fk-{rand()}"}),
    ]
    for name, path, payload in specs:
        status, body = probe.make(path, payload)
        if status != 201:
            raise RuntimeError(f"prerequisite {name} failed ({status}): {body}")
        fks[name] = body["id"]
    return fks


def probe_field(probe, path, base_builder, field):
    """Return (verdict, detail) for whether `field` is part of a unique constraint.

    Creates a baseline, then a second object that collides only on `field`
    (every other candidate value regenerated), holding FKs/scope constant.
    """
    p1 = base_builder(rand())
    s1, b1 = probe.make(path, p1)
    if s1 != 201:
        return "setup-failed", {"status": s1, "body": b1}

    p2 = base_builder(rand())
    p2[field] = p1[field]  # collide on exactly this field
    s2, b2 = probe.make(path, p2)

    if s2 == 400:
        cited = list(b2.keys()) if isinstance(b2, dict) else []
        return "UNIQUE", {"status": 400, "cited": cited, "body": b2}
    if s2 == 201:
        return "duplicate-allowed", {"status": 201}
    return "inconclusive", {"status": s2, "body": b2}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--json", help="write full results to this JSON file")
    ap.add_argument("--insecure", action="store_true", help="skip TLS verification")
    args = ap.parse_args()

    base_url = os.environ.get("NETBOX_URL")
    token = os.environ.get("NETBOX_TOKEN")
    token_file = os.environ.get("NETBOX_TOKEN_FILE")
    if token_file and not token:
        try:
            token = open(os.path.expanduser(token_file)).read().strip()
        except OSError as e:
            print(f"ERROR: cannot read NETBOX_TOKEN_FILE: {e}", file=sys.stderr)
            return 2
    if not base_url or not token:
        print("ERROR: set NETBOX_URL and NETBOX_TOKEN (or NETBOX_TOKEN_FILE).", file=sys.stderr)
        return 2

    nb = NetBox(base_url, token, insecure=args.insecure)
    status, body = nb._req("GET", "status")
    if status != 200:
        print(f"ERROR: cannot reach NetBox at {base_url} (status {status}): {body}", file=sys.stderr)
        return 2
    print(f"Probing {base_url} — NetBox {body.get('netbox-version', '?')}\n")

    probe = Probe(nb)
    results = {}
    try:
        fks = setup_prerequisites(probe)
        catalogue = build_catalogue(fks)
        for key, path, candidates, base_builder in catalogue:
            field_results = {}
            for field in candidates:
                verdict, detail = probe_field(probe, path, base_builder, field)
                field_results[field] = {"verdict": verdict, **detail}
            results[key] = {"path": path, "fields": field_results}
    except RuntimeError as e:
        print(f"ERROR during probe: {e}", file=sys.stderr)
    finally:
        errs = probe.cleanup()
        if errs:
            print(f"WARNING: {errs} created object(s) could not be deleted — check the lab.\n", file=sys.stderr)

    # --- report ---
    print(f"{'resource':<18} {'field':<10} {'verdict':<18} cited")
    print("-" * 70)
    recommendations = {}
    for key, info in results.items():
        unique_fields = []
        for field, r in info["fields"].items():
            cited = ",".join(r.get("cited", [])) if r["verdict"] == "UNIQUE" else ""
            print(f"{key:<18} {field:<10} {r['verdict']:<18} {cited}")
            if r["verdict"] == "UNIQUE":
                unique_fields.append(field)
        # Recommendation: prefer slug, else first unique field, else natural key.
        current_key = _current_key(key)
        if "slug" in unique_fields:
            rec = "slug"
        elif unique_fields:
            rec = unique_fields[0]
        else:
            rec = "(none unique — needs composite/scoped key)"
        recommendations[key] = {"unique_fields": unique_fields, "current_key": current_key, "recommended_key": rec}

    print("\n=== recommendations (current lookup key -> recommended) ===")
    for key, rec in recommendations.items():
        flag = "" if rec["recommended_key"] == rec["current_key"] else "  <-- CHANGE"
        print(f"{key:<18} {rec['current_key']:<8} -> {rec['recommended_key']}{flag}")

    if args.json:
        with open(args.json, "w") as f:
            json.dump({"results": results, "recommendations": recommendations}, f, indent=2)
        print(f"\nWrote {args.json}")
    return 0


def _current_key(resource):
    """What mechanize_idempotency.go currently keys this lookup on."""
    special = {"device_type": "model", "vlan": "vid", "asn": "asn"}
    return special.get(resource, "name")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print("\nInterrupted.", file=sys.stderr)
        sys.exit(130)
