package netbox

// MECHANIZE FORK: Idempotency support
// This file is part of the mechanize-dk fork additions.
//
// When a Create call fails (e.g. because the resource already exists in NetBox
// but not in Terraform state), these lookup functions attempt to find the
// existing object by its unique attributes so Terraform can adopt it into state.
//
// Pattern used in each Create function:
//
//	res, err := api.XXX.Create(params, nil)
//	if err != nil {
//	    if id, lookupErr := mechanizeLookupXxx(api, d); lookupErr == nil {
//	        d.SetId(strconv.FormatInt(id, 10))
//	        return resourceNetboxXxxRead(d, m)
//	    }
//	    return err
//	}

import (
	"fmt"
	"strconv"

	"github.com/fbreckle/go-netbox/netbox/client/circuits"
	"github.com/fbreckle/go-netbox/netbox/client/dcim"
	"github.com/fbreckle/go-netbox/netbox/client/extras"
	"github.com/fbreckle/go-netbox/netbox/client/ipam"
	"github.com/fbreckle/go-netbox/netbox/client/tenancy"
	"github.com/fbreckle/go-netbox/netbox/client/virtualization"
	"github.com/fbreckle/go-netbox/netbox/client/vpn"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func mechanizeListLimitTwo() *int64 {
	limit := int64(2)
	return &limit
}

// netbox_tag
func mechanizeLookupTag(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := extras.NewExtrasTagsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Extras.ExtrasTagsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_tenant
func mechanizeLookupTenant(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := tenancy.NewTenancyTenantsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Tenancy.TenancyTenantsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_tenant_group
func mechanizeLookupTenantGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := tenancy.NewTenancyTenantGroupsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Tenancy.TenancyTenantGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_contact_role
func mechanizeLookupContactRole(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := tenancy.NewTenancyContactRolesListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Tenancy.TenancyContactRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_contact_group
func mechanizeLookupContactGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := tenancy.NewTenancyContactGroupsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Tenancy.TenancyContactGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_site
func mechanizeLookupSite(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimSitesListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimSitesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_site_group
func mechanizeLookupSiteGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimSiteGroupsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimSiteGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_region
func mechanizeLookupRegion(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimRegionsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimRegionsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_location
func mechanizeLookupLocation(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimLocationsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimLocationsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_platform
func mechanizeLookupPlatform(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimPlatformsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimPlatformsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_device_role
func mechanizeLookupDeviceRole(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimDeviceRolesListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimDeviceRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_manufacturer
func mechanizeLookupManufacturer(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimManufacturersListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimManufacturersList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_device_type
func mechanizeLookupDeviceType(api *providerState, d *schema.ResourceData) (int64, error) {
	model := d.Get("model").(string)
	params := dcim.NewDcimDeviceTypesListParams()
	params.Model = &model
	params.Limit = mechanizeListLimitTwo()
	if manufacturerID, ok := d.GetOk("manufacturer_id"); ok {
		manufacturerIDStr := strconv.Itoa(manufacturerID.(int))
		params.ManufacturerID = &manufacturerIDStr
	}
	res, err := api.Dcim.DcimDeviceTypesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_rack_role
func mechanizeLookupRackRole(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := dcim.NewDcimRackRolesListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimRackRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_cluster_type
func mechanizeLookupClusterType(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := virtualization.NewVirtualizationClusterTypesListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Virtualization.VirtualizationClusterTypesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_cluster_group
func mechanizeLookupClusterGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := virtualization.NewVirtualizationClusterGroupsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Virtualization.VirtualizationClusterGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_cluster
func mechanizeLookupCluster(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := virtualization.NewVirtualizationClustersListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Virtualization.VirtualizationClustersList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_vlan_group
func mechanizeLookupVlanGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := ipam.NewIpamVlanGroupsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamVlanGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_vlan
func mechanizeLookupVlan(api *providerState, d *schema.ResourceData) (int64, error) {
	vidStr := strconv.Itoa(d.Get("vid").(int))
	params := ipam.NewIpamVlansListParams()
	params.Vid = &vidStr
	params.Limit = mechanizeListLimitTwo()
	if groupID, ok := d.GetOk("group_id"); ok {
		groupIDStr := strconv.Itoa(groupID.(int))
		params.GroupID = &groupIDStr
	}
	res, err := api.Ipam.IpamVlansList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_ipam_role
func mechanizeLookupIpamRole(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := ipam.NewIpamRolesListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_route_target
func mechanizeLookupRouteTarget(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := ipam.NewIpamRouteTargetsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamRouteTargetsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_vrf
func mechanizeLookupVrf(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := ipam.NewIpamVrfsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamVrfsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_asn
func mechanizeLookupAsn(api *providerState, d *schema.ResourceData) (int64, error) {
	asnStr := strconv.Itoa(d.Get("asn").(int))
	params := ipam.NewIpamAsnsListParams()
	params.Asn = &asnStr
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamAsnsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_rir
func mechanizeLookupRir(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := ipam.NewIpamRirsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamRirsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_circuit_type
func mechanizeLookupCircuitType(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := circuits.NewCircuitsCircuitTypesListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Circuits.CircuitsCircuitTypesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_circuit_provider
func mechanizeLookupCircuitProvider(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := circuits.NewCircuitsProvidersListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Circuits.CircuitsProvidersList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_vpn_tunnel_group
func mechanizeLookupVpnTunnelGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := vpn.NewVpnTunnelGroupsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Vpn.VpnTunnelGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, fmt.Errorf("lookup failed")
	}
	return res.GetPayload().Results[0].ID, nil
}
