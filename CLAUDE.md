# CLAUDE.md

## Project Overview

This is a fork of [e-breuninger/terraform-provider-netbox](https://github.com/e-breuninger/terraform-provider-netbox), maintained by mechanize-dk. The primary additions over upstream are **idempotent resource creation** and the **`netbox_asn_extended`** resource.

The module path is `github.com/mechanize-dk/terraform-provider-netbox` and the provider registry address is `registry.terraform.io/mechanize-dk/netbox`.

## Key Fork Changes

All mechanize-dk additions are clearly marked with `// MECHANIZE FORK:` comments to make upstream rebasing easy.

### Idempotency (`netbox/mechanize_idempotency.go`)
When a resource Create fails because an object already exists in NetBox, the provider looks it up by its identifying attributes and adopts it into Terraform state instead of returning an error. Each supported resource has a corresponding `mechanizeLookupXxx(api, d)` function in this file.

The pattern added to each resource's Create function:
```go
res, err := api.Xxx.XxxCreate(params, nil)
if err != nil {
    if id, lookupErr := mechanizeLookupXxx(api, d); lookupErr == nil {
        d.SetId(strconv.FormatInt(id, 10))
        return resourceNetboxXxxRead(d, m)
    }
    return err
}
```

### New Resource: `netbox_asn_extended` (`netbox/resource_netbox_asn_extended.go`)
Extends `netbox_asn` with an optional `tenant_id` field. Registered in `netbox/provider.go`.

## Build & Test

```bash
# Build the provider binary
go build -o terraform-provider-netbox .

# Regenerate docs (run after changing templates/ or adding resources)
go generate ./...

# Run idempotency integration tests against a live NetBox instance
python3 tests/idempotency/test_idempotency.py \
  --netbox-url      https://<netbox-host> \
  --netbox-username <username> \
  --netbox-password <password> \
  --terraform-path  /path/to/terraform \
  --provider-dir    .
```

The test script requires the provider binary to be built first. It does **not** run `terraform init` (Terraform warns against this when using `dev_overrides`).

## Release

Releases are triggered by pushing a `v*` tag. GoReleaser builds signed binaries for Linux, macOS, and Windows.

```bash
git tag v5.x.x
git push origin v5.x.x
```

Prerequisites:
- `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets set in GitHub Actions
- GitHub Actions workflow permissions set to "Read and write" (configured at the org level for mechanize-dk)

## Upstream Sync

To rebase against upstream:
```bash
git fetch upstream
git rebase upstream/master
```

Conflicts will be limited to:
- `netbox/mechanize_idempotency.go` — new file, no conflicts expected
- `netbox/resource_netbox_asn_extended.go` — new file, no conflicts expected
- `netbox/provider.go` — one line adding `netbox_asn_extended` to ResourcesMap
- The 27 resource files that have the 4-line idempotency block in their Create function
- `go.mod` — module path change

## Conventions

- Do not add `Co-Authored-By: Claude` trailers to commits
- Keep mechanize-dk changes minimal and clearly marked to ease upstream rebasing
- After any change to `templates/` or adding a new resource, run `go generate ./...` and commit the updated `docs/`
