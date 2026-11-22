package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/neocaos/geoform/internal/geoserver"
)

// Fixtures de test/testdata/stores, montados en /data/stores del contenedor
// de GeoServer por docker-compose.yml y docker-compose.test.yml.
const (
	fixtureGPKG = "file:/data/stores/places.gpkg"
	fixtureSHP  = "file:/data/stores/places.shp"
	fixtureTIF  = "file:/data/stores/dem.tif"
)

func testAccWorkspaceBlock(ws string) string {
	return fmt.Sprintf("resource \"geoserver_workspace\" \"test\" {\n  name = %q\n}\n", ws)
}

func TestAccGeoPackageDataStore_basic(t *testing.T) {
	ws := "tfacc_" + acctest.RandString(8)
	const addr = "geoserver_geopackage_datastore.test"
	cfg := func(extra string) string {
		return testAccWorkspaceBlock(ws) + fmt.Sprintf(`
resource "geoserver_geopackage_datastore" "test" {
  workspace = geoserver_workspace.test.name
  name      = "places"
  file      = %q
  %s
}

resource "geoserver_featuretype" "places" {
  workspace = geoserver_workspace.test.name
  datastore = geoserver_geopackage_datastore.test.name
  name      = "gpkg_places"
  native_name = "places"
  srs       = "EPSG:4326"
}
`, fixtureGPKG, extra)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStoreDestroyed("geoserver_geopackage_datastore"),
		Steps: []resource.TestStep{
			{
				Config: cfg(`description = "gpkg"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "id", ws+"/places"),
					resource.TestCheckResourceAttr(addr, "file", fixtureGPKG),
					resource.TestCheckResourceAttr(addr, "enabled", "true"),
					resource.TestCheckResourceAttrSet("geoserver_featuretype.places", "id"),
				),
			},
			{
				PreConfig: func() { testAccSetDataStoreParam(t, ws, "places", "fetch size", "500") },
				Config:    cfg("enabled = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "enabled", "false"),
					resource.TestCheckNoResourceAttr(addr, "description"),
					// Los parámetros no gestionados sobreviven al PUT.
					testAccCheckDataStoreParam(ws, "places", "fetch size", "500"),
				),
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     ws + "/places",
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccShapefileDataStore_basic(t *testing.T) {
	ws := "tfacc_" + acctest.RandString(8)
	const addr = "geoserver_shapefile_datastore.test"
	cfg := testAccWorkspaceBlock(ws) + fmt.Sprintf(`
resource "geoserver_shapefile_datastore" "test" {
  workspace = geoserver_workspace.test.name
  name      = "shp"
  file      = %q
}

resource "geoserver_featuretype" "places" {
  workspace = geoserver_workspace.test.name
  datastore = geoserver_shapefile_datastore.test.name
  name      = "places"
  srs       = "EPSG:4326"
}
`, fixtureSHP)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStoreDestroyed("geoserver_shapefile_datastore"),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "id", ws+"/shp"),
					resource.TestCheckResourceAttr(addr, "file", fixtureSHP),
				),
			},
			{ResourceName: addr, ImportState: true, ImportStateId: ws + "/shp", ImportStateVerify: true},
			{
				ResourceName:  addr,
				ImportState:   true,
				ImportStateId: "no-slash",
				ExpectError:   regexp.MustCompile(`Expected "<workspace>/<name>"`),
			},
		},
	})
}

func TestAccGeoTIFFCoverage_basic(t *testing.T) {
	ws := "tfacc_" + acctest.RandString(8)
	const storeAddr, covAddr = "geoserver_geotiff_coveragestore.test", "geoserver_coverage.test"
	cfg := func(title string) string {
		return testAccWorkspaceBlock(ws) + fmt.Sprintf(`
resource "geoserver_geotiff_coveragestore" "test" {
  workspace = geoserver_workspace.test.name
  name      = "dem"
  url       = %q
}

resource "geoserver_coverage" "test" {
  workspace     = geoserver_workspace.test.name
  coveragestore = geoserver_geotiff_coveragestore.test.name
  name          = "relieve"
  native_name   = "dem"
  title         = %q
  srs           = "EPSG:4326"
}
`, fixtureTIF, title)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStoreDestroyed("geoserver_geotiff_coveragestore"),
		Steps: []resource.TestStep{
			{
				Config: cfg("Relieve"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(storeAddr, "id", ws+"/dem"),
					resource.TestCheckResourceAttr(storeAddr, "url", fixtureTIF),
					resource.TestCheckResourceAttr(covAddr, "id", ws+"/dem/relieve"),
					resource.TestCheckResourceAttr(covAddr, "native_name", "dem"),
					resource.TestCheckResourceAttr(covAddr, "title", "Relieve"),
					testAccCheckLayerPublished(ws, "relieve"),
				),
			},
			{
				Config: cfg("Modelo digital"),
				Check:  resource.TestCheckResourceAttr(covAddr, "title", "Modelo digital"),
			},
			{ResourceName: storeAddr, ImportState: true, ImportStateId: ws + "/dem", ImportStateVerify: true},
			{ResourceName: covAddr, ImportState: true, ImportStateId: ws + "/dem/relieve", ImportStateVerify: true},
			{
				ResourceName:  covAddr,
				ImportState:   true,
				ImportStateId: ws + "/dem",
				ExpectError:   regexp.MustCompile(`Expected "<workspace>/<coveragestore>/<name>"`),
			},
		},
	})
}

func testAccCheckLayerPublished(ws, name string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ok, err := testAccClient().LayerExists(context.Background(), ws, name)
		if err != nil || !ok {
			return fmt.Errorf("layer %s:%s not published (err=%v)", ws, name, err)
		}
		return nil
	}
}

// testAccCheckStoreDestroyed comprueba que los stores de tipo resType ya no
// existen y que el workspace se eliminó.
func testAccCheckStoreDestroyed(resType string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccClient()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != resType {
				continue
			}
			ws, name := rs.Primary.Attributes["workspace"], rs.Primary.Attributes["name"]
			var err error
			if resType == "geoserver_geotiff_coveragestore" {
				_, err = client.GetCoverageStore(context.Background(), ws, name)
			} else {
				_, err = client.GetDataStore(context.Background(), ws, name)
			}
			if err == nil {
				return fmt.Errorf("%s %q still exists", resType, rs.Primary.ID)
			}
			if !geoserver.IsNotFound(err) {
				return err
			}
		}
		return testAccCheckWorkspaceDestroyed(s)
	}
}
