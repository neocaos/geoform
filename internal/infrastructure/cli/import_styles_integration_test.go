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

// apply con estilos y grupos, import y plan --prune: sin diff y sin propuestas
// de borrado. Luego se aplica el YAML importado sobre un workspace vacío.
func TestIntegration_ImportRoundTripStylesAndGroups(t *testing.T) {
	ctx := context.Background()
	url := itEnv("GEOSERVER_URL", "http://localhost:18080/geoserver")
	user := itEnv("GEOSERVER_USER", "admin")
	pass := itEnv("GEOSERVER_PASS", "geoserver")
	client := gs.NewClient(url, user, pass)

	const ws = "geoform_import_styles_it"
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
	srcCfg := filepath.Join(dir, "src.yaml")
	imported := filepath.Join(dir, "imported.yaml")
	cfg := strings.ReplaceAll(itStylesConfig, "geoform_cli_styles_it", ws)
	cfg = strings.Replace(cfg, "%s", "rojo", 1)
	if err := os.WriteFile(srcCfg, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	getenv := func(k string) string {
		if strings.HasSuffix(k, "_PASSWORD") {
			return "geopassword"
		}
		return ""
	}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		code := cli.Run(cli.Env{
			Args: append(args, "--server", url, "--user", user, "--password", pass), Stdin: strings.NewReader(""),
			Stdout: &out, Stderr: &errOut, Getenv: getenv, NewReconciler: cli.GeoServerReconciler,
		})
		t.Logf("geoform %s -> %d\n%s%s", args[0], code, out.String(), errOut.String())
		return code, out.String() + errOut.String()
	}

	if code, _ := run("apply", "--file", srcCfg, "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}
	if code, _ := run("import", "--workspace", ws, "--output", imported); code != cli.ExitOK {
		t.Fatalf("import: %d", code)
	}
	data, _ := os.ReadFile(imported)
	text := string(data)
	if strings.Contains(text, "geopassword") {
		t.Fatalf("el YAML importado contiene el password:\n%s", text)
	}
	for _, want := range []string{"styles:", "layergroups:", "style: rojo", "style: polygon", "name: azul", "StyledLayerDescriptor"} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q en el YAML importado:\n%s", want, text)
		}
	}
	if code, out := run("plan", "--file", imported, "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan --prune tras import: %d\n%s", code, text)
	}

	// El YAML importado reproduce la configuración en un workspace vacío.
	cleanup()
	if code, _ := run("apply", "--file", imported, "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply del YAML importado: %d", code)
	}
	if code, out := run("plan", "--file", imported, "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras reaplicar: %d\n%s", code, out)
	}
	if ref, _ := client.GetLayerDefaultStyle(ctx, ws, "cities"); ref == nil || ref.Name != "rojo" || ref.Workspace != ws {
		t.Fatalf("estilo de cities: %+v", ref)
	}
}
