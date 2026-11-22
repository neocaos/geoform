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

// import sobre un GeoServer real y plan posterior: no debe haber diff.
func TestIntegration_ImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	url := testenv.URL()
	user := testenv.User()
	pass := testenv.Pass()
	client := gs.NewClient(url, user, pass)

	const ws = "geoform_import_it"
	cleanup := func() { _ = client.DeleteWorkspace(ctx, ws, true) }
	cleanup()
	t.Cleanup(cleanup)

	dir := t.TempDir()
	src := filepath.Join(dir, "src.yaml")
	imported := filepath.Join(dir, "imported.yaml")
	cfg := strings.ReplaceAll(itConfig, "geoform_cli_it", ws)
	cfg = strings.Replace(cfg, "%s", "Ciudades", 1)
	if err := os.WriteFile(src, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string {
		if k == "IT_PG_PASSWORD" || strings.HasSuffix(k, "_PASSWORD") {
			return "geopassword"
		}
		return ""
	}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := cli.Run(cli.Env{
			Args: append(args, "--server", url, "--user", user, "--password", pass), Stdin: strings.NewReader(""),
			Stdout: &out, Stderr: &errOut, Getenv: getenv, NewReconciler: cli.GeoServerReconciler,
		})
		t.Logf("geoform %s -> %d\n%s%s", args[0], code, out.String(), errOut.String())
		return code, out.String() + errOut.String()
	}

	if code, _ := run("apply", "--file", src, "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}
	if code, _ := run("import", "--workspace", ws, "--output", imported); code != cli.ExitOK {
		t.Fatalf("import: %d", code)
	}
	data, _ := os.ReadFile(imported)
	if strings.Contains(string(data), "geopassword") {
		t.Fatalf("el YAML importado contiene el password:\n%s", data)
	}
	if code, out := run("plan", "--file", imported, "--prune"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras import: %d\n%s", code, data)
	}
}
