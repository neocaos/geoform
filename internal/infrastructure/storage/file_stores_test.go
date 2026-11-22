package storage_test

import (
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/domain/coverage"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

const storesYAML = `
workspaces:
  - name: demo
    datastores:
      - name: gpkg
        type: geopackage
        file: file:/data/places.gpkg
        layers:
          - {name: places, srs: "EPSG:4326"}
      - name: shp
        type: Shapefile
        file: file:/data/places.shp
        layers:
          - {name: shp_places, table: places, srs: "EPSG:4326"}
    coveragestores:
      - name: dem
        url: file:/data/dem.tif
        coverages:
          - {name: dem, native_name: dem, title: Relieve, srs: "EPSG:4326"}
`

func TestParse_FileStoresAndCoverages(t *testing.T) {
	d, err := storage.Parse(strings.NewReader(storesYAML), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Datastores) != 2 || d.Datastores[0].Type != datastore.TypeGeoPackage || d.Datastores[0].File != "file:/data/places.gpkg" ||
		d.Datastores[1].Type != datastore.TypeShapefile {
		t.Errorf("datastores: %+v", d.Datastores)
	}
	if len(d.Layers) != 2 || d.Layers[1].NativeTable() != "places" {
		t.Errorf("layers: %+v", d.Layers)
	}
	if len(d.CoverageStores) != 1 || d.CoverageStores[0].URL != "file:/data/dem.tif" || d.CoverageStores[0].Kind() != coverage.TypeGeoTIFF {
		t.Errorf("coveragestores: %+v", d.CoverageStores)
	}
	if len(d.Coverages) != 1 || d.Coverages[0].Store != "dem" || d.Coverages[0].Title != "Relieve" {
		t.Errorf("coverages: %+v", d.Coverages)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestParse_FileStoresErrors(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"sin file": {`
workspaces:
  - name: demo
    datastores:
      - {name: g, type: geopackage}
`, "workspaces[0].datastores[0].file: es obligatorio"},
		"host en gpkg": {`
workspaces:
  - name: demo
    datastores:
      - {name: g, type: shapefile, file: "file:/a.shp", host: db}
`, "datastores[0].host: no aplica al tipo shapefile"},
		"tipo desconocido": {`
workspaces:
  - name: demo
    datastores:
      - {name: g, type: csv, file: x}
`, `tipo "csv" no soportado`},
		"file en postgis": {`
workspaces:
  - name: demo
    datastores:
      - {name: g, host: h, database: d, user: u, file: x}
`, "file: solo aplica"},
		"coverage sin url": {`
workspaces:
  - name: demo
    coveragestores:
      - name: dem
        coverages: [{name: dem, srs: "4326"}]
`, "coveragestores[0].url: es obligatorio"},
		"srs coverage": {`
workspaces:
  - name: demo
    coveragestores:
      - name: dem
        url: file:/a.tif
        coverages: [{name: dem, srs: "4326"}]
`, "coverages[0].srs: SRS"},
		"tipo raster": {`
workspaces:
  - name: demo
    coveragestores:
      - {name: dem, type: jp2, url: x}
`, `tipo "jp2" no soportado`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := storage.Parse(strings.NewReader(tc.yaml), env(nil))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, quería %q", err, tc.want)
			}
		})
	}
}
