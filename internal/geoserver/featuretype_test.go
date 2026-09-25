package geoserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/neocaos/geoform/internal/geoserver"
)

const featureTypesURL = "/geoserver/rest/workspaces/acme/datastores/pg/featuretypes"

func TestClient_GetFeatureType(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != featureTypesURL+"/roads.json" {
			t.Errorf("expected GET %s/roads.json, got %s %s", featureTypesURL, r.Method, r.URL.Path)
		}
		// Real responses mix a string CRS and a WKT-object CRS.
		io.WriteString(w, `{"featureType":{"name":"roads","nativeName":"roads_tbl","title":"Roads",
			"srs":"EPSG:3857","projectionPolicy":"REPROJECT_TO_DECLARED","enabled":true,
			"nativeBoundingBox":{"minx":1,"maxx":2,"miny":3,"maxy":4,"crs":{"@class":"projected","$":"PROJCS[...]"}},
			"latLonBoundingBox":{"minx":-1,"maxx":1,"miny":-2,"maxy":2,"crs":"EPSG:4326"}}}`)
	})

	ft, err := client.GetFeatureType(context.Background(), "acme", "pg", "roads")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ft.NativeName != "roads_tbl" || ft.SRS != "EPSG:3857" || ft.ProjectionPolicy != geoserver.ProjectionReprojectToDeclared {
		t.Errorf("unexpected feature type: %+v", *ft)
	}
	if ft.LatLonBBox == nil || ft.LatLonBBox.MaxY != 2 || ft.NativeBBox == nil || ft.NativeBBox.MinX != 1 {
		t.Errorf("unexpected bounding boxes: %+v / %+v", ft.NativeBBox, ft.LatLonBBox)
	}
}

func TestClient_CreateFeatureType(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != featureTypesURL {
			t.Errorf("expected POST %s, got %s %s", featureTypesURL, r.Method, r.URL.Path)
		}
		var body map[string]map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		ft := body["featureType"]
		if ft["name"] != "roads" || ft["nativeName"] != "roads_tbl" {
			t.Errorf("unexpected body: %v", ft)
		}
		// Unset bounding boxes must be omitted so GeoServer computes them.
		for _, k := range []string{"nativeBoundingBox", "latLonBoundingBox", "srs"} {
			if _, ok := ft[k]; ok {
				t.Errorf("%s should be omitted when unset", k)
			}
		}
		w.WriteHeader(http.StatusCreated)
	})

	err := client.CreateFeatureType(context.Background(), "acme", "pg",
		geoserver.FeatureType{Name: "roads", NativeName: "roads_tbl", Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_UpdateAndDeleteFeatureType(t *testing.T) {
	var calls []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	ft := geoserver.FeatureType{Name: "roads"}
	if err := client.UpdateFeatureType(ctx, "acme", "pg", "roads", ft, false); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateFeatureType(ctx, "acme", "pg", "roads", ft, true); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteFeatureType(ctx, "acme", "pg", "roads", true); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PUT " + featureTypesURL + "/roads?",
		"PUT " + featureTypesURL + "/roads?recalculate=nativebbox,latlonbbox",
		"DELETE " + featureTypesURL + "/roads?recurse=true",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %q, want %q", calls, want)
		}
	}
}
