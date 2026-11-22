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
	"github.com/neocaos/geoform/internal/infrastructure/importer"
	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

type stubSource struct{}

func (stubSource) ListWorkspaces(context.Context) ([]string, error) { return []string{"demo"}, nil }
func (stubSource) GetWorkspace(_ context.Context, n string) (*gs.Workspace, error) {
	return &gs.Workspace{Name: n}, nil
}
func (stubSource) ListDataStores(context.Context, string) ([]string, error) {
	return []string{"pg"}, nil
}
func (stubSource) GetDataStore(_ context.Context, _, n string) (*gs.DataStore, error) {
	return &gs.DataStore{Name: n, ConnectionParameters: gs.ConnectionParameters{
		"dbtype": "postgis", "host": "db", "port": "5432", "database": "geo", "schema": "public", "user": "u", "passwd": "****"}}, nil
}
func (stubSource) ListFeatureTypes(context.Context, string, string) ([]string, error) {
	return []string{"cities"}, nil
}
func (stubSource) GetFeatureType(_ context.Context, _, _, n string) (*gs.FeatureType, error) {
	return &gs.FeatureType{Name: n, NativeName: n, SRS: "EPSG:4326"}, nil
}

func TestImportCommand(t *testing.T) {
	out := filepath.Join(t.TempDir(), "geoform.yaml")
	runImp := func(args ...string) (int, string, string) {
		var o, e bytes.Buffer
		code := cli.Run(cli.Env{
			Args: append([]string{"import"}, args...), Stdout: &o, Stderr: &e,
			Getenv:    func(string) string { return "" },
			NewSource: func(cli.Conn) importer.Source { return stubSource{} },
		})
		return code, o.String(), e.String()
	}
	if code, so, se := runImp(); code != cli.ExitOK || !strings.Contains(so, "password_env:") {
		t.Fatalf("stdout: %d %s %s", code, so, se)
	}
	if code, _, se := runImp("--output", out); code != cli.ExitOK {
		t.Fatalf("output: %d %s", code, se)
	}
	if code, _, _ := runImp("--output", out); code != cli.ExitInvalidArg {
		t.Errorf("sin --force debe negarse a sobrescribir: %d", code)
	}
	if code, _, _ := runImp("--output", out, "--force"); code != cli.ExitOK {
		t.Errorf("--force: %d", code)
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "****") {
		t.Error("no debe filtrarse el password enmascarado")
	}
	if _, err := storage.Parse(bytes.NewReader(data), func(string) string { return "x" }); err != nil {
		t.Errorf("YAML generado inválido: %v", err)
	}
}
