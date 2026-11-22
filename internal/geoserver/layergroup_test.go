package geoserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/geoserver"
)

const groupsURL = "/geoserver/rest/workspaces/acme/layergroups"

func TestClient_CreateLayerGroup(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			serveLayerStyle(w, r)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != groupsURL {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		var body struct {
			LayerGroup struct {
				Name         string
				Mode         string
				Workspace    struct{ Name string }
				Publishables struct{ Published []struct{ Type, Name string } }
				Styles       struct{ Style []struct{ Name string } }
			}
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		g := body.LayerGroup
		if g.Name != "base" || g.Mode != "SINGLE" || g.Workspace.Name != "acme" || len(g.Publishables.Published) != 2 ||
			g.Publishables.Published[1].Name != "acme:roads" {
			t.Errorf("body = %s", raw)
		}
		// cities usa un estilo de workspace; roads uno global.
		if st := g.Styles.Style; len(st) != 2 || st[0].Name != "acme:rojo" || st[1].Name != "line" {
			t.Errorf("styles = %+v", st)
		}
		w.WriteHeader(http.StatusCreated)
	})
	err := client.CreateLayerGroup(context.Background(), "acme",
		geoserver.LayerGroup{Name: "base", Mode: "SINGLE", Layers: []string{"acme:cities", "acme:roads"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClient_GetLayerGroup(t *testing.T) {
	for body, want := range map[string][]string{
		`{"layerGroup":{"name":"g","mode":"SINGLE","title":"T","publishables":{"published":{"@type":"layer","name":"acme:a"}}}}`:                         {"acme:a"},
		`{"layerGroup":{"name":"g","mode":"SINGLE","publishables":{"published":[{"@type":"layer","name":"acme:a"},{"@type":"layer","name":"acme:b"}]}}}`: {"acme:a", "acme:b"},
	} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != groupsURL+"/g.json" {
				t.Errorf("path = %s", r.URL.Path)
			}
			io.WriteString(w, body)
		})
		g, err := client.GetLayerGroup(context.Background(), "acme", "g")
		if err != nil || !reflect.DeepEqual(g.Layers, want) || g.Mode != "SINGLE" {
			t.Errorf("%s -> %+v, %v", body, g, err)
		}
	}
}

func TestClient_LayerGroupUpdateDeleteList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/geoserver/rest/layers/"):
			serveLayerStyle(w, r)
		case r.Method == http.MethodPut && r.URL.Path == groupsURL+"/g":
		case r.Method == http.MethodDelete && r.URL.Path == groupsURL+"/g":
		case r.Method == http.MethodGet && r.URL.Path == groupsURL+".json":
			io.WriteString(w, `{"layerGroups":{"layerGroup":[{"name":"g"},{"name":"h"}]}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	})
	ctx := context.Background()
	if err := client.UpdateLayerGroup(ctx, "acme", "g", geoserver.LayerGroup{Name: "g", Layers: []string{"acme:a"}}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteLayerGroup(ctx, "acme", "g"); err != nil {
		t.Fatal(err)
	}
	names, err := client.ListLayerGroups(ctx, "acme")
	if err != nil || !reflect.DeepEqual(names, []string{"g", "h"}) {
		t.Fatalf("%v, %v", names, err)
	}
}

// serveLayerStyle responde GET /layers/acme:<capa>: cities tiene un estilo de
// workspace y el resto uno global.
func serveLayerStyle(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "acme:cities") {
		io.WriteString(w, `{"layer":{"defaultStyle":{"name":"acme:rojo","workspace":"acme"}}}`)
		return
	}
	io.WriteString(w, `{"layer":{"defaultStyle":{"name":"line"}}}`)
}
