//go:build integration

package geoserver_test

import (
	"context"
	"os"
	"testing"

	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/geoserver"
)

// Requiere el stack de docker-compose.test.yml levantado (make test-integration).

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestIntegration_PostGISPublishLifecycle(t *testing.T) {
	ctx := context.Background()
	client := gs.NewClient(
		env("GEOSERVER_URL", "http://localhost:18080/geoserver/rest"),
		env("GEOSERVER_USER", "admin"),
		env("GEOSERVER_PASS", "geoserver"),
	)

	const ws = "geoform_it"
	// Limpieza previa (por si una ejecución anterior quedó a medias) y final.
	cleanup := func() { _ = client.DeleteWorkspace(ctx, ws, true) }
	cleanup()
	t.Cleanup(cleanup)

	wsSvc := workspace.NewService(geoserver.NewWorkspaceRepository(client))
	dsSvc := datastore.NewService(geoserver.NewDatastoreRepository(client))
	lySvc := layer.NewService(geoserver.NewLayerRepository(client))

	// Workspace (Ensure idempotente: dos veces)
	for i := 0; i < 2; i++ {
		got, err := wsSvc.Ensure(ctx, workspace.Workspace{Name: ws})
		if err != nil || got.Name != ws {
			t.Fatalf("Ensure workspace #%d: %v (%v)", i, got, err)
		}
	}

	// Datastore PostGIS: el host es el nombre del servicio en la red de compose.
	dsIn := datastore.Datastore{
		Workspace: ws, Name: "postgis",
		PostGIS: datastore.PostGIS{Host: "db", Port: 5432, Database: "geoform", Schema: "geodata", User: "geouser", Password: "geopassword"},
	}
	for i := 0; i < 2; i++ {
		got, err := dsSvc.Ensure(ctx, dsIn)
		if err != nil {
			t.Fatalf("Ensure datastore #%d: %v", i, err)
		}
		if got.PostGIS.Host != "db" || got.PostGIS.Database != "geoform" || got.PostGIS.Schema != "geodata" {
			t.Fatalf("datastore inesperado: %+v", got)
		}
	}

	// Capas desde las tablas sembradas
	for _, table := range []string{"cities", "roads", "regions"} {
		l := layer.Layer{Workspace: ws, Store: "postgis", Name: table, SRS: "EPSG:4326", Title: "Capa " + table}
		for i := 0; i < 2; i++ {
			got, err := lySvc.Ensure(ctx, l)
			if err != nil {
				t.Fatalf("Ensure layer %s #%d: %v", table, i, err)
			}
			if got.Name != table || got.SRS != "EPSG:4326" || got.Table != table {
				t.Fatalf("layer inesperada: %+v", got)
			}
		}
		ok, err := client.LayerExists(ctx, ws, table)
		if err != nil || !ok {
			t.Fatalf("layer %s no publicada vía /layers: ok=%v err=%v", table, ok, err)
		}
	}

	// Tabla inexistente: debe fallar
	if _, err := lySvc.Ensure(ctx, layer.Layer{Workspace: ws, Store: "postgis", Name: "nope", SRS: "EPSG:4326"}); err == nil {
		t.Fatal("se esperaba error al publicar una tabla inexistente")
	}

	// Remove capa, datastore y workspace
	if err := lySvc.Remove(ctx, ws, "postgis", "cities"); err != nil {
		t.Fatalf("Remove layer: %v", err)
	}
	if _, err := lySvc.Get(ctx, ws, "postgis", "cities"); err != layer.ErrNotFound {
		t.Fatalf("la capa debería no existir, err=%v", err)
	}
	if ok, _ := client.LayerExists(ctx, ws, "cities"); ok {
		t.Fatal("el layer cities sigue existiendo en /layers")
	}
	if err := dsSvc.Remove(ctx, ws, "postgis", true); err != nil {
		t.Fatalf("Remove datastore: %v", err)
	}
	if _, err := dsSvc.Get(ctx, ws, "postgis"); err != datastore.ErrNotFound {
		t.Fatalf("el datastore debería no existir, err=%v", err)
	}
	if err := wsSvc.Remove(ctx, ws, true); err != nil {
		t.Fatalf("Remove workspace: %v", err)
	}
	if _, err := wsSvc.Get(ctx, ws); err != workspace.ErrNotFound {
		t.Fatalf("el workspace debería no existir, err=%v", err)
	}
}
