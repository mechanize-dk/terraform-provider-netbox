package netbox

// MECHANIZE FORK: netbox_asn_extended resource
// Based on netbox_asn, adding support for the tenant_id parameter which
// is available in the NetBox API but missing from the upstream resource.

import (
	"strconv"

	"github.com/fbreckle/go-netbox/netbox/client/ipam"
	"github.com/fbreckle/go-netbox/netbox/models"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceNetboxAsnExtended() *schema.Resource {
	return &schema.Resource{
		Create: resourceNetboxAsnExtendedCreate,
		Read:   resourceNetboxAsnExtendedRead,
		Update: resourceNetboxAsnExtendedUpdate,
		Delete: resourceNetboxAsnExtendedDelete,

		Description: `:meta:subcategory:IP Address Management (IPAM):Extended version of netbox_asn that adds support for the tenant_id parameter.`,

		Schema: map[string]*schema.Schema{
			"asn": {
				Type:        schema.TypeInt,
				Required:    true,
				Description: "Value for the AS Number record",
			},
			"rir_id": {
				Type:        schema.TypeInt,
				Required:    true,
				Description: "ID for the RIR for the AS Number record",
			},
			"tenant_id": {
				Type:        schema.TypeInt,
				Optional:    true,
				Description: "ID of the tenant to assign to the AS Number record",
			},
			"description": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Description field for the AS Number record",
			},
			"comments": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Comments field for the AS Number record",
			},
			tagsKey: tagsSchema,
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
	}
}

func resourceNetboxAsnExtendedCreate(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)

	data := models.WritableASN{}

	asn := int64(d.Get("asn").(int))
	data.Asn = &asn

	rir := int64(d.Get("rir_id").(int))
	data.Rir = &rir

	if tenantID, ok := d.GetOk("tenant_id"); ok {
		tenantIDInt64 := int64(tenantID.(int))
		data.Tenant = &tenantIDInt64
	}

	data.Description = d.Get("description").(string)
	data.Comments = d.Get("comments").(string)
	var err error
	data.Tags, err = getNestedTagListFromResourceDataSet(api, d.Get(tagsAllKey))
	if err != nil {
		return err
	}

	params := ipam.NewIpamAsnsCreateParams().WithData(&data)

	res, err := api.Ipam.IpamAsnsCreate(params, nil)
	if err != nil {
		if id, lookupErr := mechanizeLookupAsn(api, d); lookupErr == nil {
			d.SetId(strconv.FormatInt(id, 10))
			return resourceNetboxAsnExtendedRead(d, m)
		}
		return err
	}

	d.SetId(strconv.FormatInt(res.GetPayload().ID, 10))

	return resourceNetboxAsnExtendedRead(d, m)
}

func resourceNetboxAsnExtendedRead(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)
	id, _ := strconv.ParseInt(d.Id(), 10, 64)
	params := ipam.NewIpamAsnsReadParams().WithID(id)

	res, err := api.Ipam.IpamAsnsRead(params, nil)

	if err != nil {
		if errresp, ok := err.(*ipam.IpamAsnsReadDefault); ok {
			errorcode := errresp.Code()
			if errorcode == 404 {
				d.SetId("")
				return nil
			}
		}
		return err
	}

	asn := res.GetPayload()
	d.Set("asn", asn.Asn)
	d.Set("rir_id", asn.Rir.ID)
	if asn.Tenant != nil {
		d.Set("tenant_id", asn.Tenant.ID)
	} else {
		d.Set("tenant_id", nil)
	}
	d.Set("description", asn.Description)
	d.Set("comments", asn.Comments)
	api.readTags(d, asn.Tags)

	return nil
}

func resourceNetboxAsnExtendedUpdate(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)

	id, _ := strconv.ParseInt(d.Id(), 10, 64)
	data := models.WritableASN{}

	asn := int64(d.Get("asn").(int))
	data.Asn = &asn

	rir := int64(d.Get("rir_id").(int))
	data.Rir = &rir

	if tenantID, ok := d.GetOk("tenant_id"); ok {
		tenantIDInt64 := int64(tenantID.(int))
		data.Tenant = &tenantIDInt64
	}

	data.Description = d.Get("description").(string)
	data.Comments = d.Get("comments").(string)
	var err error
	data.Tags, err = getNestedTagListFromResourceDataSet(api, d.Get(tagsAllKey))
	if err != nil {
		return err
	}

	params := ipam.NewIpamAsnsUpdateParams().WithID(id).WithData(&data)

	_, err = api.Ipam.IpamAsnsUpdate(params, nil)
	if err != nil {
		return err
	}

	return resourceNetboxAsnExtendedRead(d, m)
}

func resourceNetboxAsnExtendedDelete(d *schema.ResourceData, m interface{}) error {
	api := m.(*providerState)

	id, _ := strconv.ParseInt(d.Id(), 10, 64)
	params := ipam.NewIpamAsnsDeleteParams().WithID(id)

	_, err := api.Ipam.IpamAsnsDelete(params, nil)
	if err != nil {
		if errresp, ok := err.(*ipam.IpamAsnsDeleteDefault); ok {
			if errresp.Code() == 404 {
				d.SetId("")
				return nil
			}
		}
		return err
	}
	return nil
}
