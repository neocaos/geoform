package provider

import (
	"context"
	"os"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories runs the provider in-process for acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"geoserver": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProvider_Schema(t *testing.T) {
	resp := &fwprovider.SchemaResponse{}
	New("test")().Schema(context.Background(), fwprovider.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("invalid schema: %v", diags)
	}
}

// testAccPreCheck skips nothing itself (the framework already requires
// TF_ACC), but fails fast when the target GeoServer is not configured.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, env := range []string{envURL, envUsername, envPassword} {
		if os.Getenv(env) == "" {
			t.Fatalf("%s must be set for acceptance tests", env)
		}
	}
}
