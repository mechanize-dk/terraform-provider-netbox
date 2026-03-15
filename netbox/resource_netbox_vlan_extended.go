package netbox

// MECHANIZE FORK: netbox_vlan_extended resource
// Based on netbox_vlan, adding:
//   - Explicit tenant_id documentation (already supported in upstream but undocumented here)
//   - Cascade delete: all prefixes referencing this VLAN are deleted before the VLAN itself

import (
	"fmt"
	"strconv"

	"github.com/fbreckle/go-netbox/netbox/client/ipam"
	"github.com/fbreckle/go-netbox/netbox/models"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

var resourceNetboxVlanExtendedStatusOptions = []string{"active", "reserved", "deprecated"}

func resourceNetboxVlanExtended() *schema.Resource {
	return &schema.Resource{
		Create: resourceNetboxVlanExtendedCreate,
		Read:   resourceNetboxVlanExtendedRead,
		Update: resourceNetboxVlanExtendedUpdate,
		Delete: resourceNetboxVlanExtendedDelete,

		Description: `:meta:subcategory:IP Address Management (IPAM):Extended version of netbox_vlan that adds support for tenant_id and cascade-deletes any prefixes referencing the VLAN on destroy.`,

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
			},
			"vid": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"status": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "active",
				ValidateFunc: validation.StringInSlice(resourceNetboxVlanExtendedStatusOptions, false),
				Description:  buildValidValueDescription(resourceNetboxVlanExtendedStatusOptions),
			},
			"group_id": {
				Type:     schema.TypeInt,
				Optional: true,
			},
			"tenant_id": {
				Type:        schema.TypeInt,
				Optional:    true,
				Description: "ID of the tenant to assign to the VLAN.",
			},
			"role_id": {
				Type:     schema.TypeInt,
				Optional: true,
			},
			"site_id": {
				Type:     schema.TypeInt,
				Optional: true,
			},
			"description": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "",
			},
			tagsKey: tagsSchema,
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
	}
}

func resourceNetboxVlanExtendedCreate(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)
	data := models.WritableVLAN{}

	name := d.Get("name").(string)
	vid := int64(d.Get("vid").(int))
	status := d.Get("status").(string)
	description := d.Get("description").(string)

	data.Name = &name
	data.Vid = &vid
	data.Status = status
	data.Description = description

	if groupID, ok := d.GetOk("group_id"); ok {
		data.Group = int64ToPtr(int64(groupID.(int)))
	}
	if siteID, ok := d.GetOk("site_id"); ok {
		data.Site = int64ToPtr(int64(siteID.(int)))
	}
	if tenantID, ok := d.GetOk("tenant_id"); ok {
		data.Tenant = int64ToPtr(int64(tenantID.(int)))
	}
	if roleID, ok := d.GetOk("role_id"); ok {
		data.Role = int64ToPtr(int64(roleID.(int)))
	}

	var err error
	data.Tags, err = getNestedTagListFromResourceDataSet(api, d.Get(tagsAllKey))
	if err != nil {
		return err
	}

	params := ipam.NewIpamVlansCreateParams().WithData(&data)
	res, err := api.Ipam.IpamVlansCreate(params, nil)
	if err != nil {
		if id, lookupErr := mechanizeLookupVlan(api, d); lookupErr == nil {
			d.SetId(strconv.FormatInt(id, 10))
			return resourceNetboxVlanExtendedRead(d, m)
		}
		return err
	}
	d.SetId(strconv.FormatInt(res.GetPayload().ID, 10))

	return resourceNetboxVlanExtendedRead(d, m)
}

