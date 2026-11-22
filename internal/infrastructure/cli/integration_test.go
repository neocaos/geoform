//go:build integration

package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/cli"
	"github.com/neocaos/geoform/internal/testenv"
)

// Requiere el stack de docker-compose.test.yml levantado (make test-integration).

const itConfig = `
workspaces:
  - name: geoform_cli_it
    datastores:
      - name: postgis
        host: db
        port: 5432
        database: geoform
        schema: geodata
        user: geouser
        password_env: IT_PG_PASSWORD
        layers:
          - name: cities
            title: %s
            srs: EPSG:4326
          - name: regions
            srs: EPSG:4326
`

func TestIntegration_CLILifecycle(t *testing.T) {
	ctx := context.Background()
	url := testenv.URL()
	user := testenv.User()
	pass := testenv.Pass()
	client := gs.NewClient(url, user, pass)

	const ws = "geoform_cli_it"
	cleanup := func() { _ = client.DeleteWorkspace(ctx, ws, true) }
	cleanup()
	t.Cleanup(cleanup)

	dir := t.TempDir()
	path := filepath.Join(dir, "geoform.yaml")
	writeCfg := func(title string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(strings.Replace(itConfig, "%s", title, 1)), 0o600); err != nil {
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
			Args:   full,
			Stdin:  strings.NewReader(""),
			Stdout: &out, Stderr: &errOut,
			Getenv: func(k string) string {
				if k == "IT_PG_PASSWORD" {
					return "geopassword"
				}
				return ""
			},
			NewReconciler: cli.GeoServerReconciler,
		})
		t.Logf("geoform %s -> %d\n%s%s", strings.Join(args, " "), code, out.String(), errOut.String())
		return code, out.String() + errOut.String()
	}

	writeCfg("Ciudades")

	// validate no necesita GeoServer.
	if code, _ := run("validate"); code != cli.ExitOK {
		t.Fatalf("validate: %d", code)
	}

	// plan: hay diff y no escribe nada.
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "Plan: 4 a crear") {
		t.Fatalf("plan inicial: %d", code)
	}
	if _, err := client.GetWorkspace(ctx, ws); !gs.IsNotFound(err) {
		t.Fatalf("plan no debe crear el workspace: %v", err)
	}

	// apply crea todo.
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}
	ft, err := client.GetFeatureType(ctx, ws, "postgis", "cities")
	if err != nil || ft.Title != "Ciudades" {
		t.Fatalf("capa cities: %+v %v", ft, err)
	}

	// re-apply idempotente: plan sin diff y apply sin cambios.
	if code, out := run("plan", "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras apply: %d", code)
	}
	if code, out := run("apply", "--auto-approve"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("re-apply: %d", code)
	}

	// update: cambia el título.
	writeCfg("Ciudades de España")
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "title:") {
		t.Fatalf("plan update: %d", code)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply update: %d", code)
	}
	if ft, _ := client.GetFeatureType(ctx, ws, "postgis", "cities"); ft == nil || ft.Title != "Ciudades de España" {
		t.Fatalf("título sin actualizar: %+v", ft)
	}

	// prune: una capa publicada fuera de la config solo se borra con --prune.
	if err := client.CreateFeatureType(ctx, ws, "postgis", gs.FeatureType{Name: "roads", NativeName: "roads", SRS: "EPSG:4326", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply sin prune: %d", code)
	}
	if _, err := client.GetFeatureType(ctx, ws, "postgis", "roads"); err != nil {
		t.Fatalf("sin --prune la capa extra debe sobrevivir: %v", err)
	}
	if code, out := run("plan", "--prune"); code != cli.ExitDiff || !strings.Contains(out, "- layer "+ws+"/postgis/roads") {
		t.Fatalf("plan --prune: %d", code)
	}
	if code, _ := run("apply", "--prune", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply --prune: %d", code)
	}
	if _, err := client.GetFeatureType(ctx, ws, "postgis", "roads"); !gs.IsNotFound(err) {
		t.Fatalf("roads debía borrarse: %v", err)
	}
	if _, err := client.GetFeatureType(ctx, ws, "postgis", "regions"); err != nil {
		t.Fatalf("regions no debía borrarse: %v", err)
	}
}
