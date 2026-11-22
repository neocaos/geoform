package geoserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/neocaos/geoform/internal/geoserver"
)

func TestFileDataStoreConstructors(t *testing.T) {
	gpkg := geoserver.GeoPackageDataStore("g", "file:/data/a.gpkg")
	if gpkg.Type != "GeoPackage" || gpkg.ConnectionParameters["dbtype"] != "geopkg" ||
		gpkg.ConnectionParameters["database"] != "file:/data/a.gpkg" || !gpkg.Enabled {
		t.Errorf("geopackage inesperado: %+v", gpkg)
	}
	shp := geoserver.ShapefileDataStore("s", "file:/data/a.shp")
	if shp.Type != "Shapefile" || shp.ConnectionParameters["url"] != "file:/data/a.shp" {
		t.Errorf("shapefile inesperado: %+v", shp)
	}
}

func TestClient_CoverageStore(t *testing.T) {
	var calls []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == "/geoserver/rest/workspaces/acme/coveragestores.json" {
				io.WriteString(w, `{"coverageStores":{"coverageStore":[{"name":"a"},{"name":"b"}]}}`)
				return
			}
			io.WriteString(w, `{"coverageStore":{"name":"dem","type":"GeoTIFF","enabled":true,"url":"file:/data/dem.tif"}}`)
		case http.MethodPost:
			var env struct {
				CoverageStore geoserver.CoverageStore `json:"coverageStore"`
			}
			if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
				t.Fatal(err)
			}
			if env.CoverageStore.Name != "dem" || env.CoverageStore.Type != "GeoTIFF" || env.CoverageStore.URL != "file:/data/dem.tif" {
				t.Errorf("cuerpo inesperado: %+v", env.CoverageStore)
			}
			w.WriteHeader(http.StatusCreated)
		}
	})
	ctx := context.Background()
	cs, err := client.GetCoverageStore(ctx, "acme", "dem")
	if err != nil || cs.URL != "file:/data/dem.tif" || cs.Type != "GeoTIFF" {
		t.Fatalf("get: %+v %v", cs, err)
	}
	if err := client.CreateCoverageStore(ctx, "acme", geoserver.CoverageStore{Name: "dem", Type: "GeoTIFF", Enabled: true, URL: "file:/data/dem.tif"}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateCoverageStore(ctx, "acme", "dem", geoserver.CoverageStore{Name: "dem"}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteCoverageStore(ctx, "acme", "dem", true); err != nil {
		t.Fatal(err)
	}
	names, err := client.ListCoverageStores(ctx, "acme")
	if err != nil || len(names) != 2 || names[1] != "b" {
		t.Fatalf("list: %v %v", names, err)
	}
	want := []string{
		"GET /geoserver/rest/workspaces/acme/coveragestores/dem.json?quietOnNotFound=true",
		"POST /geoserver/rest/workspaces/acme/coveragestores?",
		"PUT /geoserver/rest/workspaces/acme/coveragestores/dem?",
		"DELETE /geoserver/rest/workspaces/acme/coveragestores/dem?purge=none&recurse=true",
		"GET /geoserver/rest/workspaces/acme/coveragestores.json?",
	}
	for i := range want {
		if i >= len(calls) || calls[i] != want[i] {
			t.Fatalf("calls = %q, want %q", calls, want)
		}
	}
}

func TestClient_Coverage(t *testing.T) {
	var calls []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/geoserver/rest/workspaces/acme/coveragestores/dem/coverages.json":
			io.WriteString(w, `{"coverages":""}`)
		case r.Method == http.MethodGet:
			io.WriteString(w, `{"coverage":{"name":"dem","nativeName":"dem","title":"DEM","srs":"EPSG:4326","enabled":true}}`)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
		}
	})
	ctx := context.Background()
	cv, err := client.GetCoverage(ctx, "acme", "dem", "dem")
	if err != nil || cv.Title != "DEM" || cv.SRS != "EPSG:4326" {
		t.Fatalf("get: %+v %v", cv, err)
	}
	if err := client.CreateCoverage(ctx, "acme", "dem", geoserver.Coverage{Name: "dem", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateCoverage(ctx, "acme", "dem", "dem", geoserver.Coverage{Name: "dem"}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteCoverage(ctx, "acme", "dem", "dem", true); err != nil {
		t.Fatal(err)
	}
	names, err := client.ListCoverages(ctx, "acme", "dem")
	if err != nil || len(names) != 0 {
		t.Fatalf("list: %v %v", names, err)
	}
	if calls[3] != "DELETE /geoserver/rest/workspaces/acme/coveragestores/dem/coverages/dem?recurse=true" {
		t.Errorf("calls = %q", calls)
	}
}
