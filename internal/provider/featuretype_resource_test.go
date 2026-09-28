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
	"github.com/jackc/pgx/v5"

	"github.com/neocaos/geoform/internal/geoserver"
)

// testAccCreateTable creates a point table with two rows in the acceptance
// PostGIS database and drops it when the test ends. Terraform only publishes
// tables; it does not create them.
func testAccCreateTable(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGIS_DSN")
	if dsn == "" {
		port := os.Getenv("POSTGIS_PORT")
		if port == "" {
			port = "5432"
		}
		dsn = "postgres://geouser:geopassword@localhost:" + port + "/geoform_dev?sslmode=disable"
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to PostGIS (set TEST_POSTGIS_DSN to override): %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })

	table := "tfacc_" + acctest.RandString(8)
	_, err = conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %[1]s (id serial PRIMARY KEY, name text, geom geometry(Point, 4326));
		INSERT INTO %[1]s (name, geom) VALUES
			('a', ST_SetSRID(ST_MakePoint(-3.7, 40.4), 4326)),
			('b', ST_SetSRID(ST_MakePoint(2.17, 41.38), 4326));`, table))
	if err != nil {
		t.Fatalf("creating test table: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table); err != nil {
			t.Errorf("dropping test table %s: %v", table, err)
		}
	})
	return table
}

func TestAccFeatureType_basic(t *testing.T) {
	testAccPreCheck(t)
	ws := "tfacc_" + acctest.RandString(8)
	table := testAccCreateTable(t)
	const addr = "geoserver_featuretype.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckFeatureTypeDestroyed,
		Steps: []resource.TestStep{
			{
				// Only the table name: everything else comes from GeoServer.
				Config: testAccFeatureTypeConfig(ws, fmt.Sprintf(`native_name = %q`, table)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "id", ws+"/pg/cities"),
					resource.TestCheckResourceAttr(addr, "native_name", table),
					resource.TestCheckResourceAttr(addr, "title", table), // GeoServer titles it after the table
					resource.TestCheckResourceAttr(addr, "srs", "EPSG:4326"),
					resource.TestCheckResourceAttrSet(addr, "projection_policy"),
					resource.TestCheckResourceAttr(addr, "enabled", "true"),
					resource.TestCheckNoResourceAttr(addr, "abstract"),
					testAccCheckFeatureTypeBBox(ws, "cities", -3.7, 2.17),
				),
			},
			{
				Config: testAccFeatureTypeConfig(ws, fmt.Sprintf(`
  native_name       = %q
  title             = "Cities"
  abstract          = "Two cities"
  srs               = "EPSG:3857"
  projection_policy = "REPROJECT_TO_DECLARED"`, table)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "title", "Cities"),
					resource.TestCheckResourceAttr(addr, "abstract", "Two cities"),
					resource.TestCheckResourceAttr(addr, "srs", "EPSG:3857"),
					resource.TestCheckResourceAttr(addr, "projection_policy", "REPROJECT_TO_DECLARED"),
					// The lat/lon box is unchanged by the reprojection.
					testAccCheckFeatureTypeBBox(ws, "cities", -3.7, 2.17),
				),
			},
			{
				// Removing abstract clears it; disabling is an in-place update.
				Config: testAccFeatureTypeConfig(ws, fmt.Sprintf(`
  native_name       = %q
  title             = "Cities"
  srs               = "EPSG:3857"
  projection_policy = "REPROJECT_TO_DECLARED"
  enabled           = false`, table)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(addr, "abstract"),
					resource.TestCheckResourceAttr(addr, "enabled", "false"),
				),
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     ws + "/pg/cities",
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFeatureType_missingTable(t *testing.T) {
	testAccPreCheck(t)
	ws := "tfacc_" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccFeatureTypeConfig(ws, `native_name = "no_such_table"`),
				ExpectError: regexp.MustCompile(`Error creating feature type`),
			},
		},
	})
}

func testAccFeatureTypeConfig(ws, body string) string {
	return testAccPostGISDataStoreConfigNamed(ws, "pg") + fmt.Sprintf(`
resource "geoserver_featuretype" "test" {
  workspace = geoserver_workspace.test.name
  datastore = geoserver_postgis_datastore.test.name
  name      = "cities"
  %s
}
`, body)
}

// testAccCheckFeatureTypeBBox checks GeoServer computed a lat/lon bounding
// box spanning the expected longitudes.
func testAccCheckFeatureTypeBBox(ws, name string, minX, maxX float64) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ft, err := testAccClient().GetFeatureType(context.Background(), ws, "pg", name)
		if err != nil {
			return err
		}
		const eps = 1e-6
		b := ft.LatLonBBox
		if b == nil || b.MinX < minX-eps || b.MinX > minX+eps || b.MaxX < maxX-eps || b.MaxX > maxX+eps {
			return fmt.Errorf("lat/lon bounding box = %+v, want minx=%v maxx=%v", b, minX, maxX)
		}
		return nil
	}
}

func testAccCheckFeatureTypeDestroyed(s *terraform.State) error {
	client := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "geoserver_featuretype" {
			continue
		}
		a := rs.Primary.Attributes
		_, err := client.GetFeatureType(context.Background(), a["workspace"], a["datastore"], a["name"])
		if err == nil {
			return fmt.Errorf("feature type %q still exists", rs.Primary.ID)
		}
		if !geoserver.IsNotFound(err) {
			return err
		}
	}
	return testAccCheckDataStoreDestroyed(s)
}
