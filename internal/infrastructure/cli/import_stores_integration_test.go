//go:build integration

package cli_test

import (
	"bytes"
	"context"
	"github.com/neocaos/geoform/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/cli"
)

const importStoresConfig = `
workspaces:
  - name: geoform_import_stores_it
    datastores:
      - name: gpkg
        type: geopackage
        file: file:/data/stores/places.gpkg
        layers:
          - {name: gpkg_places, table: places, title: Lugares, srs: "EPSG:4326"}
      - name: shp
        type: shapefile
        file: file:/data/stores/places.shp
        layers:
          - {name: places, srs: "EPSG:4326"}
    coveragestores:
      - name: dem
        url: file:/data/stores/dem.tif
        coverages:
          - {name: relieve, native_name: dem, title: Relieve, srs: "EPSG:4326"}
    layergroups:
      - name: base
        layers: [relieve, gpkg_places]
`

// apply de datastores de fichero y raster, import y plan --prune: sin diff y
// sin propuestas de borrado. Luego el YAML importado reproduce la configuración
// en un workspace vacío.
func TestIntegration_ImportRoundTripStores(t *testing.T) {
	ctx := context.Background()
	url := testenv.URL()
	user := testenv.User()
	pass := testenv.Pass()
	client := gs.NewClient(url, user, pass)

	const ws = "geoform_import_stores_it"
	cleanup := func() { _ = client.DeleteWorkspace(ctx, ws, true) }
	cleanup()
	t.Cleanup(cleanup)

	dir := t.TempDir()
	srcCfg := filepath.Join(dir, "src.yaml")
	imported := filepath.Join(dir, "imported.yaml")
	if err := os.WriteFile(srcCfg, []byte(importStoresConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		code := cli.Run(cli.Env{
			Args: append(args, "--server", url, "--user", user, "--password", pass), Stdin: strings.NewReader(""),
			Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }, NewReconciler: cli.GeoServerReconciler,
		})
		t.Logf("geoform %s -> %d\n%s%s", args[0], code, out.String(), errOut.String())
		return code, out.String() + errOut.String()
	}

	if code, _ := run("apply", "--file", srcCfg, "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}
	if code, out := run("import", "--workspace", ws, "--output", imported); code != cli.ExitOK || strings.Contains(out, "aviso") {
		t.Fatalf("import: %d (no debe haber avisos)\n%s", code, out)
	}
	data, _ := os.ReadFile(imported)
	text := string(data)
	for _, want := range []string{"type: geopackage", "type: shapefile", "file: file:/data/stores/places.gpkg",
		"coveragestores:", "url: file:/data/stores/dem.tif", "native_name: dem", "layergroups:"} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q en el YAML importado:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\n      password") || strings.Contains(text, "host:") {
		t.Errorf("los datastores de fichero no llevan conexión ni password:\n%s", text)
	}
	if code, out := run("plan", "--file", imported, "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan --prune tras import: %d\n%s", code, text)
	}

	cleanup()
	if code, _ := run("apply", "--file", imported, "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply del YAML importado: %d", code)
	}
	if code, out := run("plan", "--file", imported, "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras reaplicar: %d\n%s", code, out)
	}
	if cv, err := client.GetCoverage(ctx, ws, "dem", "relieve"); err != nil || cv.NativeName != "dem" {
		t.Fatalf("coverage relieve: %+v %v", cv, err)
	}
}
