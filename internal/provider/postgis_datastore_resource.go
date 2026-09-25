package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/neocaos/geoform/internal/geoserver"
)

// Connection parameter keys of GeoServer's PostGIS data store.
const (
	paramDBType   = "dbtype"
	paramHost     = "host"
	paramPort     = "port"
	paramDatabase = "database"
	paramSchema   = "schema"
	paramUser     = "user"
	paramPassword = "passwd"
)

var (
	_ resource.Resource                = (*postgisDataStoreResource)(nil)
	_ resource.ResourceWithConfigure   = (*postgisDataStoreResource)(nil)
	_ resource.ResourceWithImportState = (*postgisDataStoreResource)(nil)
)

type postgisDataStoreResource struct {
	client *geoserver.Client
}

type postgisDataStoreModel struct {
	ID          types.String `tfsdk:"id"`
	Workspace   types.String `tfsdk:"workspace"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	Host        types.String `tfsdk:"host"`
	Port        types.Int64  `tfsdk:"port"`
	Database    types.String `tfsdk:"database"`
	Schema      types.String `tfsdk:"schema"`
	User        types.String `tfsdk:"user"`
	Password    types.String `tfsdk:"password"`
}

// NewPostGISDataStoreResource returns the geoserver_postgis_datastore resource.
func NewPostGISDataStoreResource() resource.Resource {
	return &postgisDataStoreResource{}
}

func (r *postgisDataStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_postgis_datastore"
}

func (r *postgisDataStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	notEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}

	resp.Schema = schema.Schema{
		Description: "A GeoServer data store backed by a PostGIS database.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Identifier in the form `<workspace>/<name>`.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"workspace": schema.StringAttribute{
				Description:   "Workspace the store belongs to. Changing it replaces the store.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp, "must be a valid workspace name"),
				},
			},
			"name": schema.StringAttribute{
				Description:   "Name of the store. Changing it replaces the store.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(noSlashRegexp, "must be non-empty and not contain '/'"),
				},
			},
			"description": schema.StringAttribute{
				Description: "Free-text description of the store.",
				Optional:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the store is enabled. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"host": schema.StringAttribute{
				Description: "Database host, as reachable from the GeoServer server (not from where Terraform runs).",
				Required:    true,
				Validators:  notEmpty,
			},
			"port": schema.Int64Attribute{
				Description: "Database port. Defaults to `5432`.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(5432),
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"database": schema.StringAttribute{
				Description: "Database name.",
				Required:    true,
				Validators:  notEmpty,
			},
			"schema": schema.StringAttribute{
				Description: "Database schema. Defaults to `public`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("public"),
				Validators:  notEmpty,
			},
			"user": schema.StringAttribute{
				Description: "Database user.",
				Required:    true,
				Validators:  notEmpty,
			},
			"password": schema.StringAttribute{
				Description: "Database password. GeoServer only returns it encrypted, so changes made " +
					"outside Terraform are not detected, and it is empty after an import until set.",
				Required:  true,
				Sensitive: true,
			},
		},
	}
}

func (r *postgisDataStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*geoserver.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *geoserver.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *postgisDataStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan postgisDataStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ds := plan.toAPI(geoserver.ConnectionParameters{})
	if err := r.client.CreateDataStore(ctx, plan.Workspace.ValueString(), ds); err != nil {
		resp.Diagnostics.AddError("Error creating PostGIS data store", err.Error())
		return
	}

	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *postgisDataStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state postgisDataStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ds, err := r.client.GetDataStore(ctx, state.Workspace.ValueString(), state.Name.ValueString())
	if geoserver.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading PostGIS data store", err.Error())
		return
	}

	resp.Diagnostics.Append(state.fromAPI(ds)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *postgisDataStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan postgisDataStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	workspace, name := plan.Workspace.ValueString(), plan.Name.ValueString()

	// Start from the stored parameters so settings this resource does not
	// manage (pool sizes, namespace, ...) survive the full-replace PUT.
	current, err := r.client.GetDataStore(ctx, workspace, name)
	if err != nil {
		resp.Diagnostics.AddError("Error reading PostGIS data store before update", err.Error())
		return
	}

	if err := r.client.UpdateDataStore(ctx, workspace, name, plan.toAPI(current.ConnectionParameters)); err != nil {
		resp.Diagnostics.AddError("Error updating PostGIS data store", err.Error())
		return
	}

	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *postgisDataStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state postgisDataStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Never recurse: layers published from this store must be removed first.
	err := r.client.DeleteDataStore(ctx, state.Workspace.ValueString(), state.Name.ValueString(), false)
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting PostGIS data store", err.Error())
	}
}

func (r *postgisDataStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, name, ok := strings.Cut(req.ID, "/")
	if !ok || workspace == "" || name == "" || strings.Contains(name, "/") {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected \"<workspace>/<name>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

// refresh re-reads the store after a write so state reflects what GeoServer
// actually stored.
func (r *postgisDataStoreResource) refresh(ctx context.Context, m *postgisDataStoreModel, diags *diag.Diagnostics) {
	ds, err := r.client.GetDataStore(ctx, m.Workspace.ValueString(), m.Name.ValueString())
	if err != nil {
		diags.AddError("Error reading PostGIS data store after write", err.Error())
		return
	}
	diags.Append(m.fromAPI(ds)...)
}

// toAPI builds the request body, overlaying the managed connection
// parameters on base.
func (m *postgisDataStoreModel) toAPI(base geoserver.ConnectionParameters) geoserver.DataStore {
	params := make(geoserver.ConnectionParameters, len(base)+7)
	for k, v := range base {
		params[k] = v
	}
	params[paramDBType] = "postgis"
	params[paramHost] = m.Host.ValueString()
	params[paramPort] = strconv.FormatInt(m.Port.ValueInt64(), 10)
	params[paramDatabase] = m.Database.ValueString()
	params[paramSchema] = m.Schema.ValueString()
	params[paramUser] = m.User.ValueString()
	params[paramPassword] = m.Password.ValueString()

	return geoserver.DataStore{
		Name:                 m.Name.ValueString(),
		Description:          m.Description.ValueString(),
		Enabled:              m.Enabled.ValueBool(),
		ConnectionParameters: params,
	}
}

// fromAPI copies GeoServer's view of the store into m. The password is left
// untouched because GeoServer only returns it encrypted.
func (m *postgisDataStoreModel) fromAPI(ds *geoserver.DataStore) diag.Diagnostics {
	var diags diag.Diagnostics
	p := ds.ConnectionParameters

	if dbtype := p[paramDBType]; dbtype != "postgis" && dbtype != "postgisng" {
		diags.AddError("Not a PostGIS data store",
			fmt.Sprintf("Data store %q has dbtype %q (type %q); manage it with a matching resource.", ds.Name, dbtype, ds.Type))
		return diags
	}

	port, err := strconv.ParseInt(p[paramPort], 10, 64)
	if err != nil {
		diags.AddError("Unexpected port value", fmt.Sprintf("Data store %q has non-numeric port %q.", ds.Name, p[paramPort]))
		return diags
	}

	m.ID = types.StringValue(m.Workspace.ValueString() + "/" + ds.Name)
	m.Name = types.StringValue(ds.Name)
	if ds.Description == "" {
		m.Description = types.StringNull()
	} else {
		m.Description = types.StringValue(ds.Description)
	}
	m.Enabled = types.BoolValue(ds.Enabled)
	m.Host = types.StringValue(p[paramHost])
	m.Port = types.Int64Value(port)
	m.Database = types.StringValue(p[paramDatabase])
	m.Schema = types.StringValue(p[paramSchema])
	m.User = types.StringValue(p[paramUser])
	return diags
}
