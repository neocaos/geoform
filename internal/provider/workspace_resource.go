package provider

import (
	"context"
	"fmt"
	"regexp"

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

// A workspace name doubles as its namespace prefix, which must be an XML NCName.
var workspaceNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

var (
	_ resource.Resource                = (*workspaceResource)(nil)
	_ resource.ResourceWithConfigure   = (*workspaceResource)(nil)
	_ resource.ResourceWithImportState = (*workspaceResource)(nil)
)

type workspaceResource struct {
	client *geoserver.Client
}

type workspaceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Isolated types.Bool   `tfsdk:"isolated"`
}

// NewWorkspaceResource returns the geoserver_workspace resource.
func NewWorkspaceResource() resource.Resource {
	return &workspaceResource{}
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A GeoServer workspace. GeoServer creates the matching namespace automatically.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Identifier of the workspace; equal to its name.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the workspace, also used as its namespace prefix. Changing it replaces the workspace.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(workspaceNameRegexp,
						"must start with a letter or underscore and contain only letters, digits, '_', '-' and '.'"),
				},
			},
			"isolated": schema.BoolAttribute{
				Description: "Whether the workspace is isolated: its layers are only visible through workspace-specific services. Defaults to `false`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

func (r *workspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ws := geoserver.Workspace{Name: plan.Name.ValueString(), Isolated: plan.Isolated.ValueBool()}
	if err := r.client.CreateWorkspace(ctx, ws); err != nil {
		resp.Diagnostics.AddError("Error creating workspace", err.Error())
		return
	}

	r.readInto(ctx, ws.Name, &plan, resp.State.Set, &resp.Diagnostics)
}

func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ws, err := r.client.GetWorkspace(ctx, state.Name.ValueString())
	if geoserver.IsNotFound(err) {
		// Deleted outside Terraform: drop it from state so it gets recreated.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace", err.Error())
		return
	}

	state.fromAPI(ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// name forces replacement, so only in-place attributes reach here.
	ws := geoserver.Workspace{Name: plan.Name.ValueString(), Isolated: plan.Isolated.ValueBool()}
	if err := r.client.UpdateWorkspace(ctx, ws.Name, ws); err != nil {
		resp.Diagnostics.AddError("Error updating workspace", err.Error())
		return
	}

	r.readInto(ctx, ws.Name, &plan, resp.State.Set, &resp.Diagnostics)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Never recurse: a workspace that still holds stores or layers not managed
	// here must fail loudly rather than take that content with it.
	err := r.client.DeleteWorkspace(ctx, state.Name.ValueString(), false)
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting workspace", err.Error())
	}
}

func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// readInto re-reads the workspace after a write so state reflects what
// GeoServer actually stored, then saves it with set.
func (r *workspaceResource) readInto(ctx context.Context, name string, m *workspaceModel,
	set func(context.Context, any) diag.Diagnostics, diags *diag.Diagnostics) {
	ws, err := r.client.GetWorkspace(ctx, name)
	if err != nil {
		diags.AddError("Error reading workspace after write", err.Error())
		return
	}
	m.fromAPI(ws)
	diags.Append(set(ctx, m)...)
}

func (m *workspaceModel) fromAPI(ws *geoserver.Workspace) {
	m.ID = types.StringValue(ws.Name)
	m.Name = types.StringValue(ws.Name)
	m.Isolated = types.BoolValue(ws.Isolated)
}