func resourceNetboxVlanExtendedRead(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)
	id, _ := strconv.ParseInt(d.Id(), 10, 64)
	params := ipam.NewIpamVlansReadParams().WithID(id)

	res, err := api.Ipam.IpamVlansRead(params, nil)
	if err != nil {
		if errresp, ok := err.(*ipam.IpamVlansReadDefault); ok {
			if errresp.Code() == 404 {
				d.SetId("")
				return nil
			}
		}
		return err
	}

	vlan := res.GetPayload()
	d.Set("name", vlan.Name)
	d.Set("vid", vlan.Vid)
	d.Set("description", vlan.Description)
	api.readTags(d, vlan.Tags)

	if vlan.Status != nil {
		d.Set("status", vlan.Status.Value)
	}
	if vlan.Group != nil {
		d.Set("group_id", vlan.Group.ID)
	}
	if vlan.Site != nil {
		d.Set("site_id", vlan.Site.ID)
	}
	if vlan.Tenant != nil {
		d.Set("tenant_id", vlan.Tenant.ID)
	}
	if vlan.Role != nil {
		d.Set("role_id", vlan.Role.ID)
	}

	return nil
}

func resourceNetboxVlanExtendedUpdate(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)
	id, _ := strconv.ParseInt(d.Id(), 10, 64)
	data := models.WritableVLAN{}

	name := d.Get("name").(string)
	vid := int64(d.Get("vid").(int))
	status := d.Get("status").(string)
	description := d.Get("description").(string)

	data.Name = &name
	data.Vid = &vid
	data.Status = status
	data.Description = description

	if groupID, ok := d.GetOk("group_id"); ok {
		data.Group = int64ToPtr(int64(groupID.(int)))
	}
	if siteID, ok := d.GetOk("site_id"); ok {
		data.Site = int64ToPtr(int64(siteID.(int)))
	}
	if tenantID, ok := d.GetOk("tenant_id"); ok {
		data.Tenant = int64ToPtr(int64(tenantID.(int)))
	}
	if roleID, ok := d.GetOk("role_id"); ok {
		data.Role = int64ToPtr(int64(roleID.(int)))
	}

	var err error
	data.Tags, err = getNestedTagListFromResourceDataSet(api, d.Get(tagsAllKey))
	if err != nil {
		return err
	}

	params := ipam.NewIpamVlansUpdateParams().WithID(id).WithData(&data)
	_, err = api.Ipam.IpamVlansUpdate(params, nil)
	if err != nil {
		return err
	}
	return resourceNetboxVlanExtendedRead(d, m)
}

func resourceNetboxVlanExtendedDelete(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)
	id, _ := strconv.ParseInt(d.Id(), 10, 64)

	// MECHANIZE FORK: cascade-delete all prefixes referencing this VLAN
	if err := mechanizeDeletePrefixesByVlan(api, id); err != nil {
		return err
	}

	params := ipam.NewIpamVlansDeleteParams().WithID(id)
	_, err := api.Ipam.IpamVlansDelete(params, nil)
	if err != nil {
		if errresp, ok := err.(*ipam.IpamVlansDeleteDefault); ok {
			if errresp.Code() == 404 {
				d.SetId("")
				return nil
			}
		}
		return err
	}

	return nil
}

// mechanizeDeletePrefixesByVlan deletes all prefixes in NetBox that reference the given VLAN ID.
func mechanizeDeletePrefixesByVlan(api *providerState, vlanID int64) error {
	vlanIDStr := strconv.FormatInt(vlanID, 10)
	limit := int64(1000)

	listParams := ipam.NewIpamPrefixesListParams()
	listParams.VlanID = &vlanIDStr
	listParams.Limit = &limit

	res, err := api.Ipam.IpamPrefixesList(listParams, nil)
	if err != nil {
		return fmt.Errorf("listing prefixes for vlan %d: %w", vlanID, err)
	}

	for _, prefix := range res.GetPayload().Results {
		delParams := ipam.NewIpamPrefixesDeleteParams().WithID(prefix.ID)
		_, err := api.Ipam.IpamPrefixesDelete(delParams, nil)
		if err != nil {
			return fmt.Errorf("deleting prefix %d (vlan %d): %w", prefix.ID, vlanID, err)
		}
	}

	return nil
}
