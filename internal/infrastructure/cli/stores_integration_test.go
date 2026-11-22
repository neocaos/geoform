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
)

// Fixtures de test/testdata/stores, montados en /data/stores del contenedor.
const storesConfig = `
workspaces:
  - name: geoform_stores_it
    datastores:
      - name: gpkg
        type: geopackage
        file: file:/data/stores/places.gpkg
        layers:
          - {name: gpkg_places, table: places, title: %s, srs: "EPSG:4326"}
      - name: shp
        type: shapefile
        file: file:/data/stores/places.shp
        layers:
          - {name: shp_places, table: places, srs: "EPSG:4326"}
    coveragestores:
      - name: dem
        type: geotiff
        url: file:/data/stores/dem.tif
        coverages:
          - {name: dem, title: Relieve, srs: "EPSG:4326"}
`

func TestIntegration_StoreTypesLifecycle(t *testing.T) {
	ctx := context.Background()
	url := itEnv("GEOSERVER_URL", "http://localhost:18080/geoserver")
	user := itEnv("GEOSERVER_USER", "admin")
	pass := itEnv("GEOSERVER_PASS", "geoserver")
	client := gs.NewClient(url, user, pass)

	const ws = "geoform_stores_it"
	cleanup := func() { _ = client.DeleteWorkspace(ctx, ws, true) }
	cleanup()
	t.Cleanup(cleanup)

	path := filepath.Join(t.TempDir(), "geoform.yaml")
	writeCfg := func(title string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(strings.Replace(storesConfig, "%s", title, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		full := append(args, "--file", path, "--server", url, "--user", user, "--password", pass)
		code := cli.Run(cli.Env{
			Args: full, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut,
			Getenv: func(string) string { return "" }, NewReconciler: cli.GeoServerReconciler,
		})
		t.Logf("geoform %s -> %d\n%s%s", strings.Join(args, " "), code, out.String(), errOut.String())
		return code, out.String() + errOut.String()
	}

	writeCfg("Lugares")
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "Plan: 7 a crear") {
		t.Fatalf("plan inicial: %d", code)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}

	// Lo publicado en GeoServer.
	if ds, err := client.GetDataStore(ctx, ws, "gpkg"); err != nil || ds.ConnectionParameters["dbtype"] != "geopkg" {
		t.Fatalf("gpkg: %+v %v", ds, err)
	}
	if ft, err := client.GetFeatureType(ctx, ws, "shp", "shp_places"); err != nil || ft.NativeName != "places" {
		t.Fatalf("shp_places: %+v %v", ft, err)
	}
	cv, err := client.GetCoverage(ctx, ws, "dem", "dem")
	if err != nil || cv.Title != "Relieve" || cv.SRS != "EPSG:4326" {
		t.Fatalf("dem: %+v %v", cv, err)
	}
	if ok, err := client.LayerExists(ctx, ws, "dem"); err != nil || !ok {
		t.Fatalf("la capa raster debía existir: %v %v", ok, err)
	}

	// Idempotente.
	if code, out := run("plan", "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras apply: %d", code)
	}

	// Update: título vectorial y raster.
	writeCfg("Sitios")
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "title:") {
		t.Fatalf("plan update: %d", code)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply update: %d", code)
	}

	// Prune: un coveragestore ajeno a la config solo se borra con --prune.
	if err := client.CreateCoverageStore(ctx, ws, gs.CoverageStore{Name: "extra", Type: gs.CoverageStoreTypeGeoTIFF, Enabled: true, URL: "file:/data/stores/dem.tif"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply sin prune: %d", code)
	}
	if _, err := client.GetCoverageStore(ctx, ws, "extra"); err != nil {
		t.Fatalf("sin --prune el almacén extra debe sobrevivir: %v", err)
	}
	if code, out := run("plan", "--prune"); code != cli.ExitDiff || !strings.Contains(out, "- coveragestore "+ws+"/extra") {
		t.Fatalf("plan --prune: %d", code)
	}
	if code, _ := run("apply", "--prune", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply --prune: %d", code)
	}
	if _, err := client.GetCoverageStore(ctx, ws, "extra"); !gs.IsNotFound(err) {
		t.Fatalf("extra debía borrarse: %v", err)
	}
	if _, err := client.GetCoverage(ctx, ws, "dem", "dem"); err != nil {
		t.Fatalf("dem no debía borrarse: %v", err)
	}
}
