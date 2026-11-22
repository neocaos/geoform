package geoserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/geoserver"
)

const stylesURL = "/geoserver/rest/workspaces/acme/styles"

const sld10 = `<StyledLayerDescriptor version="1.0.0"></StyledLayerDescriptor>`
const sld11 = `<StyledLayerDescriptor version="1.1.0"></StyledLayerDescriptor>`

// newRawClient no impone Accept: application/json, porque los estilos se
// descargan como XML.
func newRawClient(t *testing.T, h http.HandlerFunc) *geoserver.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return geoserver.NewClient(srv.URL+"/geoserver", "admin", "secret")
}

func TestClient_CreateStyle(t *testing.T) {
	for _, tc := range []struct{ sld, ctype string }{
		{sld10, "application/vnd.ogc.sld+xml"},
		{sld11, "application/vnd.ogc.se+xml"},
	} {
		client := newRawClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != stylesURL || r.URL.Query().Get("name") != "roads" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL)
			}
			if got := r.Header.Get("Content-Type"); got != tc.ctype {
				t.Errorf("Content-Type = %q, want %q", got, tc.ctype)
			}
			if b, _ := io.ReadAll(r.Body); string(b) != tc.sld {
				t.Errorf("body = %q", b)
			}
			w.WriteHeader(http.StatusCreated)
		})
		if err := client.CreateStyle(context.Background(), "acme", "roads", tc.sld); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClient_UpdateAndGetStyle(t *testing.T) {
	client := newRawClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == stylesURL+"/roads":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == stylesURL+"/roads.sld":
			io.WriteString(w, sld10)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	ctx := context.Background()
	if err := client.UpdateStyle(ctx, "acme", "roads", sld10); err != nil {
		t.Fatal(err)
	}
	got, err := client.GetStyleSLD(ctx, "acme", "roads")
	if err != nil || got != sld10 {
		t.Fatalf("GetStyleSLD = %q, %v", got, err)
	}
	if _, err := client.GetStyleSLD(ctx, "acme", "nope"); !geoserver.IsNotFound(err) {
		t.Fatalf("esperaba not found, got %v", err)
	}
}

func TestClient_DeleteStyle(t *testing.T) {
	var query string
	client := newRawClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != stylesURL+"/roads" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		query = r.URL.RawQuery
	})
	if err := client.DeleteStyle(context.Background(), "acme", "roads", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "recurse=true") || !strings.Contains(query, "purge=true") {
		t.Errorf("query = %q", query)
	}
}

func TestClient_ListStyles(t *testing.T) {
	for body, want := range map[string]int{
		`{"styles":""}`: 0,
		`{"styles":{"style":{"name":"a","href":"x"}}}`:                           1,
		`{"styles":{"style":[{"name":"a","href":"x"},{"name":"b","href":"y"}]}}`: 2,
	} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		names, err := client.ListStyles(context.Background(), "acme")
		if err != nil || len(names) != want {
			t.Errorf("%s: %v, %v", body, names, err)
		}
	}
}

func TestClient_LayerDefaultStyle(t *testing.T) {
	var put map[string]map[string]map[string]string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/geoserver/rest/layers/acme:roads.json" && r.URL.Path != "/geoserver/rest/layers/acme:roads" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Method == http.MethodPut {
			json.NewDecoder(r.Body).Decode(&put)
			return
		}
		io.WriteString(w, `{"layer":{"name":"roads","defaultStyle":{"name":"acme:red","workspace":"acme"}}}`)
	})
	ctx := context.Background()
	ref, err := client.GetLayerDefaultStyle(ctx, "acme", "roads")
	if err != nil || ref == nil || ref.Name != "red" || ref.Workspace != "acme" {
		t.Fatalf("ref = %+v, %v", ref, err)
	}
	if err := client.SetLayerDefaultStyle(ctx, "acme", "roads", geoserver.StyleRef{Name: "red", Workspace: "acme"}); err != nil {
		t.Fatal(err)
	}
	if ds := put["layer"]["defaultStyle"]; ds["name"] != "red" || ds["workspace"] != "acme" {
		t.Errorf("body = %v", put)
	}
}
