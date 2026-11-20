package geoserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"geoform/internal/infrastructure/geoserver"
)

func TestClient_GetWorkspace_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/acme.json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"workspace": map[string]any{
				"name":     "acme",
				"isolated": false,
			},
		})
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")

	ws, err := client.GetWorkspace(context.Background(), "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.Name != "acme" {
		t.Errorf("expected name 'acme', got %q", ws.Name)
	}
}

func TestClient_GetWorkspace_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")

	_, err := client.GetWorkspace(context.Background(), "no-existe")
	if !geoserver.IsNotFound(err) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestClient_CreateWorkspace_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")

	err := client.CreateWorkspace(context.Background(), "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_CreateWorkspace_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")

	err := client.CreateWorkspace(context.Background(), "acme")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClient_DeleteWorkspace_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Query().Get("recurse") != "true" {
			t.Errorf("expected recurse=true query param, got %q", r.URL.Query().Get("recurse"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")

	err := client.DeleteWorkspace(context.Background(), "acme", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_DeleteWorkspace_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")

	err := client.DeleteWorkspace(context.Background(), "acme", true)
	if !geoserver.IsNotFound(err) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestClient_CreateDataStore_Body(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/acme/datastores" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")
	err := client.CreateDataStore(context.Background(), "acme", geoserver.DataStore{Name: "pg", Type: "PostGIS", Params: map[string]string{"host": "db"}})
	if err != nil {
		t.Fatal(err)
	}
	ds := got["dataStore"].(map[string]any)
	if ds["name"] != "pg" {
		t.Errorf("name = %v", ds["name"])
	}
}

func TestClient_GetFeatureType_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := geoserver.NewClient(server.URL, "admin", "geoserver")
	if _, err := client.GetFeatureType(context.Background(), "a", "b", "c"); !geoserver.IsNotFound(err) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
