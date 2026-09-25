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

// newTestClient starts a server running handler after checking the request
// is authenticated, and returns a client pointed at it.
func newTestClient(t *testing.T, handler http.HandlerFunc) *geoserver.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			t.Errorf("expected basic auth admin/secret, got %q/%q (ok=%t)", user, pass, ok)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("expected Accept: application/json, got %q", got)
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return geoserver.NewClient(server.URL+"/geoserver", "admin", "secret")
}

func decodeWorkspace(t *testing.T, r *http.Request) geoserver.Workspace {
	t.Helper()
	if got := r.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type: application/json, got %q", got)
	}
	var env struct {
		Workspace geoserver.Workspace `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		t.Fatalf("decoding request body: %v", err)
	}
	return env.Workspace
}

func TestNewClient_AcceptsRestSuffix(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	for _, base := range []string{server.URL + "/geoserver", server.URL + "/geoserver/", server.URL + "/geoserver/rest/"} {
		client := geoserver.NewClient(base, "admin", "secret")
		if err := client.CreateWorkspace(context.Background(), geoserver.Workspace{Name: "acme"}); err != nil {
			t.Fatalf("%s: unexpected error: %v", base, err)
		}
		if gotPath != "/geoserver/rest/workspaces" {
			t.Errorf("%s: expected path /geoserver/rest/workspaces, got %s", base, gotPath)
		}
	}
}

func TestClient_GetWorkspace(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/geoserver/rest/workspaces/acme.json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("quietOnNotFound") != "true" {
			t.Errorf("expected quietOnNotFound=true")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"workspace":{"name":"acme","isolated":true,"dateCreated":"2026-09-25 10:00:00.0 UTC"}}`)
	})

	ws, err := client.GetWorkspace(context.Background(), "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.Name != "acme" || !ws.Isolated {
		t.Errorf("expected {acme true}, got %+v", *ws)
	}
}

func TestClient_GetWorkspace_NotFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := client.GetWorkspace(context.Background(), "missing")
	if !geoserver.IsNotFound(err) {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}

func TestClient_CreateWorkspace(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/geoserver/rest/workspaces" {
			t.Errorf("expected POST /geoserver/rest/workspaces, got %s %s", r.Method, r.URL.Path)
		}
		if ws := decodeWorkspace(t, r); ws.Name != "acme" || !ws.Isolated {
			t.Errorf("expected body {acme true}, got %+v", ws)
		}
		w.WriteHeader(http.StatusCreated)
	})

	if err := client.CreateWorkspace(context.Background(), geoserver.Workspace{Name: "acme", Isolated: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_CreateWorkspace_Error(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "boom\n")
	})

	err := client.CreateWorkspace(context.Background(), geoserver.Workspace{Name: "acme"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if geoserver.IsNotFound(err) {
		t.Error("a 500 must not be reported as not found")
	}
	if !strings.Contains(err.Error(), "500: boom") {
		t.Errorf("expected status and body in error, got: %v", err)
	}
}

func TestClient_UpdateWorkspace(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/geoserver/rest/workspaces/acme" {
			t.Errorf("expected PUT /geoserver/rest/workspaces/acme, got %s %s", r.Method, r.URL.Path)
		}
		if ws := decodeWorkspace(t, r); ws.Name != "acme" || ws.Isolated {
			t.Errorf("expected body {acme false}, got %+v", ws)
		}
		w.WriteHeader(http.StatusOK)
	})

	if err := client.UpdateWorkspace(context.Background(), "acme", geoserver.Workspace{Name: "acme"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_DeleteWorkspace(t *testing.T) {
	for _, recurse := range []bool{true, false} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete || r.URL.Path != "/geoserver/rest/workspaces/acme" {
				t.Errorf("expected DELETE /geoserver/rest/workspaces/acme, got %s %s", r.Method, r.URL.Path)
			}
			want := "false"
			if recurse {
				want = "true"
			}
			if got := r.URL.Query().Get("recurse"); got != want {
				t.Errorf("expected recurse=%s, got %q", want, got)
			}
			w.WriteHeader(http.StatusOK)
		})

		if err := client.DeleteWorkspace(context.Background(), "acme", recurse); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestClient_DeleteWorkspace_NotFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	err := client.DeleteWorkspace(context.Background(), "acme", true)
	if !geoserver.IsNotFound(err) {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}
