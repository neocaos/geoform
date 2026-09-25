//go:build integration

package geoserver_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"geoform/internal/infrastructure/geoserver"
)

func setupGeoServer(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "docker.osgeo.org/geoserver:2.28.0",
		ExposedPorts: []string{"8080/tcp"},
		Env: map[string]string{
			"GEOSERVER_ADMIN_USER":     "admin",
			"GEOSERVER_ADMIN_PASSWORD": "geoserver",
			"SKIP_DEMO_DATA":           "true",
		},
		WaitingFor: wait.ForHTTP("/geoserver/web/").
			WithPort("8080/tcp").
			WithStartupTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start geoserver container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8080")
	if err != nil {
		t.Fatalf("failed to get mapped port: %v", err)
	}

	baseURL := fmt.Sprintf("http://%s:%s/geoserver/rest", host, port.Port())

	cleanup := func() {
		_ = container.Terminate(ctx)
	}

	return baseURL, cleanup
}

func TestClient_Integration_WorkspaceLifecycle(t *testing.T) {
	baseURL, cleanup := setupGeoServer(t)
	defer cleanup()

	client := geoserver.NewClient(baseURL, "admin", "geoserver")
	ctx := context.Background()

	t.Run("create workspace", func(t *testing.T) {
		err := client.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace failed: %v", err)
		}
	})

	t.Run("read workspace", func(t *testing.T) {
		ws, err := client.GetWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("GetWorkspace failed: %v", err)
		}
		if ws.Name != "acme" {
			t.Errorf("expected name 'acme', got %q", ws.Name)
		}
	})

	t.Run("delete workspace", func(t *testing.T) {
		err := client.DeleteWorkspace(ctx, "acme", true)
		if err != nil {
			t.Fatalf("DeleteWorkspace failed: %v", err)
		}

		_, err = client.GetWorkspace(ctx, "acme")
		if !geoserver.IsNotFound(err) {
			t.Fatalf("expected workspace to be gone, got: %v", err)
		}
	})
}