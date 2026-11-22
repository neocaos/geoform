package provider

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	roleNameRegexp = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	userNameRegexp = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]*$`)
	// Rule segments are joined with '.', so they cannot contain one.
	ruleSegmentRegexp = regexp.MustCompile(`^(\*|[A-Za-z_][A-Za-z0-9_-]*)$`)
)

func configureClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *geoserver.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*geoserver.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *geoserver.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return nil
	}
	return client
}

func setToSorted(ctx context.Context, s types.Set) []string {
	var out []string
	if !s.IsNull() && !s.IsUnknown() {
		s.ElementsAs(ctx, &out, false)
	}
	sort.Strings(out)
	return out
}

func sortedToSet(v []string) types.Set {
	sort.Strings(v)
	elems := make([]attr.Value, 0, len(v))
	for _, s := range v {
		elems = append(elems, types.StringValue(s))
	}
	set, _ := types.SetValue(types.StringType, elems)
	return set
}

// ---------------------------------------------------------------- role

var (
	_ resource.Resource                = (*roleResource)(nil)
	_ resource.ResourceWithConfigure   = (*roleResource)(nil)
	_ resource.ResourceWithImportState = (*roleResource)(nil)
)

type roleResource struct{ client *geoserver.Client }

type roleModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

// NewRoleResource returns the geoserver_security_role resource.
func NewRoleResource() resource.Resource { return &roleResource{} }

func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_role"
}

func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A GeoServer security role.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Identifier of the role; equal to its name.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "Name of the role, e.g. `ROLE_EDITOR`. Changing it replaces the role.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.RegexMatches(roleNameRegexp,
					"must start with a letter and contain only letters, digits and '_'")},
			},
		},
	}
}

func (r *roleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c := configureClient(req, resp); c != nil {
		r.client = c
	}
}

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	if err := r.client.CreateRole(ctx, name); err != nil {
		resp.Diagnostics.AddError("Error creating role", err.Error())
		return
	}
	plan.ID = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	roles, err := r.client.ListRoles(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading role", err.Error())
		return
	}
	for _, n := range roles {
		if n == state.Name.ValueString() {
			state.ID = types.StringValue(n)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

// Update is never reached: name is the only attribute and forces replacement.
func (r *roleResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteRole(ctx, state.Name.ValueString()); err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting role", err.Error())
	}
}

func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// ---------------------------------------------------------------- user

var (
	_ resource.Resource                = (*userResource)(nil)
	_ resource.ResourceWithConfigure   = (*userResource)(nil)
	_ resource.ResourceWithImportState = (*userResource)(nil)
)

type userResource struct{ client *geoserver.Client }

type userModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Password types.String `tfsdk:"password"`
	Enabled  types.Bool   `tfsdk:"enabled"`
	Roles    types.Set    `tfsdk:"roles"`
}

// NewUserResource returns the geoserver_security_user resource.
func NewUserResource() resource.Resource { return &userResource{} }

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A GeoServer user (default user/group service) and its role assignments.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Identifier of the user; equal to its name.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "User name. Changing it replaces the user.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{stringvalidator.RegexMatches(userNameRegexp,
					"must contain only letters, digits, '_', '-', '.' and '@'")},
			},
			"password": schema.StringAttribute{
				Description: "Password. Sensitive; pass it from a sensitive variable or environment, never as a literal. " +
					"GeoServer never returns it, so changes made outside Terraform are not detected.",
				Required:  true,
				Sensitive: true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the user can log in. Defaults to `true`.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"roles": schema.SetAttribute{
				Description: "Roles assigned to the user. Roles are not created implicitly: manage them with `geoserver_security_role` (or use ones that already exist).",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
		},
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c := configureClient(req, resp); c != nil {
		r.client = c
	}
}

// sync makes GeoServer match m: creates or updates the user, then diffs roles.
func (r *userResource) sync(ctx context.Context, m *userModel, create bool) error {
	u := geoserver.User{Name: m.Name.ValueString(), Password: m.Password.ValueString(), Enabled: m.Enabled.ValueBool()}
	var have []string
	if create {
		if err := r.client.CreateUser(ctx, u); err != nil {
			return err
		}
	} else {
		if err := r.client.UpdateUser(ctx, u); err != nil {
			return err
		}
		var err error
		if have, err = r.client.UserRoles(ctx, u.Name); err != nil {
			return err
		}
	}
	want := setToSorted(ctx, m.Roles)
	if m.Roles.IsUnknown() { // not configured: leave existing assignments as they are
		want = have
	}
	has, wants := map[string]bool{}, map[string]bool{}
	for _, x := range have {
		has[x] = true
	}
	for _, x := range want {
		wants[x] = true
		if !has[x] {
			if err := r.client.AssignRole(ctx, u.Name, x); err != nil {
				return err
			}
		}
	}
	for _, x := range have {
		if !wants[x] {
			if err := r.client.UnassignRole(ctx, u.Name, x); err != nil {
				return err
			}
		}
	}
	return nil
}

// refresh reads enabled and roles back from GeoServer (the password is kept).
func (r *userResource) refresh(ctx context.Context, m *userModel) error {
	u, err := r.client.GetUser(ctx, m.Name.ValueString())
	if err != nil {
		return err
	}
	roles, err := r.client.UserRoles(ctx, u.Name)
	if err != nil {
		return err
	}
	m.ID = types.StringValue(u.Name)
	m.Enabled = types.BoolValue(u.Enabled)
	m.Roles = sortedToSet(roles)
	return nil
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.sync(ctx, &plan, true); err != nil {
		resp.Diagnostics.AddError("Error creating user", err.Error())
		return
	}
	if err := r.refresh(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error reading user after write", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.refresh(ctx, &state); err != nil {
		if geoserver.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading user", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.sync(ctx, &plan, false); err != nil {
		resp.Diagnostics.AddError("Error updating user", err.Error())
		return
	}
	if err := r.refresh(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error reading user after write", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteUser(ctx, state.Name.ValueString()); err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting user", err.Error())
	}
}

// ImportState imports by user name. The password is not importable: set it in
// configuration and the next apply writes it.
func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// ---------------------------------------------------------------- layer rule

var (
	_ resource.Resource                = (*layerRuleResource)(nil)
	_ resource.ResourceWithConfigure   = (*layerRuleResource)(nil)
	_ resource.ResourceWithImportState = (*layerRuleResource)(nil)
)

type layerRuleResource struct{ client *geoserver.Client }

type layerRuleModel struct {
	ID        types.String `tfsdk:"id"`
	Workspace types.String `tfsdk:"workspace"`
	Layer     types.String `tfsdk:"layer"`
	Access    types.String `tfsdk:"access"`
	Roles     types.Set    `tfsdk:"roles"`
}

// NewLayerRuleResource returns the geoserver_security_layer_rule resource.
func NewLayerRuleResource() resource.Resource { return &layerRuleResource{} }

func (r *layerRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_layer_rule"
}

func (r *layerRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A data access rule: which roles can read or write a layer, or every layer of a workspace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "GeoServer rule key, `<workspace>.<layer>.<r|w>`.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"workspace": schema.StringAttribute{
				Description: "Workspace the rule applies to, or `*` for all. Changing it replaces the rule.",
				Required:    true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.RegexMatches(ruleSegmentRegexp, "must be `*` or a name without '.'")},
			},
			"layer": schema.StringAttribute{
				Description: "Layer the rule applies to, or `*` for all the workspace's layers. Changing it replaces the rule.",
				Required:    true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.RegexMatches(ruleSegmentRegexp, "must be `*` or a name without '.'")},
			},
			"access": schema.StringAttribute{
				Description: "`read` or `write`. Changing it replaces the rule.",
				Required:    true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.OneOf("read", "write")},
			},
			"roles": schema.SetAttribute{
				Description: "Roles granted the access; `*` means everyone.",
				ElementType: types.StringType,
				Required:    true,
			},
		},
	}
}

func (r *layerRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c := configureClient(req, resp); c != nil {
		r.client = c
	}
}

func ruleKey(m layerRuleModel) string {
	mode := "r"
	if m.Access.ValueString() == "write" {
		mode = "w"
	}
	return m.Workspace.ValueString() + "." + m.Layer.ValueString() + "." + mode
}

func (r *layerRuleResource) write(ctx context.Context, m *layerRuleModel, update bool) error {
	key, roles := ruleKey(*m), setToSorted(ctx, m.Roles)
	var err error
	if update {
		err = r.client.UpdateLayerRule(ctx, key, roles)
	} else {
		err = r.client.CreateLayerRule(ctx, key, roles)
	}
	if err != nil {
		return err
	}
	m.ID = types.StringValue(key)
	return nil
}

func (r *layerRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan layerRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan, false); err != nil {
		resp.Diagnostics.AddError("Error creating layer rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *layerRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state layerRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rules, err := r.client.LayerRules(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading layer rule", err.Error())
		return
	}
	key := ruleKey(state)
	val, ok := rules[key]
	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = types.StringValue(key)
	state.Roles = sortedToSet(splitRoles(val))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *layerRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan layerRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan, true); err != nil {
		resp.Diagnostics.AddError("Error updating layer rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *layerRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state layerRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteLayerRule(ctx, ruleKey(state)); err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting layer rule", err.Error())
	}
}

// ImportState imports by rule key: `<workspace>.<layer>.<r|w>`.
func (r *layerRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ".")
	if len(parts) != 3 || (parts[2] != "r" && parts[2] != "w") {
		resp.Diagnostics.AddError("Invalid import ID", "Expected `<workspace>.<layer>.<r|w>`, e.g. `demo.cities.r`.")
		return
	}
	access := "read"
	if parts[2] == "w" {
		access = "write"
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("layer"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("access"), access)...)
}

func splitRoles(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
