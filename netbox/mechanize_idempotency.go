package netbox

// MECHANIZE FORK: Idempotency (look-up-first)
//
// Goal: when Terraform tries to create an object that already exists in NetBox
// but is absent from Terraform state, adopt the existing object into state
// instead of creating a duplicate (or failing).
//
// Design — deliberately self-contained to keep upstream sync easy:
//
//   - ALL fork logic lives in this file. The generic wrapper below is applied
//     from a SINGLE line in provider.go (mechanizeAddIdempotency), so the
//     upstream resource_*.go files carry NO fork code and stay byte-identical
//     to upstream.
//
//   - The wrapper is "look-up-first": before the resource's own Create runs, we
//     look the object up by the key NetBox actually enforces unique. If exactly
//     one match exists, we adopt it (SetId + Read) and skip Create; otherwise we
//     run the original Create unchanged.
//
// Why not key on "name": NetBox does not enforce uniqueness on name for many
// models. The authoritative unique key is the slug (often derived from name and
// scoped by a parent), or a natural key (asn, vid+group, rd, ...). These were
// established empirically against a live NetBox — see
// tests/idempotency/probe_uniqueness.py. A lookup returns a match ONLY when the
// query yields exactly one object; 0, >1, or an API error all mean "not found",
// so the caller falls through to a normal Create.

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

// errMechanizeLookupFailed means "no single existing object to adopt" — the
// caller should proceed with a normal Create.
var errMechanizeLookupFailed = fmt.Errorf("no unique existing object found")

// mechanizeLookupFunc finds an existing object by its NetBox-enforced unique key.
type mechanizeLookupFunc func(api *providerState, d *schema.ResourceData) (int64, error)

// mechanizeLookups maps a resource type to the lookup used for adoption. Each key
// is the field(s) NetBox actually enforces unique (verified against a live NetBox).
var mechanizeLookups = map[string]mechanizeLookupFunc{
	// slug (globally unique)
	"netbox_tag":              mechanizeLookupTag,
	"netbox_tenant":           mechanizeLookupTenant,
	"netbox_tenant_group":     mechanizeLookupTenantGroup,
	"netbox_contact_role":     mechanizeLookupContactRole,
	"netbox_site":             mechanizeLookupSite,
	"netbox_site_group":       mechanizeLookupSiteGroup,
	"netbox_region":           mechanizeLookupRegion,
	"netbox_platform":         mechanizeLookupPlatform,
	"netbox_device_role":      mechanizeLookupDeviceRole,
	"netbox_manufacturer":     mechanizeLookupManufacturer,
	"netbox_rack_role":        mechanizeLookupRackRole,
	"netbox_cluster_type":     mechanizeLookupClusterType,
	"netbox_cluster_group":    mechanizeLookupClusterGroup,
	"netbox_ipam_role":        mechanizeLookupIpamRole,
	"netbox_rir":              mechanizeLookupRir,
	"netbox_circuit_type":     mechanizeLookupCircuitType,
	"netbox_circuit_provider": mechanizeLookupCircuitProvider,
	"netbox_vpn_tunnel_group": mechanizeLookupVpnTunnelGroup,
	// slug + parent/scope
	"netbox_location":      mechanizeLookupLocation,
	"netbox_contact_group": mechanizeLookupContactGroup,
	"netbox_vlan_group":    mechanizeLookupVlanGroup,
	"netbox_device_type":   mechanizeLookupDeviceType,
	// natural / other keys
	"netbox_cluster":      mechanizeLookupCluster,
	"netbox_vlan":         mechanizeLookupVlan,
	"netbox_asn":          mechanizeLookupAsn,
	"netbox_route_target": mechanizeLookupRouteTarget,
	"netbox_vrf":          mechanizeLookupVrf,
	// fork resources reuse the same natural keys
	"netbox_asn_extended":  mechanizeLookupAsn,
	"netbox_vlan_extended": mechanizeLookupVlan,
}

