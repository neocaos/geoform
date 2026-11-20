// Package provider implements the GeoServer Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/neocaos/geoform/internal/geoserver"
)

// Environment variables used when the matching provider attribute is unset.
const (
	envURL      = "GEOSERVER_URL"
	envUsername = "GEOSERVER_USERNAME"
	envPassword = "GEOSERVER_PASSWORD"
)

var _ provider.Provider = (*geoserverProvider)(nil)

type geoserverProvider struct {
	version string
}

type providerModel struct {
	URL      types.String `tfsdk:"url"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}

// New returns a constructor for the provider at the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &geoserverProvider{version: version}
	}
}

func (p *geoserverProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "geoserver"
	resp.Version = p.version
}

func (p *geoserverProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages GeoServer configuration through its REST API.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Description: "Base URL of the GeoServer instance, e.g. `http://localhost:8080/geoserver`. " +
					"May also be set with the `" + envURL + "` environment variable.",
				Optional: true,
			},
			"username": schema.StringAttribute{
				Description: "Username for the REST API. May also be set with the `" + envUsername + "` environment variable.",
				Optional:    true,
			},
			"password": schema.StringAttribute{
				Description: "Password for the REST API. May also be set with the `" + envPassword + "` environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *geoserverProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := resolve(cfg.URL, envURL, "url", resp)
	username := resolve(cfg.Username, envUsername, "username", resp)
	password := resolve(cfg.Password, envPassword, "password", resp)
	if resp.Diagnostics.HasError() {
		return
	}

	client := geoserver.NewClient(url, username, password,
		geoserver.WithUserAgent("terraform-provider-geoserver/"+p.version))
	resp.ResourceData = client
	resp.DataSourceData = client
}

// resolve returns the configured value of attr, falling back to the envVar
// environment variable. It adds an error diagnostic if the value is unknown
// at plan time or missing from both sources.
func resolve(v types.String, envVar, attr string, resp *provider.ConfigureResponse) string {
	if v.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root(attr), "Unknown GeoServer "+attr,
			"The provider cannot be configured with a value that is only known after apply. "+
				"Set it statically or use the "+envVar+" environment variable.")
		return ""
	}
	if !v.IsNull() && v.ValueString() != "" {
		return v.ValueString()
	}
	if env := os.Getenv(envVar); env != "" {
		return env
	}
	resp.Diagnostics.AddAttributeError(path.Root(attr), "Missing GeoServer "+attr,
		"Set the \""+attr+"\" provider attribute or the "+envVar+" environment variable.")
	return ""
}

func (p *geoserverProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewWorkspaceResource,
		NewPostGISDataStoreResource,
		NewFeatureTypeResource,
	}
}

func (p *geoserverProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
