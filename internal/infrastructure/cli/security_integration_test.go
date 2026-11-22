//go:build integration

package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/cli"
	"github.com/neocaos/geoform/internal/testenv"
)

const itSecConfig = `
workspaces:
  - name: geoform_sec_it
security:
  roles: [ROLE_SEC_IT_EDITOR, ROLE_SEC_IT_VIEWER]
  users:
    - name: sec_it_alice
      password_env: IT_ALICE_PASSWORD
      enabled: %s
      roles: [%s]
  rules:
    - workspace: geoform_sec_it
      access: read
      roles: [ROLE_SEC_IT_VIEWER, ROLE_SEC_IT_EDITOR]
    - workspace: geoform_sec_it
      layer: cities
      access: write
      roles: [ROLE_SEC_IT_EDITOR]
`

func TestIntegration_SecurityLifecycle(t *testing.T) {
	ctx := context.Background()
	url := testenv.URL()
	user := testenv.User()
	pass := testenv.Pass()
	client := gs.NewClient(url, user, pass)

	cleanup := func() {
		_ = client.DeleteLayerRule(ctx, "geoform_sec_it.*.r")
		_ = client.DeleteLayerRule(ctx, "geoform_sec_it.cities.w")
		_ = client.DeleteUser(ctx, "sec_it_alice")
		_ = client.DeleteRole(ctx, "ROLE_SEC_IT_EDITOR")
		_ = client.DeleteRole(ctx, "ROLE_SEC_IT_VIEWER")
		_ = client.DeleteWorkspace(ctx, "geoform_sec_it", true)
	}
	cleanup()
	t.Cleanup(cleanup)

	path := filepath.Join(t.TempDir(), "geoform.yaml")
	writeCfg := func(enabled, roles string) {
		t.Helper()
		cfg := strings.Replace(itSecConfig, "%s", enabled, 1)
		cfg = strings.Replace(cfg, "%s", roles, 1)
		if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		full := append(args, "--file", path)
		if args[0] != "validate" {
			full = append(full, "--server", url, "--user", user, "--password", pass)
		}
		code := cli.Run(cli.Env{
			Stdin: strings.NewReader(""), Args: full, Stdout: &out, Stderr: &errOut,
			Getenv: func(k string) string {
				if k == "IT_ALICE_PASSWORD" {
					return "it-Alice-pw-1"
				}
				return ""
			},
			NewReconciler: cli.GeoServerReconciler,
		})
		combined := out.String() + errOut.String()
		if strings.Contains(combined, "it-Alice-pw-1") {
			t.Fatal("la salida no debe contener contraseñas")
		}
		t.Logf("geoform %s -> %d\n%s", strings.Join(args, " "), code, combined)
		return code, combined
	}

	writeCfg("true", "ROLE_SEC_IT_EDITOR")
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "+ user sec_it_alice") {
		t.Fatalf("plan inicial: %d", code)
	}
	if _, err := client.GetUser(ctx, "sec_it_alice"); !gs.IsNotFound(err) {
		t.Fatalf("plan no debe crear usuarios: %v", err)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}
	roles, err := client.UserRoles(ctx, "sec_it_alice")
	if err != nil || !reflect.DeepEqual(roles, []string{"ROLE_SEC_IT_EDITOR"}) {
		t.Fatalf("roles de alice: %v %v", roles, err)
	}
	rules, _ := client.LayerRules(ctx)
	if rules["geoform_sec_it.cities.w"] != "ROLE_SEC_IT_EDITOR" || rules["geoform_sec_it.*.r"] == "" {
		t.Fatalf("reglas: %v", rules)
	}

	// Idempotente (con --prune: no debe tocar admin, roles de sistema ni reglas globales).
	if code, out := run("plan", "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras apply: %d", code)
	}

	// Cambio de roles y de enabled.
	writeCfg("false", "ROLE_SEC_IT_VIEWER")
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "enabled: true -> false") {
		t.Fatalf("plan update: %d", code)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply update: %d", code)
	}
	if roles, _ := client.UserRoles(ctx, "sec_it_alice"); !reflect.DeepEqual(roles, []string{"ROLE_SEC_IT_VIEWER"}) {
		t.Fatalf("roles tras update: %v", roles)
	}
	if u, _ := client.GetUser(ctx, "sec_it_alice"); u == nil || u.Enabled {
		t.Fatalf("alice debía quedar deshabilitada: %+v", u)
	}

	// prune: una regla fuera de la config en un workspace declarado se borra.
	if err := client.CreateLayerRule(ctx, "geoform_sec_it.old.r", []string{"ROLE_SEC_IT_VIEWER"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatal("apply sin prune")
	}
	if rules, _ := client.LayerRules(ctx); rules["geoform_sec_it.old.r"] == "" {
		t.Fatal("sin --prune la regla extra debe sobrevivir")
	}
	if code, _ := run("apply", "--prune", "--auto-approve"); code != cli.ExitOK {
		t.Fatal("apply --prune")
	}
	rules, _ = client.LayerRules(ctx)
	if rules["geoform_sec_it.old.r"] != "" || rules["*.*.w"] == "" {
		t.Fatalf("prune: %v", rules)
	}
}
