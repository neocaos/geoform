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

func testAccSecurityConfig(suffix, enabled, userRoles, ruleRoles string) string {
	return fmt.Sprintf(`
resource "geoserver_workspace" "test" {
  name = "tfsec_%[1]s"
}

resource "geoserver_security_role" "editor" {
  name = "ROLE_TFSEC_EDITOR_%[1]s"
}

resource "geoserver_security_role" "viewer" {
  name = "ROLE_TFSEC_VIEWER_%[1]s"
}

resource "geoserver_security_user" "test" {
  name     = "tfsec_%[1]s"
  password = "Tf-acc-password-1"
  enabled  = %[2]s
  roles    = [%[3]s]
}

resource "geoserver_security_layer_rule" "ws_read" {
  workspace = geoserver_workspace.test.name
  layer     = "*"
  access    = "read"
  roles     = [%[4]s]
}

resource "geoserver_security_layer_rule" "layer_write" {
  workspace = geoserver_workspace.test.name
  layer     = "cities"
  access    = "write"
  roles     = [geoserver_security_role.editor.name]
}
`, suffix, enabled, userRoles, ruleRoles)
}

func TestAccSecurity_lifecycle(t *testing.T) {
	sfx := acctest.RandString(6)
	ed := "geoserver_security_role.editor.name"
	vw := "geoserver_security_role.viewer.name"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSecurityDestroyed(sfx),
		Steps: []resource.TestStep{
			{
				Config: testAccSecurityConfig(sfx, "true", ed, ed),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("geoserver_security_user.test", "enabled", "true"),
					resource.TestCheckResourceAttr("geoserver_security_user.test", "roles.#", "1"),
					resource.TestCheckResourceAttr("geoserver_security_layer_rule.ws_read", "id", "tfsec_"+sfx+".*.r"),
					resource.TestCheckResourceAttr("geoserver_security_layer_rule.layer_write", "roles.#", "1"),
				),
			},
			{
				Config: testAccSecurityConfig(sfx, "false", vw+", "+ed, vw+", "+ed),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("geoserver_security_user.test", "enabled", "false"),
					resource.TestCheckResourceAttr("geoserver_security_user.test", "roles.#", "2"),
					resource.TestCheckResourceAttr("geoserver_security_layer_rule.ws_read", "roles.#", "2"),
				),
			},
			{
				ResourceName:      "geoserver_security_role.editor",
				ImportState:       true,
				ImportStateId:     "ROLE_TFSEC_EDITOR_" + sfx,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "geoserver_security_layer_rule.layer_write",
				ImportState:       true,
				ImportStateId:     "tfsec_" + sfx + ".cities.w",
				ImportStateVerify: true,
			},
			{
				// La contraseña no se puede importar: el resto del estado sí.
				ResourceName:            "geoserver_security_user.test",
				ImportState:             true,
				ImportStateId:           "tfsec_" + sfx,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
		},
	})
}

func TestAccSecurity_recreatedAfterExternalDelete(t *testing.T) {
	sfx := acctest.RandString(6)
	ed := "geoserver_security_role.editor.name"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSecurityDestroyed(sfx),
		Steps: []resource.TestStep{
			{Config: testAccSecurityConfig(sfx, "true", ed, ed)},
			{
				Config: testAccSecurityConfig(sfx, "true", ed, ed),
				Check: func(*terraform.State) error {
					return testAccClient().DeleteLayerRule(context.Background(), "tfsec_"+sfx+".cities.w")
				},
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccSecurityConfig(sfx, "true", ed, ed),
				Check:  resource.TestCheckResourceAttr("geoserver_security_layer_rule.layer_write", "access", "write"),
			},
		},
	})
}

func TestAccSecurity_invalidInput(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "geoserver_security_layer_rule" "bad" {
  workspace = "a.b"
  layer     = "*"
  access    = "read"
  roles     = ["ROLE_X"]
}`,
				ExpectError: regexp.MustCompile(`without '\.'`),
			},
			{
				Config: `
resource "geoserver_security_layer_rule" "bad" {
  workspace = "demo"
  layer     = "*"
  access    = "admin"
  roles     = ["ROLE_X"]
}`,
				ExpectError: regexp.MustCompile(`must be one of`),
			},
		},
	})
}

func testAccCheckSecurityDestroyed(sfx string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ctx, c := context.Background(), testAccClient()
		if _, err := c.GetUser(ctx, "tfsec_"+sfx); err == nil {
			return fmt.Errorf("user tfsec_%s still exists", sfx)
		} else if !geoserver.IsNotFound(err) {
			return err
		}
		roles, err := c.ListRoles(ctx)
		if err != nil {
			return err
		}
		for _, r := range roles {
			if r == "ROLE_TFSEC_EDITOR_"+sfx || r == "ROLE_TFSEC_VIEWER_"+sfx {
				return fmt.Errorf("role %s still exists", r)
			}
		}
		rules, err := c.LayerRules(ctx)
		if err != nil {
			return err
		}
		for _, k := range []string{"tfsec_" + sfx + ".*.r", "tfsec_" + sfx + ".cities.w"} {
			if _, ok := rules[k]; ok {
				return fmt.Errorf("rule %s still exists", k)
			}
		}
		return nil
	}
}
