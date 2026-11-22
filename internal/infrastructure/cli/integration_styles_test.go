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

const itStylesConfig = `
workspaces:
  - name: geoform_cli_styles_it
    styles:
      - name: rojo
        file: styles/rojo.sld
      - name: azul
        file: styles/azul.sld
    layergroups:
      - name: base
        title: Base
        layers: [regions, cities]
    datastores:
      - name: postgis
        host: db
        port: 5432
        database: geoform
        schema: geodata
        user: geouser
        password: geopassword
        layers:
          - name: cities
            srs: EPSG:4326
            style: %s
          - name: regions
            srs: EPSG:4326
            style: polygon
`

func TestIntegration_CLIStylesAndLayerGroups(t *testing.T) {
	ctx := context.Background()
	url := testenv.URL()
	user := testenv.User()
	pass := testenv.Pass()
	client := gs.NewClient(url, user, pass)

	const ws = "geoform_cli_styles_it"
	cleanup := func() { _ = client.DeleteWorkspace(ctx, ws, true) }
	cleanup()
	t.Cleanup(cleanup)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{"rojo.sld": "point_red.sld", "azul.sld": "point_blue.sld"} {
		b, err := os.ReadFile("../../../test/testdata/sld/" + src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "styles", name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "geoform.yaml")
	writeCfg := func(style string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(strings.Replace(itStylesConfig, "%s", style, 1)), 0o600); err != nil {
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
			Args: full, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut,
			Getenv: func(string) string { return "" }, NewReconciler: cli.GeoServerReconciler,
		})
		t.Logf("geoform %s -> %d\n%s%s", strings.Join(args, " "), code, out.String(), errOut.String())
		return code, out.String() + errOut.String()
	}

	writeCfg("rojo")
	if code, _ := run("validate"); code != cli.ExitOK {
		t.Fatalf("validate: %d", code)
	}
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "+ style "+ws+"/rojo") || !strings.Contains(out, "+ layergroup "+ws+"/base") {
		t.Fatalf("plan inicial: %d", code)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}
	if ref, err := client.GetLayerDefaultStyle(ctx, ws, "cities"); err != nil || ref == nil || ref.Name != "rojo" || ref.Workspace != ws {
		t.Fatalf("estilo de cities: %+v %v", ref, err)
	}
	if g, err := client.GetLayerGroup(ctx, ws, "base"); err != nil || len(g.Layers) != 2 || g.Layers[0] != ws+":regions" {
		t.Fatalf("grupo: %+v %v", g, err)
	}

	// Idempotente, incluida la comparación del SLD y del estilo global.
	if code, out := run("plan", "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras apply: %d", code)
	}

	// Cambiar el estilo de la capa y el SLD de rojo.
	writeCfg("azul")
	if err := os.WriteFile(filepath.Join(dir, "styles", "rojo.sld"), []byte(strings.ReplaceAll(readFile(t, filepath.Join(dir, "styles", "rojo.sld")), "#ff0000", "#00ff00")), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := run("plan"); code != cli.ExitDiff || !strings.Contains(out, "~ style "+ws+"/rojo") || !strings.Contains(out, "~ layer "+ws+"/postgis/cities") {
		t.Fatalf("plan update: %d", code)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply update: %d", code)
	}
	if ref, _ := client.GetLayerDefaultStyle(ctx, ws, "cities"); ref == nil || ref.Name != "azul" {
		t.Fatalf("estilo sin actualizar: %+v", ref)
	}
	if code, out := run("plan"); code != cli.ExitOK {
		t.Fatalf("plan tras update: %d\n%s", code, out)
	}

	// Prune: un estilo y un grupo ajenos a la config.
	if err := client.CreateStyle(ctx, ws, "extra", readFile(t, filepath.Join(dir, "styles", "azul.sld"))); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateLayerGroup(ctx, ws, gs.LayerGroup{Name: "extra", Mode: "SINGLE", Layers: []string{ws + ":cities"}}); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply sin prune: %d", code)
	}
	if _, err := client.GetStyleSLD(ctx, ws, "extra"); err != nil {
		t.Fatalf("sin --prune el estilo extra debe sobrevivir: %v", err)
	}
	if code, _ := run("apply", "--prune", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply --prune: %d", code)
	}
	if _, err := client.GetStyleSLD(ctx, ws, "extra"); !gs.IsNotFound(err) {
		t.Fatalf("estilo extra debía borrarse: %v", err)
	}
	if _, err := client.GetLayerGroup(ctx, ws, "extra"); !gs.IsNotFound(err) {
		t.Fatalf("grupo extra debía borrarse: %v", err)
	}
	if _, err := client.GetLayerGroup(ctx, ws, "base"); err != nil {
		t.Fatalf("grupo base no debía borrarse: %v", err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
