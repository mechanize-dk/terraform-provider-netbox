resource "netbox_rir" "test" {
  name = "testrir"
}

resource "netbox_tenant" "test" {
  name = "testtenant"
}

resource "netbox_asn_extended" "test" {
  asn         = 1337
  rir_id      = netbox_rir.test.id
  tenant_id   = netbox_tenant.test.id
  description = "test"
  comments    = "test"
}
