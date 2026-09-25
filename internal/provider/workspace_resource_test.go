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

func TestAccWorkspace_basic(t *testing.T) {
	name := "tfacc_" + acctest.RandString(8)
	const addr = "geoserver_workspace.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWorkspaceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceConfig(name, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "id", name),
					resource.TestCheckResourceAttr(addr, "name", name),
					resource.TestCheckResourceAttr(addr, "isolated", "false"),
				),
			},
			{
				Config: testAccWorkspaceConfig(name, "isolated = true"),
				Check:  resource.TestCheckResourceAttr(addr, "isolated", "true"),
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
			{
				Config: testAccWorkspaceConfig(name, "isolated = false"),
				Check:  resource.TestCheckResourceAttr(addr, "isolated", "false"),
			},
		},
	})
}

func TestAccWorkspace_recreatedAfterExternalDelete(t *testing.T) {
	name := "tfacc_" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWorkspaceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceConfig(name, ""),
				Check: func(*terraform.State) error {
					return testAccClient().DeleteWorkspace(context.Background(), name, false)
				},
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccWorkspaceConfig(name, ""),
				Check:  resource.TestCheckResourceAttr("geoserver_workspace.test", "name", name),
			},
		},
	})
}

func TestAccWorkspace_invalidName(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccWorkspaceConfig("1bad name", ""),
				ExpectError: regexp.MustCompile(`must start with a letter or underscore`),
			},
		},
	})
}

func testAccWorkspaceConfig(name, extra string) string {
	return fmt.Sprintf(`
resource "geoserver_workspace" "test" {
  name = %q
  %s
}
`, name, extra)
}

func testAccClient() *geoserver.Client {
	return geoserver.NewClient(os.Getenv(envURL), os.Getenv(envUsername), os.Getenv(envPassword))
}

func testAccCheckWorkspaceDestroyed(s *terraform.State) error {
	client := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "geoserver_workspace" {
			continue
		}
		_, err := client.GetWorkspace(context.Background(), rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("workspace %q still exists", rs.Primary.ID)
		}
		if !geoserver.IsNotFound(err) {
			return err
		}
	}
	return nil
}
