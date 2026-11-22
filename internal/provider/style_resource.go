package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/geoserver"
)

var (
	_ resource.Resource                = (*styleResource)(nil)
	_ resource.ResourceWithConfigure   = (*styleResource)(nil)
	_ resource.ResourceWithImportState = (*styleResource)(nil)
)

type styleResource struct {
	client *geoserver.Client
}

type styleModel struct {
	ID        types.String `tfsdk:"id"`
	Workspace types.String `tfsdk:"workspace"`
	Name      types.String `tfsdk:"name"`
	SLD       types.String `tfsdk:"sld"`
}

// NewStyleResource returns the geoserver_style resource.
func NewStyleResource() resource.Resource { return &styleResource{} }

func (r *styleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_style"
}

func (r *styleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "An SLD style scoped to a workspace. Assign it to a layer with the `default_style` attribute of `geoserver_featuretype`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Identifier in the form `<workspace>/<name>`.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"workspace": schema.StringAttribute{
				Description:   "Workspace owning the style. Changing it replaces the style.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp, "must be a valid workspace name"),
				},
			},
			"name": schema.StringAttribute{
				Description:   "Style name. Changing it replaces the style.",
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp,
						"must start with a letter or underscore and contain only letters, digits, '_', '-' and '.'"),
				},
			},
			"sld": schema.StringAttribute{
				Description: "Full SLD document, e.g. `file(\"style.sld\")`. Leading and trailing whitespace is ignored when comparing with GeoServer.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *styleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *styleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan styleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateStyle(ctx, plan.Workspace.ValueString(), plan.Name.ValueString(), plan.SLD.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error creating style", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.Workspace.ValueString() + "/" + plan.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *styleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state styleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sld, err := r.client.GetStyleSLD(ctx, state.Workspace.ValueString(), state.Name.ValueString())
	if geoserver.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading style", err.Error())
		return
	}
	// Se conserva el texto del estado si solo difiere en espacios en los
	// extremos, para no mostrar cambios espurios.
	if !style.SameSLD(sld, state.SLD.ValueString()) {
		state.SLD = types.StringValue(sld)
	}
	state.ID = types.StringValue(state.Workspace.ValueString() + "/" + state.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *styleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan styleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateStyle(ctx, plan.Workspace.ValueString(), plan.Name.ValueString(), plan.SLD.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error updating style", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.Workspace.ValueString() + "/" + plan.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *styleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state styleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// recurse retira el estilo de las capas que lo usan.
	err := r.client.DeleteStyle(ctx, state.Workspace.ValueString(), state.Name.ValueString(), true)
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting style", err.Error())
	}
}

func (r *styleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected \"<workspace>/<name>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
}
