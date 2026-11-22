package datastore_test

import (
	"testing"

	"github.com/neocaos/geoform/internal/domain/datastore"
)

func TestDatastore_ValidateFileTypes(t *testing.T) {
	cases := map[string]struct {
		d  datastore.Datastore
		ok bool
	}{
		"geopackage ok":     {datastore.Datastore{Workspace: "w", Name: "n", Type: datastore.TypeGeoPackage, File: "file:/d/a.gpkg"}, true},
		"shapefile ok":      {datastore.Datastore{Workspace: "w", Name: "n", Type: datastore.TypeShapefile, File: "file:/d/a.shp"}, true},
		"sin file":          {datastore.Datastore{Workspace: "w", Name: "n", Type: datastore.TypeShapefile}, false},
		"file con postgis":  {datastore.Datastore{Workspace: "w", Name: "n", Type: datastore.TypeGeoPackage, File: "x", PostGIS: datastore.PostGIS{Host: "h"}}, false},
		"tipo desconocido":  {datastore.Datastore{Workspace: "w", Name: "n", Type: "csv", File: "x"}, false},
		"postgis implícito": {datastore.Datastore{Workspace: "w", Name: "n", PostGIS: datastore.PostGIS{Host: "h", Database: "d", User: "u"}}, true},
		"postgis sin host":  {datastore.Datastore{Workspace: "w", Name: "n", Type: datastore.TypePostGIS}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.d.Validate(); (err == nil) != tc.ok {
				t.Fatalf("Validate() = %v, ok esperado %t", err, tc.ok)
			}
		})
	}
}
