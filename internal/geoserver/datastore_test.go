package geoserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/neocaos/geoform/internal/geoserver"
)

func TestConnectionParameters_MarshalJSON(t *testing.T) {
	got, err := json.Marshal(geoserver.ConnectionParameters{"port": "5432", "host": "db"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"entry":[{"@key":"host","$":"db"},{"@key":"port","$":"5432"}]}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestConnectionParameters_UnmarshalJSON(t *testing.T) {
	cases := map[string]struct {
		in   string
		want geoserver.ConnectionParameters
	}{
		"array":  {`{"entry":[{"@key":"host","$":"db"},{"@key":"port","$":"5432"}]}`, geoserver.ConnectionParameters{"host": "db", "port": "5432"}},
		"single": {`{"entry":{"@key":"url","$":"file:data/x.shp"}}`, geoserver.ConnectionParameters{"url": "file:data/x.shp"}},
		"empty":  {`{}`, geoserver.ConnectionParameters{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got geoserver.ConnectionParameters
			if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestClient_GetDataStore(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/geoserver/rest/workspaces/acme/datastores/roads.json" {
			t.Errorf("expected GET .../workspaces/acme/datastores/roads.json, got %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"dataStore":{"name":"roads","type":"PostGIS","enabled":true,
			"workspace":{"name":"acme","href":"http://x/acme.json"},
			"connectionParameters":{"entry":[{"@key":"dbtype","$":"postgis"},{"@key":"host","$":"db"}]}}}`)
	})

	ds, err := client.GetDataStore(context.Background(), "acme", "roads")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.Name != "roads" || ds.Type != "PostGIS" || !ds.Enabled || ds.ConnectionParameters["host"] != "db" {
		t.Errorf("unexpected data store: %+v", *ds)
	}
}

func TestClient_CreateDataStore(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/geoserver/rest/workspaces/acme/datastores" {
			t.Errorf("expected POST .../workspaces/acme/datastores, got %s %s", r.Method, r.URL.Path)
		}
		var env struct {
			DataStore geoserver.DataStore `json:"dataStore"`
		}
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Fatal(err)
		}
		if env.DataStore.Name != "roads" || !env.DataStore.Enabled || env.DataStore.ConnectionParameters["passwd"] != "pw" {
			t.Errorf("unexpected body: %+v", env.DataStore)
		}
		w.WriteHeader(http.StatusCreated)
	})

	err := client.CreateDataStore(context.Background(), "acme", geoserver.DataStore{
		Name: "roads", Enabled: true,
		ConnectionParameters: geoserver.ConnectionParameters{"passwd": "pw"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_UpdateAndDeleteDataStore(t *testing.T) {
	var calls []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	if err := client.UpdateDataStore(ctx, "acme", "roads", geoserver.DataStore{Name: "roads"}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteDataStore(ctx, "acme", "roads", false); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PUT /geoserver/rest/workspaces/acme/datastores/roads?",
		"DELETE /geoserver/rest/workspaces/acme/datastores/roads?recurse=false",
	}
	for i := range want {
		if i >= len(calls) || calls[i] != want[i] {
			t.Fatalf("calls = %q, want %q", calls, want)
		}
	}
}

func TestClient_ListAvailableFeatureTypes(t *testing.T) {
	cases := map[string]struct {
		body string
		want []string
	}{
		"empty":  {`{"list":""}`, nil},
		"single": {`{"list":{"string":"roads"}}`, []string{"roads"}},
		"many":   {`{"list":{"string":["roads","rivers"]}}`, []string{"roads", "rivers"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/geoserver/rest/workspaces/acme/datastores/pg/featuretypes.json" || r.URL.Query().Get("list") != "available" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				io.WriteString(w, tc.body)
			})
			got, err := client.ListAvailableFeatureTypes(context.Background(), "acme", "pg")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}
