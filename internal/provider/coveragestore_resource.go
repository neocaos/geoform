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
	_ resource.Resource                = (*geoTIFFCoverageStoreResource)(nil)
	_ resource.ResourceWithConfigure   = (*geoTIFFCoverageStoreResource)(nil)
	_ resource.ResourceWithImportState = (*geoTIFFCoverageStoreResource)(nil)
)

type geoTIFFCoverageStoreResource struct {
	client *geoserver.Client
}

type coverageStoreModel struct {
	ID          types.String `tfsdk:"id"`
	Workspace   types.String `tfsdk:"workspace"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	URL         types.String `tfsdk:"url"`
}

// NewGeoTIFFCoverageStoreResource returns the geoserver_geotiff_coveragestore resource.
func NewGeoTIFFCoverageStoreResource() resource.Resource {
	return &geoTIFFCoverageStoreResource{}
}

func (r *geoTIFFCoverageStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_geotiff_coveragestore"
}

func (r *geoTIFFCoverageStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A GeoServer coverage store (raster source) backed by a GeoTIFF file.",
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
				Validators:    []validator.String{stringvalidator.RegexMatches(workspaceNameRegexp, "must be a valid workspace name")},
			},
			"name": schema.StringAttribute{
				Description:   "Name of the store. Changing it replaces the store.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators:    []validator.String{stringvalidator.RegexMatches(noSlashRegexp, "must be non-empty and not contain '/'")},
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
			"url": schema.StringAttribute{
				Description: "GeoServer URL of the `.tif` file, as seen by the GeoServer server (not by Terraform), " +
					"e.g. `file:/data/dem.tif` or `file:data/dem.tif` (relative to the data directory).",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *geoTIFFCoverageStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *geoTIFFCoverageStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan coverageStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateCoverageStore(ctx, plan.Workspace.ValueString(), plan.toAPI()); err != nil {
		resp.Diagnostics.AddError("Error creating GeoTIFF coverage store", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *geoTIFFCoverageStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state coverageStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cs, err := r.client.GetCoverageStore(ctx, state.Workspace.ValueString(), state.Name.ValueString())
	if geoserver.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading GeoTIFF coverage store", err.Error())
		return
	}
	resp.Diagnostics.Append(state.fromAPI(cs)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *geoTIFFCoverageStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan coverageStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateCoverageStore(ctx, plan.Workspace.ValueString(), plan.Name.ValueString(), plan.toAPI()); err != nil {
		resp.Diagnostics.AddError("Error updating GeoTIFF coverage store", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *geoTIFFCoverageStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state coverageStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Sin recurse: las coverages publicadas desde el store deben borrarse antes.
	// El fichero nunca se purga.
	err := r.client.DeleteCoverageStore(ctx, state.Workspace.ValueString(), state.Name.ValueString(), false)
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting GeoTIFF coverage store", err.Error())
	}
}

func (r *geoTIFFCoverageStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, name, ok := strings.Cut(req.ID, "/")
	if !ok || workspace == "" || name == "" || strings.Contains(name, "/") {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected \"<workspace>/<name>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

func (r *geoTIFFCoverageStoreResource) refresh(ctx context.Context, m *coverageStoreModel, diags *diag.Diagnostics) {
	cs, err := r.client.GetCoverageStore(ctx, m.Workspace.ValueString(), m.Name.ValueString())
	if err != nil {
		diags.AddError("Error reading GeoTIFF coverage store after write", err.Error())
		return
	}
	diags.Append(m.fromAPI(cs)...)
}

func (m *coverageStoreModel) toAPI() geoserver.CoverageStore {
	return geoserver.CoverageStore{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueString(),
		Type:        geoserver.CoverageStoreTypeGeoTIFF,
		Enabled:     m.Enabled.ValueBool(),
		URL:         m.URL.ValueString(),
	}
}

func (m *coverageStoreModel) fromAPI(cs *geoserver.CoverageStore) diag.Diagnostics {
	var diags diag.Diagnostics
	if cs.Type != geoserver.CoverageStoreTypeGeoTIFF {
		diags.AddError("Not a GeoTIFF coverage store",
			fmt.Sprintf("Coverage store %q has type %q; manage it with a matching resource.", cs.Name, cs.Type))
		return diags
	}
	m.ID = types.StringValue(m.Workspace.ValueString() + "/" + cs.Name)
	m.Name = types.StringValue(cs.Name)
	if cs.Description == "" {
		m.Description = types.StringNull()
	} else {
		m.Description = types.StringValue(cs.Description)
	}
	m.Enabled = types.BoolValue(cs.Enabled)
	m.URL = types.StringValue(cs.URL)
	return diags
}
