package application_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/coverage"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
	"github.com/neocaos/geoform/internal/infrastructure/memory"
)

func newRasterReconciler(s *memory.Store) *application.Reconciler {
	return newReconciler(s).WithCoverage(coverage.NewService(s.Coverage()))
}

func storesDesired() application.Desired {
	return application.Desired{
		Workspaces: []workspace.Workspace{{Name: "demo"}},
		Datastores: []datastore.Datastore{
			{Workspace: "demo", Name: "gpkg", Type: datastore.TypeGeoPackage, File: "file:/d/a.gpkg"},
			{Workspace: "demo", Name: "shp", Type: datastore.TypeShapefile, File: "file:/d/a.shp"},
		},
		Layers: []layer.Layer{{Workspace: "demo", Store: "gpkg", Name: "places", SRS: "EPSG:4326"}},
		CoverageStores: []coverage.Store{
			{Workspace: "demo", Name: "dem", Type: coverage.TypeGeoTIFF, URL: "file:/d/dem.tif"},
		},
		Coverages: []coverage.Coverage{{Workspace: "demo", Store: "dem", Name: "dem", Title: "DEM", SRS: "EPSG:4326"}},
	}
}

func actions(p *application.Plan) []string {
	var got []string
	for _, c := range p.Changes {
		got = append(got, string(c.Action)+" "+string(c.Kind)+" "+c.Address)
	}
	return got
}

func TestStores_PlanApplyIdempotent(t *testing.T) {
	s := memory.New()
	r := newRasterReconciler(s)
	p := mustPlan(t, r, storesDesired(), false)
	want := []string{
		"create workspace demo", "create datastore demo/gpkg", "create datastore demo/shp",
		"create layer demo/gpkg/places", "create coveragestore demo/dem", "create coverage demo/dem/dem",
	}
	if got := actions(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := r.Apply(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Datastores["demo/shp"]; got.Type != datastore.TypeShapefile || got.File != "file:/d/a.shp" {
		t.Errorf("shapefile guardado: %+v", got)
	}
	if p2 := mustPlan(t, r, storesDesired(), true); p2.HasChanges() {
		t.Fatalf("re-plan debía estar vacío: %v", actions(p2))
	}
}

func TestStores_UpdateDetails(t *testing.T) {
	s := memory.New()
	r := newRasterReconciler(s)
	if err := r.Apply(context.Background(), mustPlan(t, r, storesDesired(), false), nil); err != nil {
		t.Fatal(err)
	}
	d := storesDesired()
	d.Datastores[0].File = "file:/d/b.gpkg"
	d.CoverageStores[0].URL = "file:/d/other.tif"
	d.Coverages[0].Title = "Relieve"
	p := mustPlan(t, r, d, false)
	want := []string{"update datastore demo/gpkg", "update coveragestore demo/dem", "update coverage demo/dem/dem"}
	if got := actions(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !strings.Contains(p.Changes[0].Details[0], "file: file:/d/a.gpkg -> file:/d/b.gpkg") {
		t.Errorf("detalle: %v", p.Changes[0].Details)
	}
}

func TestStores_TypeChangeIsUpdate(t *testing.T) {
	s := memory.New()
	r := newRasterReconciler(s)
	d := storesDesired()
	if err := r.Apply(context.Background(), mustPlan(t, r, d, false), nil); err != nil {
		t.Fatal(err)
	}
	d.Datastores[1] = datastore.Datastore{Workspace: "demo", Name: "shp", PostGIS: datastore.PostGIS{Host: "db", Database: "g", User: "u"}}
	p := mustPlan(t, r, d, false)
	if len(p.Changes) != 1 || !strings.Contains(strings.Join(p.Changes[0].Details, ";"), "type: shapefile -> postgis") {
		t.Fatalf("got %v %v", actions(p), p.Changes[0].Details)
	}
}

func TestStores_PruneOrder(t *testing.T) {
	s := memory.New()
	r := newRasterReconciler(s)
	if err := r.Apply(context.Background(), mustPlan(t, r, storesDesired(), false), nil); err != nil {
		t.Fatal(err)
	}
	// Sobrantes: una coverage en un almacén declarado y un almacén entero.
	s.Coverages["demo/dem/extra"] = coverage.Coverage{Workspace: "demo", Store: "dem", Name: "extra", SRS: "EPSG:4326"}
	s.CoverageStores["demo/old"] = coverage.Store{Workspace: "demo", Name: "old", URL: "file:/d/old.tif"}
	s.Coverages["demo/old/old"] = coverage.Coverage{Workspace: "demo", Store: "old", Name: "old", SRS: "EPSG:4326"}

	if p := mustPlan(t, r, storesDesired(), false); p.HasChanges() {
		t.Fatalf("sin prune no se borra nada: %v", actions(p))
	}
	p := mustPlan(t, r, storesDesired(), true)
	want := []string{"delete coverage demo/dem/extra", "delete coveragestore demo/old"}
	if got := actions(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := r.Apply(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.CoverageStores["demo/old"]; ok {
		t.Error("old debía borrarse")
	}
	if _, ok := s.Coverages["demo/dem/dem"]; !ok {
		t.Error("dem no debía borrarse")
	}
}

func TestStores_Validation(t *testing.T) {
	r := newRasterReconciler(memory.New())
	d := storesDesired()
	d.Coverages = append(d.Coverages, coverage.Coverage{Workspace: "demo", Store: "dem", Name: "places", SRS: "EPSG:4326"},
		coverage.Coverage{Workspace: "demo", Store: "nope", Name: "x", SRS: "EPSG:4326"})
	_, err := r.Plan(context.Background(), d, application.PlanOptions{})
	if err == nil || !strings.Contains(err.Error(), "ya existe una capa vectorial") || !strings.Contains(err.Error(), `coveragestore "demo/nope" no está declarado`) {
		t.Fatalf("err = %v", err)
	}
}

func TestStores_RasterRequiresService(t *testing.T) {
	_, err := newReconciler(memory.New()).Plan(context.Background(), storesDesired(), application.PlanOptions{})
	if err == nil || !strings.Contains(err.Error(), "WithCoverage") {
		t.Fatalf("err = %v", err)
	}
}
