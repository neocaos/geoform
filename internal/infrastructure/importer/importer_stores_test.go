package importer_test

import (
	"context"
	"strings"
	"testing"

	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/importer"
	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

// rasterSource añade almacenes raster a fakeSource.
type rasterSource struct{ fakeSource }

func (rasterSource) ListWorkspaces(context.Context) ([]string, error) { return []string{"demo"}, nil }
func (rasterSource) ListDataStores(context.Context, string) ([]string, error) {
	return []string{"gpkg"}, nil
}
func (rasterSource) GetDataStore(_ context.Context, _, n string) (*gs.DataStore, error) {
	ds := gs.GeoPackageDataStore(n, "file:/data/a.gpkg")
	ds.ConnectionParameters["passwd"] = "secret-value"
	return &ds, nil
}
func (rasterSource) ListFeatureTypes(context.Context, string, string) ([]string, error) {
	return []string{"places"}, nil
}
func (rasterSource) ListCoverageStores(context.Context, string) ([]string, error) {
	return []string{"dem", "mosaic"}, nil
}
func (rasterSource) GetCoverageStore(_ context.Context, _, n string) (*gs.CoverageStore, error) {
	if n == "mosaic" {
		return &gs.CoverageStore{Name: n, Type: "ImageMosaic", URL: "file:/data/m"}, nil
	}
	return &gs.CoverageStore{Name: n, Type: "GeoTIFF", URL: "file:/data/dem.tif"}, nil
}
func (rasterSource) ListCoverages(context.Context, string, string) ([]string, error) {
	return []string{"relieve", "raro"}, nil
}
func (rasterSource) GetCoverage(_ context.Context, _, _, n string) (*gs.Coverage, error) {
	if n == "raro" {
		return &gs.Coverage{Name: n, NativeName: n, SRS: "WGS84"}, nil
	}
	return &gs.Coverage{Name: n, NativeName: "dem", Title: "Relieve", SRS: "EPSG:4326"}, nil
}

func TestImport_FileStoresAndRaster(t *testing.T) {
	res, err := importer.Import(context.Background(), rasterSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 2 {
		t.Errorf("avisos (mosaic y SRS raro): %v", res.Warnings)
	}
	data, err := storage.Marshal(res.Desired)
	if err != nil {
		t.Fatal(err)
	}
	y := string(data)
	for _, bad := range []string{"secret-value", "password", "host:", "database:", "user:"} {
		if strings.Contains(y, bad) {
			t.Errorf("el YAML de un datastore de fichero no debe contener %q:\n%s", bad, y)
		}
	}
	back, err := storage.Parse(strings.NewReader(y), func(string) string { return "" })
	if err != nil {
		t.Fatalf("YAML inválido: %v\n%s", err, y)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("Desired inválido: %v", err)
	}
	if len(back.Datastores) != 1 || back.Datastores[0].File != "file:/data/a.gpkg" ||
		len(back.CoverageStores) != 1 || back.CoverageStores[0].URL != "file:/data/dem.tif" ||
		len(back.Coverages) != 1 || back.Coverages[0].NativeName() != "dem" || back.Coverages[0].Title != "Relieve" {
		t.Errorf("tras releer: %+v", back)
	}
}
