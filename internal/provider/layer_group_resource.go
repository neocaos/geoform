package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/geoserver"
)

var (
	_ resource.Resource                = (*layerGroupResource)(nil)
	_ resource.ResourceWithConfigure   = (*layerGroupResource)(nil)
	_ resource.ResourceWithImportState = (*layerGroupResource)(nil)
)

type layerGroupResource struct {
	client *geoserver.Client
}

type layerGroupModel struct {
	ID        types.String `tfsdk:"id"`
	Workspace types.String `tfsdk:"workspace"`
	Name      types.String `tfsdk:"name"`
	Title     types.String `tfsdk:"title"`
	Mode      types.String `tfsdk:"mode"`
	Layers    types.List   `tfsdk:"layers"`
}

// NewLayerGroupResource returns the geoserver_layer_group resource.
func NewLayerGroupResource() resource.Resource { return &layerGroupResource{} }

func (r *layerGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_layer_group"
}

func (r *layerGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A layer group scoped to a workspace. Its bounds are computed by GeoServer from the member layers.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Identifier in the form `<workspace>/<name>`.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"workspace": schema.StringAttribute{
				Description:   "Workspace owning the group. Changing it replaces the group.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp, "must be a valid workspace name"),
				},
			},
			"name": schema.StringAttribute{
				Description:   "Group name. Changing it replaces the group.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp,
						"must start with a letter or underscore and contain only letters, digits, '_', '-' and '.'"),
				},
			},
			"title": schema.StringAttribute{
				Description:   "Human-readable title.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"mode": schema.StringAttribute{
				Description: "Group mode: `SINGLE` (default), `OPAQUE_CONTAINER`, `NAMED`, `CONTAINER` or `EO`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(layergroup.ModeSingle),
				Validators: []validator.String{stringvalidator.OneOf(layergroup.ModeSingle,
					layergroup.ModeOpaqueContainer, layergroup.ModeNamed, layergroup.ModeContainer, layergroup.ModeEO)},
			},
			"layers": schema.ListAttribute{
				Description: "Names of the member layers of the same workspace, in drawing order (first is drawn at the bottom).",
				Required:    true,
				ElementType: types.StringType,
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
			},
		},
	}
}

func (r *layerGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (m *layerGroupModel) toAPI(ctx context.Context, diags *diag.Diagnostics) geoserver.LayerGroup {
	var names []string
	diags.Append(m.Layers.ElementsAs(ctx, &names, false)...)
	layers := make([]string, 0, len(names))
	for _, n := range names {
		layers = append(layers, m.Workspace.ValueString()+":"+n)
	}
	return geoserver.LayerGroup{Name: m.Name.ValueString(), Title: m.Title.ValueString(), Mode: m.Mode.ValueString(), Layers: layers}
}

func (m *layerGroupModel) fromAPI(g *geoserver.LayerGroup) {
	m.ID = types.StringValue(m.Workspace.ValueString() + "/" + g.Name)
	m.Name = types.StringValue(g.Name)
	m.Title = types.StringValue(g.Title)
	m.Mode = types.StringValue(g.Mode)
	elems := make([]string, 0, len(g.Layers))
	for _, l := range g.Layers {
		elems = append(elems, strings.TrimPrefix(l, m.Workspace.ValueString()+":"))
	}
	list, _ := types.ListValueFrom(context.Background(), types.StringType, elems)
	m.Layers = list
}

func (r *layerGroupResource) refresh(ctx context.Context, m *layerGroupModel, diags *diag.Diagnostics) {
	g, err := r.client.GetLayerGroup(ctx, m.Workspace.ValueString(), m.Name.ValueString())
	if err != nil {
		diags.AddError("Error reading layer group after write", err.Error())
		return
	}
	m.fromAPI(g)
}

func (r *layerGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan layerGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	g := plan.toAPI(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateLayerGroup(ctx, plan.Workspace.ValueString(), g); err != nil {
		resp.Diagnostics.AddError("Error creating layer group", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *layerGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state layerGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	g, err := r.client.GetLayerGroup(ctx, state.Workspace.ValueString(), state.Name.ValueString())
	if geoserver.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading layer group", err.Error())
		return
	}
	state.fromAPI(g)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *layerGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan layerGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	g := plan.toAPI(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateLayerGroup(ctx, plan.Workspace.ValueString(), plan.Name.ValueString(), g); err != nil {
		resp.Diagnostics.AddError("Error updating layer group", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *layerGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state layerGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteLayerGroup(ctx, state.Workspace.ValueString(), state.Name.ValueString())
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting layer group", err.Error())
	}
}

func (r *layerGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected \"<workspace>/<name>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
}
