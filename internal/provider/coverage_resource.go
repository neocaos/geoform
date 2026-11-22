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
	_ resource.Resource                = (*coverageResource)(nil)
	_ resource.ResourceWithConfigure   = (*coverageResource)(nil)
	_ resource.ResourceWithImportState = (*coverageResource)(nil)
)

type coverageResource struct {
	client *geoserver.Client
}

type coverageModel struct {
	ID            types.String `tfsdk:"id"`
	Workspace     types.String `tfsdk:"workspace"`
	CoverageStore types.String `tfsdk:"coveragestore"`
	Name          types.String `tfsdk:"name"`
	NativeName    types.String `tfsdk:"native_name"`
	Title         types.String `tfsdk:"title"`
	Abstract      types.String `tfsdk:"abstract"`
	SRS           types.String `tfsdk:"srs"`
	Enabled       types.Bool   `tfsdk:"enabled"`
}

// NewCoverageResource returns the geoserver_coverage resource.
func NewCoverageResource() resource.Resource {
	return &coverageResource{}
}

func (r *coverageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_coverage"
}

func (r *coverageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keepComputed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		Description: "A raster layer published from a coverage store. GeoServer creates the matching layer automatically.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Identifier in the form `<workspace>/<coveragestore>/<name>`.",
				Computed:      true,
				PlanModifiers: keepComputed,
			},
			"workspace": schema.StringAttribute{
				Description:   "Workspace of the coverage store. Changing it replaces the coverage.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators:    []validator.String{stringvalidator.RegexMatches(workspaceNameRegexp, "must be a valid workspace name")},
			},
			"coveragestore": schema.StringAttribute{
				Description:   "Coverage store holding the raster. Changing it replaces the coverage.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators:    []validator.String{stringvalidator.RegexMatches(noSlashRegexp, "must be non-empty and not contain '/'")},
			},
			"name": schema.StringAttribute{
				Description:   "Published layer name. Changing it replaces the coverage.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp,
						"must start with a letter or underscore and contain only letters, digits, '_', '-' and '.'"),
				},
			},
			"native_name": schema.StringAttribute{
				Description: "Name of the coverage inside the file (for a GeoTIFF, the file name without extension). " +
					"Defaults to `name`. Changing it replaces the coverage.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"title": schema.StringAttribute{
				Description:   "Human-readable title. GeoServer defaults it to the coverage name.",
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
				Description:   "Declared coordinate reference system, e.g. `EPSG:4326`. Defaults to the raster's native CRS.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keepComputed,
				Validators: []validator.String{
					stringvalidator.RegexMatches(srsRegexp, "must look like AUTHORITY:CODE, e.g. EPSG:4326"),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the coverage is enabled. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *coverageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *coverageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan coverageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cv := plan.toAPI()
	cv.NativeName = plan.NativeName.ValueString()
	if cv.NativeName == "" {
		cv.NativeName = cv.Name
	}
	cv.NativeCoverageName = cv.NativeName
	if err := r.client.CreateCoverage(ctx, plan.Workspace.ValueString(), plan.CoverageStore.ValueString(), cv); err != nil {
		resp.Diagnostics.AddError("Error creating coverage", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *coverageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state coverageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cv, err := r.client.GetCoverage(ctx, state.Workspace.ValueString(), state.CoverageStore.ValueString(), state.Name.ValueString())
	if geoserver.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading coverage", err.Error())
		return
	}
	state.fromAPI(cv)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *coverageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan coverageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.UpdateCoverage(ctx, plan.Workspace.ValueString(), plan.CoverageStore.ValueString(),
		plan.Name.ValueString(), plan.toAPI())
	if err != nil {
		resp.Diagnostics.AddError("Error updating coverage", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *coverageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state coverageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Recurse: GeoServer crea una capa por cada coverage y no borra una sin la otra.
	err := r.client.DeleteCoverage(ctx, state.Workspace.ValueString(), state.CoverageStore.ValueString(),
		state.Name.ValueString(), true)
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting coverage", err.Error())
	}
}

func (r *coverageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected \"<workspace>/<coveragestore>/<name>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("coveragestore"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[2])...)
}

func (r *coverageResource) refresh(ctx context.Context, m *coverageModel, diags *diag.Diagnostics) {
	cv, err := r.client.GetCoverage(ctx, m.Workspace.ValueString(), m.CoverageStore.ValueString(), m.Name.ValueString())
	if err != nil {
		diags.AddError("Error reading coverage after write", err.Error())
		return
	}
	m.fromAPI(cv)
}

func (m *coverageModel) toAPI() geoserver.Coverage {
	return geoserver.Coverage{
		Name:     m.Name.ValueString(),
		Title:    m.Title.ValueString(),
		Abstract: m.Abstract.ValueString(),
		SRS:      m.SRS.ValueString(),
		Enabled:  m.Enabled.ValueBool(),
	}
}

func (m *coverageModel) fromAPI(cv *geoserver.Coverage) {
	m.ID = types.StringValue(m.Workspace.ValueString() + "/" + m.CoverageStore.ValueString() + "/" + cv.Name)
	m.Name = types.StringValue(cv.Name)
	m.NativeName = types.StringValue(cv.NativeName)
	m.Title = types.StringValue(cv.Title)
	if cv.Abstract == "" {
		m.Abstract = types.StringNull()
	} else {
		m.Abstract = types.StringValue(cv.Abstract)
	}
	m.SRS = types.StringValue(cv.SRS)
	m.Enabled = types.BoolValue(cv.Enabled)
}
