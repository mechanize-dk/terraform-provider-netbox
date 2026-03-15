resource "netbox_tenant" "example" {
  name = "Example Tenant"
}

resource "netbox_vlan_extended" "example" {
  name        = "VLAN 100"
  vid         = 100
  tenant_id   = netbox_tenant.example.id
  description = "Example VLAN with tenant and cascade-delete of prefixes"
  tags        = []
}
