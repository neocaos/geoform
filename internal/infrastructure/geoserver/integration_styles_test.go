//go:build integration

package geoserver_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/domain/workspace"
	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/geoserver"
)

func readSLD(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../../test/testdata/sld/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIntegration_StylesAndLayerGroups(t *testing.T) {
	ctx := context.Background()
	client := gs.NewClient(
		env("GEOSERVER_URL", "http://localhost:18080/geoserver/rest"),
		env("GEOSERVER_USER", "admin"),
		env("GEOSERVER_PASS", "geoserver"),
	)
	const ws = "geoform_it_styles"
	cleanup := func() { _ = client.DeleteWorkspace(ctx, ws, true) }
	cleanup()
	t.Cleanup(cleanup)

	wsSvc := workspace.NewService(geoserver.NewWorkspaceRepository(client))
	dsSvc := datastore.NewService(geoserver.NewDatastoreRepository(client))
	lySvc := layer.NewService(geoserver.NewLayerRepository(client))
	stSvc := style.NewService(geoserver.NewStyleRepository(client))
	lgSvc := layergroup.NewService(geoserver.NewLayerGroupRepository(client))

	if _, err := wsSvc.Ensure(ctx, workspace.Workspace{Name: ws}); err != nil {
		t.Fatal(err)
	}
	if _, err := dsSvc.Ensure(ctx, datastore.Datastore{Workspace: ws, Name: "postgis",
		PostGIS: datastore.PostGIS{Host: "db", Port: 5432, Database: "geoform", Schema: "geodata", User: "geouser", Password: "geopassword"}}); err != nil {
		t.Fatal(err)
	}

	// Estilos: crear, leer, actualizar, listar.
	red, blue := readSLD(t, "point_red.sld"), readSLD(t, "point_blue.sld")
	for i := 0; i < 2; i++ { // Ensure es idempotente
		if _, err := stSvc.Ensure(ctx, style.Style{Workspace: ws, Name: "rojo", SLD: red}); err != nil {
			t.Fatalf("Ensure style #%d: %v", i, err)
		}
	}
	got, err := stSvc.Get(ctx, ws, "rojo")
	if err != nil || !style.SameSLD(got.SLD, red) {
		t.Fatalf("SLD leído distinto del subido: %v\n%s", err, got)
	}
	if err := stSvc.Update(ctx, style.Style{Workspace: ws, Name: "rojo", SLD: blue}); err != nil {
		t.Fatal(err)
	}
	if got, _ := stSvc.Get(ctx, ws, "rojo"); got == nil || !style.SameSLD(got.SLD, blue) {
		t.Fatalf("SLD sin actualizar: %+v", got)
	}
	if _, err := stSvc.Ensure(ctx, style.Style{Workspace: ws, Name: "azul", SLD: blue}); err != nil {
		t.Fatal(err)
	}
	if names, _ := stSvc.List(ctx, ws); !reflect.DeepEqual(sorted(names), []string{"azul", "rojo"}) {
		t.Fatalf("List = %v", names)
	}

	// Capas con estilo por defecto (en Create y en Update).
	for _, table := range []string{"cities", "regions"} {
		l := layer.Layer{Workspace: ws, Store: "postgis", Name: table, SRS: "EPSG:4326", Style: ws + ":azul"}
		if _, err := lySvc.Ensure(ctx, l); err != nil {
			t.Fatalf("Ensure layer %s: %v", table, err)
		}
	}
	if l, _ := lySvc.Get(ctx, ws, "postgis", "cities"); l == nil || l.Style != ws+":azul" {
		t.Fatalf("estilo por defecto: %+v", l)
	}
	if err := lySvc.Update(ctx, layer.Layer{Workspace: ws, Store: "postgis", Name: "cities", SRS: "EPSG:4326", Style: ws + ":rojo"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := lySvc.Get(ctx, ws, "postgis", "cities"); l == nil || l.Style != ws+":rojo" {
		t.Fatalf("estilo tras Update: %+v", l)
	}
	// Un estilo global también se puede asignar.
	if err := lySvc.Update(ctx, layer.Layer{Workspace: ws, Store: "postgis", Name: "regions", SRS: "EPSG:4326", Style: "polygon"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := lySvc.Get(ctx, ws, "postgis", "regions"); l == nil || l.Style != "polygon" {
		t.Fatalf("estilo global: %+v", l)
	}

	// Grupos de capas.
	g := layergroup.Group{Workspace: ws, Name: "base", Title: "Base", Layers: []string{"regions", "cities"}}
	for i := 0; i < 2; i++ {
		if _, err := lgSvc.Ensure(ctx, g); err != nil {
			t.Fatalf("Ensure group #%d: %v", i, err)
		}
	}
	gotG, err := lgSvc.Get(ctx, ws, "base")
	if err != nil || gotG.Mode != layergroup.ModeSingle || gotG.Title != "Base" || !reflect.DeepEqual(gotG.Layers, g.Layers) {
		t.Fatalf("grupo: %+v %v", gotG, err)
	}
	g.Layers, g.Mode = []string{"cities"}, layergroup.ModeNamed
	if err := lgSvc.Update(ctx, g); err != nil {
		t.Fatal(err)
	}
	if gotG, err := lgSvc.Get(ctx, ws, "base"); err != nil || gotG.Mode != layergroup.ModeNamed || !reflect.DeepEqual(gotG.Layers, []string{"cities"}) {
		t.Fatalf("grupo tras Update: %+v %v", gotG, err)
	}
	if names, _ := lgSvc.List(ctx, ws); !reflect.DeepEqual(names, []string{"base"}) {
		t.Fatalf("List groups = %v", names)
	}

	// Borrado: grupo, y estilo en uso (recurse retira la referencia).
	if err := lgSvc.Remove(ctx, ws, "base"); err != nil {
		t.Fatal(err)
	}
	if _, err := lgSvc.Get(ctx, ws, "base"); err != layergroup.ErrNotFound {
		t.Fatalf("grupo debería no existir: %v", err)
	}
	if err := stSvc.Remove(ctx, ws, "rojo"); err != nil {
		t.Fatalf("Remove style en uso: %v", err)
	}
	if _, err := stSvc.Get(ctx, ws, "rojo"); err != style.ErrNotFound {
		t.Fatalf("estilo debería no existir: %v", err)
	}
	if err := stSvc.Remove(ctx, ws, "rojo"); !errors.Is(err, style.ErrNotFound) {
		t.Fatalf("segundo Remove debe dar ErrNotFound: %v", err)
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
