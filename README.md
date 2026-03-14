# Terraform NetBox Idempotent Provider

This is a fork of [e-breuninger/terraform-provider-netbox](https://github.com/e-breuninger/terraform-provider-netbox). The primary addition in this fork is idempotent resource creation: when Terraform attempts to create an object that already exists in NetBox (e.g. because it was created manually or by a previous run whose state was lost), the provider looks up the existing object by its identifying attributes and imports it into state — instead of failing with an error.

It solves the following issues:

- **Existing NetBox objects without Terraform state**
  Would normally cause `terraform apply` to fail with a duplicate-name error. Now, these objects are detected, looked up by name, and silently adopted into state.

- **State loss or drift**
  If Terraform state is lost or deleted, re-applying will not attempt to create duplicates. Existing objects are matched and re-imported automatically.

- **No import statements needed**
  Since conflicts are resolved in code, explicit `terraform import` commands are no longer required to reconcile manually created objects with Terraform-managed infrastructure.

The following resources are covered by this fix:

`netbox_tag`, `netbox_tenant_group`, `netbox_tenant`, `netbox_contact_role`, `netbox_contact_group`, `netbox_region`, `netbox_site_group`, `netbox_site`, `netbox_location`, `netbox_platform`, `netbox_device_role`, `netbox_rack_role`, `netbox_manufacturer`, `netbox_device_type`, `netbox_cluster_type`, `netbox_cluster_group`, `netbox_cluster`, `netbox_rir`, `netbox_asn`, `netbox_vlan_group`, `netbox_vlan`, `netbox_ipam_role`, `netbox_route_target`, `netbox_vrf`, `netbox_circuit_type`, `netbox_circuit_provider`, `netbox_vpn_tunnel_group`

In addition, the following new resource has been added:

- **`netbox_asn_extended`** — extends `netbox_asn` with support for the `tenant_id` parameter, which is available in the NetBox API but missing from the upstream resource.

## From the original Netbox Provider

The Terraform Netbox provider is a plugin for Terraform that allows for the full lifecycle management of [Netbox](https://netboxlabs.com/docs/netbox/) resources.
This provider is maintained by E. Breuninger.

See: [Official documentation](https://registry.terraform.io/providers/e-breuninger/netbox/latest/docs) in the Terraform registry.

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 0.12.x

## Supported netbox versions

Netbox often makes breaking API changes even in non-major releases. Check the table below to see which version a provider was tested against. It is generally recommended to use the provider version matching your Netbox version. We aim to always support the latest minor version of Netbox.

Since version [1.6.6](https://github.com/e-breuninger/terraform-provider-netbox/commit/0b0b2fffa54d4ab2e5f1677e948b01e56ba211c8), each version of the provider has a built-in list of all Netbox versions it supports at release time. Upon initialization, the provider will probe your Netbox version and include a (non-blocking) warning if the used Netbox version is not supported.

| Netbox version  | Provider version |
| --------------- | ---------------- |
| v4.3.0 - 4.4.10 | v5.0.0 and up    |
| v4.2.2 - 4.2.9  | v4.0.0 - 4.3.1   |
| v4.1.0 - 4.1.11 | v3.10.0 - 3.11.1 |
| v4.0.0 - 4.0.11 | v3.9.0 - 3.9.2   |
| v3.7.0 - 3.7.8  | v3.8.0 - 3.8.9   |
| v3.6.0 - 3.6.9  | v3.7.0 - 3.7.7   |
| v3.5.1 - 3.5.9  | v3.6.x           |
| v3.4.3 - 3.4.10 | v3.5.x           |
| v3.3.0 - 3.4.2  | v3.0.x - 3.5.1   |
| v3.2.0 - 3.2.9  | v2.0.x           |
| v3.1.9          | v1.6.0 - 1.6.7   |
| v3.1.3          | v1.1.x - 1.5.2   |
| v3.0.9          | v1.0.x           |
| v2.11.12        | v0.3.x           |
| v2.10.10        | v0.2.x           |
| v2.9            | v0.1.x           |

## Building The Provider

1. Clone the repository
1. Enter the repository directory
1. Build the provider using the Go `install` command:

```sh
go install
```

## Installation

Starting with Terraform 0.13, you can download the provider via the Terraform registry.

For further information on how to use third party providers, see the [Terraform documentation](https://www.terraform.io/docs/configuration/providers.html)

Releases for all major plattforms are available on the release page.

## Using the provider

Here is a short example on how to use this provider:

```hcl
provider "netbox" {
  server_url = "https://demo.netbox.dev"
  api_token  = "<your api token>"
}

resource "netbox_platform" "testplatform" {
  name = "my-test-platform"
}
```

For a more examples, see the [provider documentation](https://registry.terraform.io/providers/e-breuninger/netbox/latest/docs).

## Developing the Provider

If you wish to work on the provider, you need [Go](http://www.golang.org) installed on your machine.

To compile the provider, run `go install`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

To generate or update documentation, run `make docs`.

In order to run the suite of unit tests, run `make test`.

In order to run the full suite of acceptance tests, run `make testacc`.

In order to run a single specific acceptance test, run

```sh
TEST_FUNC=<test_name> make testacc-specific-test
```

For example:

```sh
TEST_FUNC=TestAccNetboxLocationDataSource_basic make testacc-specific-test
```

_Note:_ Acceptance tests create a docker compose stack on port 8001.

```sh
make testacc
```

If you notice a failed test, it might be due to a stale netbox data volume. Before concluding there is a problem,
refresh the docker containers by running `docker-compose down --volumes` in the `docker` directory. Then run the tests again.

If you get `too many open files` errors when running the acceptance test suite locally on Linux, your user limit for open file descriptors might be too low. You can increase that limit with `ulimit -n 2048`.

## Contribution

We focus on virtual machine management and IPAM. If you want to contribute more resources to this provider, feel free to make a PR.
