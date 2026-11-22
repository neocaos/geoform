package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/geoserver"
)

func testAccSLD(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../test/testdata/sld/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAccStyle_basic(t *testing.T) {
	testAccPreCheck(t)
	ws := "tfacc_" + acctest.RandString(8)
	red, blue := testAccSLD(t, "point_red.sld"), testAccSLD(t, "point_blue.sld")
	const addr = "geoserver_style.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStyleDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccStyleConfig(ws, red),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "id", ws+"/mine"),
					testAccCheckStyleSLD(ws, "mine", red),
				),
			},
			{
				// El SLD se actualiza en sitio.
				Config: testAccStyleConfig(ws, blue),
				Check:  testAccCheckStyleSLD(ws, "mine", blue),
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           ws + "/mine",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"sld"}, // GeoServer reserializa el XML
			},
		},
	})
}

func testAccStyleConfig(ws, sld string) string {
	return testAccWorkspaceConfig(ws, "") + fmt.Sprintf(`
resource "geoserver_style" "test" {
  workspace = geoserver_workspace.test.name
  name      = "mine"
  sld       = %q
}
`, sld)
}

func testAccCheckStyleSLD(ws, name, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, err := testAccClient().GetStyleSLD(context.Background(), ws, name)
		if err != nil {
			return err
		}
		if !style.SameSLD(got, want) {
			return fmt.Errorf("SLD de %s/%s distinto del esperado:\n%s", ws, name, got)
		}
		return nil
	}
}

func testAccCheckStyleDestroyed(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "geoserver_style" {
			continue
		}
		a := rs.Primary.Attributes
		_, err := testAccClient().GetStyleSLD(context.Background(), a["workspace"], a["name"])
		if err == nil {
			return fmt.Errorf("style %q still exists", rs.Primary.ID)
		}
		if !geoserver.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// Estilo asignado a una capa y grupo de capas que la usa, de extremo a extremo.
func TestAccStyleAndLayerGroup_withFeatureType(t *testing.T) {
	testAccPreCheck(t)
	ws := "tfacc_" + acctest.RandString(8)
	table := testAccCreateTable(t)
	red, blue := testAccSLD(t, "point_red.sld"), testAccSLD(t, "point_blue.sld")
	const group = "geoserver_layer_group.test"

	cfg := func(styleRef, extraLayers, mode, title string) string {
		return testAccPostGISDataStoreConfigNamed(ws, "pg") + fmt.Sprintf(`
resource "geoserver_style" "red" {
  workspace = geoserver_workspace.test.name
  name      = "red"
  sld       = %q
}

resource "geoserver_style" "blue" {
  workspace = geoserver_workspace.test.name
  name      = "blue"
  sld       = %q
}

resource "geoserver_featuretype" "cities" {
  workspace     = geoserver_workspace.test.name
  datastore     = geoserver_postgis_datastore.test.name
  name          = "cities"
  native_name   = %q
  default_style = %s
}

resource "geoserver_featuretype" "towns" {
  workspace   = geoserver_workspace.test.name
  datastore   = geoserver_postgis_datastore.test.name
  name        = "towns"
  native_name = %q
}

resource "geoserver_layer_group" "test" {
  workspace = geoserver_workspace.test.name
  name      = "base"
  mode      = %q
  title     = %q
  layers    = [geoserver_featuretype.cities.name%s]
}
`, red, blue, table, styleRef, table, mode, title, extraLayers)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckStyleDestroyed, testAccCheckLayerGroupDestroyed, testAccCheckFeatureTypeDestroyed),
		Steps: []resource.TestStep{
			{
				Config: cfg(`"${geoserver_workspace.test.name}:${geoserver_style.red.name}"`, "", "SINGLE", "Base"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("geoserver_featuretype.cities", "default_style", ws+":red"),
					resource.TestCheckResourceAttr(group, "id", ws+"/base"),
					resource.TestCheckResourceAttr(group, "mode", "SINGLE"),
					resource.TestCheckResourceAttr(group, "layers.#", "1"),
				),
			},
			{
				// Cambia el estilo, añade una capa y cambia el modo, todo en sitio.
				Config: cfg(`"${geoserver_workspace.test.name}:${geoserver_style.blue.name}"`,
					", geoserver_featuretype.towns.name", "NAMED", "Base 2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("geoserver_featuretype.cities", "default_style", ws+":blue"),
					resource.TestCheckResourceAttr(group, "layers.#", "2"),
					resource.TestCheckResourceAttr(group, "layers.1", "towns"),
					resource.TestCheckResourceAttr(group, "mode", "NAMED"),
					resource.TestCheckResourceAttr(group, "title", "Base 2"),
				),
			},
			{
				ResourceName:      group,
				ImportState:       true,
				ImportStateId:     ws + "/base",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccCheckLayerGroupDestroyed(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "geoserver_layer_group" {
			continue
		}
		a := rs.Primary.Attributes
		_, err := testAccClient().GetLayerGroup(context.Background(), a["workspace"], a["name"])
		if err == nil {
			return fmt.Errorf("layer group %q still exists", rs.Primary.ID)
		}
		if !geoserver.IsNotFound(err) {
			return err
		}
	}
	return nil
}