// mechanizeAddIdempotency wraps each registered resource's Create with
// look-up-first adoption. Called once from provider.go. All covered resources
// use non-context Create/Read (verified), so wrapping those fields is safe.
func mechanizeAddIdempotency(resources map[string]*schema.Resource) {
	//nolint:staticcheck // SA1019: upstream resources only set the deprecated Create/Read, so wrapping them must read those fields.
	for name, lookup := range mechanizeLookups {
		r, ok := resources[name]
		if !ok || r.Create == nil || r.Read == nil {
			continue
		}
		origCreate := r.Create
		read := r.Read
		lk := lookup
		r.Create = func(d *schema.ResourceData, m interface{}) error {
			api := m.(*providerState)
			if id, err := lk(api, d); err == nil {
				d.SetId(strconv.FormatInt(id, 10))
				return read(d, m)
			}
			return origCreate(d, m)
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func mechanizeListLimitTwo() *int64 {
	limit := int64(2)
	return &limit
}

// mechanizeEffectiveSlug returns the slug the resource's Create will use: the
// explicit slug if set, otherwise the slug getSlug() derives from sourceField
// (mirrors the Create functions, whose slug is Optional+Computed).
func mechanizeEffectiveSlug(d *schema.ResourceData, sourceField string) string {
	if v, ok := d.GetOk("slug"); ok {
		return v.(string)
	}
	return getSlug(d.Get(sourceField).(string))
}

// mechanizeIntFilter returns a *string for an int-valued optional filter, or nil.
func mechanizeIntFilter(d *schema.ResourceData, field string) *string {
	if v, ok := d.GetOk(field); ok {
		s := strconv.Itoa(v.(int))
		return &s
	}
	return nil
}

// ── lookups: slug (globally unique) ────────────────────────────────────────────

func mechanizeLookupTag(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := extras.NewExtrasTagsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Extras.ExtrasTagsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupTenant(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := tenancy.NewTenancyTenantsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Tenancy.TenancyTenantsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupTenantGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := tenancy.NewTenancyTenantGroupsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Tenancy.TenancyTenantGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupContactRole(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := tenancy.NewTenancyContactRolesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Tenancy.TenancyContactRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupSite(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimSitesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimSitesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupSiteGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimSiteGroupsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimSiteGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupRegion(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimRegionsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimRegionsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupPlatform(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimPlatformsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimPlatformsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupDeviceRole(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimDeviceRolesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimDeviceRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupManufacturer(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimManufacturersListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimManufacturersList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupRackRole(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimRackRolesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Dcim.DcimRackRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupClusterType(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := virtualization.NewVirtualizationClusterTypesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Virtualization.VirtualizationClusterTypesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupClusterGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := virtualization.NewVirtualizationClusterGroupsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Virtualization.VirtualizationClusterGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupIpamRole(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := ipam.NewIpamRolesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamRolesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupRir(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := ipam.NewIpamRirsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamRirsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupCircuitType(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := circuits.NewCircuitsCircuitTypesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Circuits.CircuitsCircuitTypesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupCircuitProvider(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := circuits.NewCircuitsProvidersListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Circuits.CircuitsProvidersList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

func mechanizeLookupVpnTunnelGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := vpn.NewVpnTunnelGroupsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Vpn.VpnTunnelGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// ── lookups: slug + parent/scope ────────────────────────────────────────────

// netbox_location — unique on (site, slug).
func mechanizeLookupLocation(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := dcim.NewDcimLocationsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	params.SiteID = mechanizeIntFilter(d, "site_id")
	res, err := api.Dcim.DcimLocationsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_contact_group — unique on (parent, slug).
func mechanizeLookupContactGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := tenancy.NewTenancyContactGroupsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	params.ParentID = mechanizeIntFilter(d, "parent_id")
	res, err := api.Tenancy.TenancyContactGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_vlan_group — unique on (scope, slug). With no scope NetBox allows
// duplicates; filtering by slug still adopts the single existing group on a
// state-less re-apply, which is the common case.
func mechanizeLookupVlanGroup(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "name")
	params := ipam.NewIpamVlanGroupsListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	if v, ok := d.GetOk("scope_type"); ok {
		s := v.(string)
		params.ScopeType = &s
	}
	params.ScopeID = mechanizeIntFilter(d, "scope_id")
	res, err := api.Ipam.IpamVlanGroupsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_device_type — unique on (manufacturer, slug); slug derives from model.
func mechanizeLookupDeviceType(api *providerState, d *schema.ResourceData) (int64, error) {
	slug := mechanizeEffectiveSlug(d, "model")
	params := dcim.NewDcimDeviceTypesListParams()
	params.Slug = &slug
	params.Limit = mechanizeListLimitTwo()
	params.ManufacturerID = mechanizeIntFilter(d, "manufacturer_id")
	res, err := api.Dcim.DcimDeviceTypesList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// ── lookups: natural / other keys ──────────────────────────────────────────

// netbox_cluster — unique on (group, name); no slug field.
func mechanizeLookupCluster(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := virtualization.NewVirtualizationClustersListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	params.GroupID = mechanizeIntFilter(d, "cluster_group_id")
	res, err := api.Virtualization.VirtualizationClustersList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_vlan — unique on (group, vid).
func mechanizeLookupVlan(api *providerState, d *schema.ResourceData) (int64, error) {
	vid := strconv.Itoa(d.Get("vid").(int))
	params := ipam.NewIpamVlansListParams()
	params.Vid = &vid
	params.Limit = mechanizeListLimitTwo()
	params.GroupID = mechanizeIntFilter(d, "group_id")
	res, err := api.Ipam.IpamVlansList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_asn — asn is globally unique.
func mechanizeLookupAsn(api *providerState, d *schema.ResourceData) (int64, error) {
	asn := strconv.Itoa(d.Get("asn").(int))
	params := ipam.NewIpamAsnsListParams()
	params.Asn = &asn
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamAsnsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_route_target — name is globally unique.
func mechanizeLookupRouteTarget(api *providerState, d *schema.ResourceData) (int64, error) {
	name := d.Get("name").(string)
	params := ipam.NewIpamRouteTargetsListParams()
	params.Name = &name
	params.Limit = mechanizeListLimitTwo()
	res, err := api.Ipam.IpamRouteTargetsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}

// netbox_vrf — rd (route distinguisher) is the unique key; name is not unique.
// Fall back to name only when no rd is configured (best effort).
func mechanizeLookupVrf(api *providerState, d *schema.ResourceData) (int64, error) {
	params := ipam.NewIpamVrfsListParams()
	params.Limit = mechanizeListLimitTwo()
	if v, ok := d.GetOk("rd"); ok {
		rd := v.(string)
		params.Rd = &rd
	} else {
		name := d.Get("name").(string)
		params.Name = &name
	}
	res, err := api.Ipam.IpamVrfsList(params, nil)
	if err != nil || *res.GetPayload().Count != 1 {
		return 0, errMechanizeLookupFailed
	}
	return res.GetPayload().Results[0].ID, nil
}
