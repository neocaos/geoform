package importer_test

import (
	"context"
	"strings"
	"testing"

	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/importer"
	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

type fakeSource struct{}

func (fakeSource) ListWorkspaces(context.Context) ([]string, error) {
	return []string{"zeta", "demo", "bad ws"}, nil
}
func (fakeSource) GetWorkspace(_ context.Context, n string) (*gs.Workspace, error) {
	return &gs.Workspace{Name: n, Isolated: n == "zeta"}, nil
}
func (fakeSource) ListDataStores(_ context.Context, ws string) ([]string, error) {
	if ws == "demo" {
		return []string{"pg", "shp"}, nil
	}
	return nil, nil
}
func (fakeSource) GetDataStore(_ context.Context, _, n string) (*gs.DataStore, error) {
	if n == "shp" {
		return &gs.DataStore{Name: n, Type: "Shapefile", ConnectionParameters: gs.ConnectionParameters{"url": "file:x.shp"}}, nil
	}
	return &gs.DataStore{Name: n, Type: "PostGIS", ConnectionParameters: gs.ConnectionParameters{
		"dbtype": "postgis", "host": "db", "port": "5432", "database": "geo", "schema": "public", "user": "u", "passwd": "secret-value",
	}}, nil
}
func (fakeSource) ListFeatureTypes(context.Context, string, string) ([]string, error) {
	return []string{"cities", "odd", "alias"}, nil
}
func (fakeSource) GetFeatureType(_ context.Context, _, _, n string) (*gs.FeatureType, error) {
	switch n {
	case "odd":
		return &gs.FeatureType{Name: n, NativeName: n, SRS: "urn:ogc:def:crs:OGC:1.3:CRS84"}, nil
	case "alias":
		return &gs.FeatureType{Name: n, NativeName: "real_table", Title: "Alias", SRS: "EPSG:3857"}, nil
	}
	return &gs.FeatureType{Name: n, NativeName: n, Title: n, SRS: "EPSG:4326"}, nil
}

func TestImport_RoundTripYAML(t *testing.T) {
	res, err := importer.Import(context.Background(), fakeSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 3 {
		t.Errorf("avisos: %v", res.Warnings)
	}
	data, err := storage.Marshal(res.Desired)
	if err != nil {
		t.Fatal(err)
	}
	y := string(data)
	if strings.Contains(y, "secret-value") || strings.Contains(y, "password:") {
		t.Fatalf("el YAML no debe contener secretos:\n%s", y)
	}
	if !strings.Contains(y, "password_env: GEOFORM_DEMO_PG_PASSWORD") {
		t.Errorf("falta el marcador de password:\n%s", y)
	}
	back, err := storage.Parse(strings.NewReader(y), func(string) string { return "x" })
	if err != nil {
		t.Fatalf("el YAML generado no valida: %v\n%s", err, y)
	}
	if len(back.Workspaces) != 2 || len(back.Datastores) != 1 || len(back.Layers) != 2 {
		t.Errorf("tras releer: %+v", back)
	}
	if back.Layers[0].Table != "real_table" {
		t.Errorf("tabla nativa perdida: %+v", back.Layers)
	}
}

func TestImport_UnknownWorkspace(t *testing.T) {
	if _, err := importer.Import(context.Background(), fakeSource{}, []string{"nope"}); err == nil {
		t.Fatal("se esperaba error")
	}
}
