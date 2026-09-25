package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/neocaos/geoform/internal/geoserver"
)

// testAccPostGIS returns the database GeoServer should connect to in
// acceptance tests. Defaults match docker-compose.yml.
func testAccPostGIS() (host, database, user, password string) {
	get := func(env, def string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return def
	}
	return get("TEST_POSTGIS_HOST", "db"), get("TEST_POSTGIS_DATABASE", "geoform_dev"),
		get("TEST_POSTGIS_USER", "geouser"), get("TEST_POSTGIS_PASSWORD", "geopassword")
}

func TestAccPostGISDataStore_basic(t *testing.T) {
	ws := "tfacc_" + acctest.RandString(8)
	const addr = "geoserver_postgis_datastore.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDataStoreDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccPostGISDataStoreConfig(ws, `description = "roads of acme"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "id", ws+"/roads"),
					resource.TestCheckResourceAttr(addr, "description", "roads of acme"),
					resource.TestCheckResourceAttr(addr, "enabled", "true"),
					resource.TestCheckResourceAttr(addr, "port", "5432"),
					resource.TestCheckResourceAttr(addr, "schema", "public"),
					testAccCheckDataStoreConnects(ws, "roads"),
				),
			},
			{
				// Mark a parameter this resource does not manage, then update:
				// it must survive the PUT.
				PreConfig: func() { testAccSetDataStoreParam(t, ws, "roads", "Expose primary keys", "true") },
				Config:    testAccPostGISDataStoreConfig(ws, "enabled = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "enabled", "false"),
					resource.TestCheckNoResourceAttr(addr, "description"),
					testAccCheckDataStoreParam(ws, "roads", "Expose primary keys", "true"),
				),
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           ws + "/roads",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
		},
	})
}

func TestAccPostGISDataStore_invalidImportID(t *testing.T) {
	ws := "tfacc_" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccPostGISDataStoreConfig(ws, "")},
			{
				ResourceName:  "geoserver_postgis_datastore.test",
				ImportState:   true,
				ImportStateId: "no-slash",
				ExpectError:   regexp.MustCompile(`Expected "<workspace>/<name>"`),
			},
		},
	})
}

func testAccPostGISDataStoreConfig(ws, extra string) string {
	host, database, user, password := testAccPostGIS()
	return fmt.Sprintf(`
resource "geoserver_workspace" "test" {
  name = %q
}

resource "geoserver_postgis_datastore" "test" {
  workspace = geoserver_workspace.test.name
  name      = "roads"
  host      = %q
  database  = %q
  user      = %q
  password  = %q
  %s
}
`, ws, host, database, user, password, extra)
}

func testAccSetDataStoreParam(t *testing.T, ws, name, key, value string) {
	t.Helper()
	client := testAccClient()
	ds, err := client.GetDataStore(context.Background(), ws, name)
	if err != nil {
		t.Fatal(err)
	}
	ds.ConnectionParameters[key] = value
	// Resend the real password: the stored one comes back encrypted.
	_, _, _, password := testAccPostGIS()
	ds.ConnectionParameters[paramPassword] = password
	if err := client.UpdateDataStore(context.Background(), ws, name, *ds); err != nil {
		t.Fatal(err)
	}
}

func testAccCheckDataStoreParam(ws, name, key, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ds, err := testAccClient().GetDataStore(context.Background(), ws, name)
		if err != nil {
			return err
		}
		if got := ds.ConnectionParameters[key]; got != want {
			return fmt.Errorf("connection parameter %q = %q, want %q", key, got, want)
		}
		return nil
	}
}

// testAccCheckDataStoreConnects asks GeoServer to list the store's tables,
// which fails unless it can log in to PostGIS with the configured password.
func testAccCheckDataStoreConnects(ws, name string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if _, err := testAccClient().ListAvailableFeatureTypes(context.Background(), ws, name); err != nil {
			return fmt.Errorf("GeoServer cannot connect through data store %s/%s: %w", ws, name, err)
		}
		return nil
	}
}

func testAccCheckDataStoreDestroyed(s *terraform.State) error {
	client := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "geoserver_postgis_datastore" {
			continue
		}
		_, err := client.GetDataStore(context.Background(), rs.Primary.Attributes["workspace"], rs.Primary.Attributes["name"])
		if err == nil {
			return fmt.Errorf("data store %q still exists", rs.Primary.ID)
		}
		if !geoserver.IsNotFound(err) {
			return err
		}
	}
	return testAccCheckWorkspaceDestroyed(s)
}
