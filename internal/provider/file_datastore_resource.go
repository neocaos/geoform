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

// fileDataStoreKind describe un tipo de data store vectorial definido por un
// único fichero. Los recursos geopackage y shapefile comparten implementación.
type fileDataStoreKind struct {
	suffix      string // sufijo del recurso: geoserver_<suffix>
	label       string // nombre legible en mensajes
	description string
	fileDesc    string
	// param es el parámetro de conexión que guarda el fichero.
	param string
	// build crea el data store de GeoServer.
	build func(name, file string) geoserver.DataStore
	// matches indica si el data store de GeoServer es de este tipo.
	matches func(ds *geoserver.DataStore) bool
}

var (
	geoPackageKind = fileDataStoreKind{
		suffix:      "geopackage_datastore",
		label:       "GeoPackage data store",
		description: "A GeoServer data store backed by a GeoPackage (.gpkg) file with vector tables.",
		fileDesc: "GeoServer URL of the `.gpkg` file, as seen by the GeoServer server (not by Terraform), " +
			"e.g. `file:/data/places.gpkg` or `file:data/places.gpkg` (relative to the data directory).",
		param: "database",
		build: geoserver.GeoPackageDataStore,
		matches: func(ds *geoserver.DataStore) bool {
			return ds.ConnectionParameters["dbtype"] == geoserver.DBTypeGeoPackage
		},
	}
	shapefileKind = fileDataStoreKind{
		suffix:      "shapefile_datastore",
		label:       "shapefile data store",
		description: "A GeoServer data store backed by a shapefile (.shp).",
		fileDesc: "GeoServer URL of the `.shp` file, as seen by the GeoServer server (not by Terraform), " +
			"e.g. `file:/data/places.shp`. The published table is named after the file.",
		param: "url",
		build: geoserver.ShapefileDataStore,
		matches: func(ds *geoserver.DataStore) bool {
			p := ds.ConnectionParameters
			return p["dbtype"] == "" && p["url"] != ""
		},
	}
)

var (
	_ resource.Resource                = (*fileDataStoreResource)(nil)
	_ resource.ResourceWithConfigure   = (*fileDataStoreResource)(nil)
	_ resource.ResourceWithImportState = (*fileDataStoreResource)(nil)
)

type fileDataStoreResource struct {
	kind   fileDataStoreKind
	client *geoserver.Client
}

type fileDataStoreModel struct {
	ID          types.String `tfsdk:"id"`
	Workspace   types.String `tfsdk:"workspace"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	File        types.String `tfsdk:"file"`
}

// NewGeoPackageDataStoreResource returns the geoserver_geopackage_datastore resource.
func NewGeoPackageDataStoreResource() resource.Resource {
	return &fileDataStoreResource{kind: geoPackageKind}
}

// NewShapefileDataStoreResource returns the geoserver_shapefile_datastore resource.
func NewShapefileDataStoreResource() resource.Resource {
	return &fileDataStoreResource{kind: shapefileKind}
}

func (r *fileDataStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.kind.suffix
}

func (r *fileDataStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: r.kind.description,
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
			"file": schema.StringAttribute{
				Description: r.kind.fileDesc,
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *fileDataStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *fileDataStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan fileDataStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateDataStore(ctx, plan.Workspace.ValueString(), r.toAPI(&plan, nil)); err != nil {
		resp.Diagnostics.AddError("Error creating "+r.kind.label, err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fileDataStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state fileDataStoreModel
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
		resp.Diagnostics.AddError("Error reading "+r.kind.label, err.Error())
		return
	}
	resp.Diagnostics.Append(r.fromAPI(&state, ds)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *fileDataStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan fileDataStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	workspace, name := plan.Workspace.ValueString(), plan.Name.ValueString()

	// Conserva los parámetros que este recurso no gestiona (el PUT reemplaza todo).
	current, err := r.client.GetDataStore(ctx, workspace, name)
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.kind.label+" before update", err.Error())
		return
	}
	if err := r.client.UpdateDataStore(ctx, workspace, name, r.toAPI(&plan, current.ConnectionParameters)); err != nil {
		resp.Diagnostics.AddError("Error updating "+r.kind.label, err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fileDataStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state fileDataStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Sin recurse: las capas publicadas desde el store deben borrarse antes.
	err := r.client.DeleteDataStore(ctx, state.Workspace.ValueString(), state.Name.ValueString(), false)
	if err != nil && !geoserver.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting "+r.kind.label, err.Error())
	}
}

func (r *fileDataStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, name, ok := strings.Cut(req.ID, "/")
	if !ok || workspace == "" || name == "" || strings.Contains(name, "/") {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected \"<workspace>/<name>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

func (r *fileDataStoreResource) refresh(ctx context.Context, m *fileDataStoreModel, diags *diag.Diagnostics) {
	ds, err := r.client.GetDataStore(ctx, m.Workspace.ValueString(), m.Name.ValueString())
	if err != nil {
		diags.AddError("Error reading "+r.kind.label+" after write", err.Error())
		return
	}
	diags.Append(r.fromAPI(m, ds)...)
}

// toAPI construye el cuerpo de la petición sobreponiendo los parámetros
// gestionados a base.
func (r *fileDataStoreResource) toAPI(m *fileDataStoreModel, base geoserver.ConnectionParameters) geoserver.DataStore {
	ds := r.kind.build(m.Name.ValueString(), m.File.ValueString())
	params := make(geoserver.ConnectionParameters, len(base)+len(ds.ConnectionParameters))
	for k, v := range base {
		params[k] = v
	}
	for k, v := range ds.ConnectionParameters {
		params[k] = v
	}
	ds.ConnectionParameters = params
	ds.Description = m.Description.ValueString()
	ds.Enabled = m.Enabled.ValueBool()
	return ds
}

func (r *fileDataStoreResource) fromAPI(m *fileDataStoreModel, ds *geoserver.DataStore) diag.Diagnostics {
	var diags diag.Diagnostics
	if !r.kind.matches(ds) {
		diags.AddError("Not a "+r.kind.label,
			fmt.Sprintf("Data store %q has type %q; manage it with a matching resource.", ds.Name, ds.Type))
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
	m.File = types.StringValue(ds.ConnectionParameters[r.kind.param])
	return diags
}
