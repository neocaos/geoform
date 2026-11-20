package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/neocaos/geoform/internal/geoserver"
)

var (
	_ resource.Resource                = (*featureTypeResource)(nil)
	_ resource.ResourceWithConfigure   = (*featureTypeResource)(nil)
	_ resource.ResourceWithImportState = (*featureTypeResource)(nil)
)

type featureTypeResource struct {
	client *geoserver.Client
}

type featureTypeModel struct {
	ID               types.String `tfsdk:"id"`
	Workspace        types.String `tfsdk:"workspace"`
	DataStore        types.String `tfsdk:"datastore"`
	Name             types.String `tfsdk:"name"`
	NativeName       types.String `tfsdk:"native_name"`
	Title            types.String `tfsdk:"title"`
	Abstract         types.String `tfsdk:"abstract"`
	SRS              types.String `tfsdk:"srs"`
	ProjectionPolicy types.String `tfsdk:"projection_policy"`
	Enabled          types.Bool   `tfsdk:"enabled"`
}

// NewFeatureTypeResource returns the geoserver_featuretype resource.
func NewFeatureTypeResource() resource.Resource {
	return &featureTypeResource{}
}

func (r *featureTypeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_featuretype"
}

func (r *featureTypeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	// Computed values GeoServer fills in: keep the known value across plans
	// instead of showing "(known after apply)" on every change.
	keepComputed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		Description: "A vector layer published from a data store table. GeoServer creates the matching layer automatically.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Identifier in the form `<workspace>/<datastore>/<name>`.",
				Computed:      true,
				PlanModifiers: keepComputed,
			},
			"workspace": schema.StringAttribute{
				Description:   "Workspace of the data store. Changing it replaces the feature type.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp, "must be a valid workspace name"),
				},
			},
			"datastore": schema.StringAttribute{
				Description:   "Data store holding the table. Changing it replaces the feature type.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(noSlashRegexp, "must be non-empty and not contain '/'"),
				},
			},
			"name": schema.StringAttribute{
				Description:   "Published layer name. Changing it replaces the feature type.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp,
						"must start with a letter or underscore and contain only letters, digits, '_', '-' and '.'"),
				},
			},
			"native_name": schema.StringAttribute{
				Description: "Table (or view) name in the data store. Defaults to `name`. Changing it replaces the feature type.",
				Optional:    true,
				Computed:    true,
				// UseStateForUnknown must run first so an unset value does not force replacement.
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"title": schema.StringAttribute{
				Description:   "Human-readable title. GeoServer defaults it to the table name (`native_name`).",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keepComputed,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"abstract": schema.StringAttribute{
				Description: "Longer description of the layer.",
				Optional:    true,
			},
			"srs": schema.StringAttribute{
				Description: "Declared coordinate reference system, e.g. `EPSG:4326`. Defaults to the table's native CRS. " +
					"Changing it recalculates the bounding boxes.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keepComputed,
				Validators: []validator.String{
					stringvalidator.RegexMatches(srsRegexp, "must look like AUTHORITY:CODE, e.g. EPSG:4326"),
				},
			},
			"projection_policy": schema.StringAttribute{
				Description: "How the declared `srs` relates to the native CRS: `FORCE_DECLARED`, " +
					"`REPROJECT_TO_DECLARED` or `NONE` (keep native). Chosen by GeoServer if unset.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keepComputed,
				Validators: []validator.String{
					stringvalidator.OneOf(geoserver.ProjectionForceDeclared,
						geoserver.ProjectionReprojectToDeclared, geoserver.ProjectionKeepNative),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the feature type is enabled. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *featureTypeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *featureTypeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan featureTypeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ft := plan.toAPI()
	ft.NativeName = plan.NativeName.ValueString()
	if ft.NativeName == "" {
		ft.NativeName = ft.Name
	}
	if err := r.client.CreateFeatureType(ctx, plan.Workspace.ValueString(), plan.DataStore.ValueString(), ft); err != nil {
		resp.Diagnostics.AddError("Error creating feature type", err.Error())
		return
	}

	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *featureTypeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state featureTypeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ft, err := r.client.GetFeatureType(ctx, state.Workspace.ValueString(), state.DataStore.ValueString(), state.Name.ValueString())
	if geoserver.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading feature type", err.Error())
		return
	}

	state.fromAPI(ft)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *featureTypeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state featureTypeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Bounding boxes depend on the CRS handling; recompute them only when that
	// changes, so manually tuned extents are otherwise left alone.
	recalculate := !plan.SRS.Equal(state.SRS) || !plan.ProjectionPolicy.Equal(state.ProjectionPolicy)

	err := r.client.UpdateFeatureType(ctx, plan.Workspace.ValueString(), plan.DataStore.ValueString(),
		plan.Name.ValueString(), plan.toAPI(), recalculate)
	if err != nil {
		resp.Diagnostics.AddError("Error updating feature type", err.Error())
		return
	}

	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *featureTypeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state featureTypeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Recurse is required: GeoServer creates a layer for every feature type
	// and will not delete one without the other. It also drops the layer from
	// any layer groups that reference it.
	err := r.client.DeleteFeatureType(ctx, state.Workspace.ValueString(), state.DataStore.ValueString(),
		state.Name.ValueString(), true)
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting feature type", err.Error())
	}
}

func (r *featureTypeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected \"<workspace>/<datastore>/<name>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("datastore"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[2])...)
}

// refresh re-reads the feature type after a write so state reflects what
// GeoServer actually stored, including the values it computed.
func (r *featureTypeResource) refresh(ctx context.Context, m *featureTypeModel, diags *diag.Diagnostics) {
	ft, err := r.client.GetFeatureType(ctx, m.Workspace.ValueString(), m.DataStore.ValueString(), m.Name.ValueString())
	if err != nil {
		diags.AddError("Error reading feature type after write", err.Error())
		return
	}
	m.fromAPI(ft)
}

// toAPI builds the fields sent on create and update. Unknown computed values
// are sent empty, which leaves GeoServer to choose them.
func (m *featureTypeModel) toAPI() geoserver.FeatureType {
	return geoserver.FeatureType{
		Name:             m.Name.ValueString(),
		Title:            m.Title.ValueString(),
		Abstract:         m.Abstract.ValueString(),
		SRS:              m.SRS.ValueString(),
		ProjectionPolicy: m.ProjectionPolicy.ValueString(),
		Enabled:          m.Enabled.ValueBool(),
	}
}

func (m *featureTypeModel) fromAPI(ft *geoserver.FeatureType) {
	m.ID = types.StringValue(m.Workspace.ValueString() + "/" + m.DataStore.ValueString() + "/" + ft.Name)
	m.Name = types.StringValue(ft.Name)
	m.NativeName = types.StringValue(ft.NativeName)
	m.Title = types.StringValue(ft.Title)
	if ft.Abstract == "" {
		m.Abstract = types.StringNull()
	} else {
		m.Abstract = types.StringValue(ft.Abstract)
	}
	m.SRS = types.StringValue(ft.SRS)
	m.ProjectionPolicy = types.StringValue(ft.ProjectionPolicy)
	m.Enabled = types.BoolValue(ft.Enabled)
}
