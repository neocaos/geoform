package application_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
	"github.com/neocaos/geoform/internal/infrastructure/memory"
)

func newReconciler(s *memory.Store) *application.Reconciler {
	return application.NewReconciler(
		workspace.NewService(s.Workspace()),
		datastore.NewService(s.Datastore()),
		layer.NewService(s.Layer()),
	)
}

func desired() application.Desired {
	return application.Desired{
		Workspaces: []workspace.Workspace{{Name: "demo"}},
		Datastores: []datastore.Datastore{{
			Workspace: "demo", Name: "pg",
			PostGIS: datastore.PostGIS{Host: "db", Database: "geo", User: "u", Password: "p"},
		}},
		Layers: []layer.Layer{{Workspace: "demo", Store: "pg", Name: "cities", Title: "Ciudades", SRS: "EPSG:4326"}},
	}
}

func mustPlan(t *testing.T, r *application.Reconciler, d application.Desired, prune bool) *application.Plan {
	t.Helper()
	p, err := r.Plan(context.Background(), d, application.PlanOptions{Prune: prune})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return p
}

func TestPlan_EmptyServerCreatesEverythingInOrder(t *testing.T) {
	s := memory.New()
	p := mustPlan(t, newReconciler(s), desired(), false)

	var got []string
	for _, c := range p.Changes {
		got = append(got, string(c.Action)+" "+string(c.Kind)+" "+c.Address)
	}
	want := []string{"create workspace demo", "create datastore demo/pg", "create layer demo/pg/cities"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(s.Writes) != 0 {
		t.Fatalf("Plan no debe escribir, escribió %v", s.Writes)
	}
}

func TestApply_ThenReapplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	r := newReconciler(s)

	if err := r.Apply(ctx, mustPlan(t, r, desired(), false), nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Workspaces) != 1 || len(s.Datastores) != 1 || len(s.Layers) != 1 {
		t.Fatalf("estado inesperado tras apply: %+v", s)
	}
	n := len(s.Writes)

	p := mustPlan(t, r, desired(), true)
	if p.HasChanges() {
		t.Fatalf("el segundo plan debería estar vacío, hay %+v", p.Changes)
	}
	if err := r.Apply(ctx, p, nil); err != nil || len(s.Writes) != n {
		t.Fatalf("el segundo apply no debe escribir: err=%v writes=%v", err, s.Writes[n:])
	}
}

func TestPlan_DetectsUpdates(t *testing.T) {
	s := memory.New()
	r := newReconciler(s)
	if err := r.Apply(context.Background(), mustPlan(t, r, desired(), false), nil); err != nil {
		t.Fatal(err)
	}

	d := desired()
	d.Workspaces[0].Isolated = true
	d.Datastores[0].PostGIS.Host = "otro"
	d.Datastores[0].PostGIS.Port = 6543
	d.Layers[0].Title = "Nuevo"
	p := mustPlan(t, r, d, false)

	if len(p.Changes) != 3 || p.Count(application.Update) != 3 {
		t.Fatalf("se esperaban 3 updates, got %+v", p.Changes)
	}
	ds := p.Changes[1]
	if !reflect.DeepEqual(ds.Details, []string{"host: db -> otro", "port: 5432 -> 6543"}) {
		t.Errorf("detalles de datastore: %v", ds.Details)
	}
	if err := r.Apply(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if !s.Workspaces["demo"].Isolated || s.Datastores["demo/pg"].PostGIS.Host != "otro" || s.Layers["demo/pg/cities"].Title != "Nuevo" {
		t.Errorf("los updates no se aplicaron: %+v", s)
	}
	if again := mustPlan(t, r, d, false); again.HasChanges() {
		t.Errorf("tras el update no debería haber diff: %+v", again.Changes)
	}
}

func TestPlan_DefaultsDoNotCauseDiff(t *testing.T) {
	// El real tiene port/schema explícitos; el deseado los omite.
	s := memory.New()
	s.Workspaces["demo"] = workspace.Workspace{Name: "demo"}
	s.Datastores["demo/pg"] = datastore.Datastore{Workspace: "demo", Name: "pg",
		PostGIS: datastore.PostGIS{Host: "db", Port: 5432, Database: "geo", Schema: "public", User: "u"}}
	s.Layers["demo/pg/cities"] = layer.Layer{Workspace: "demo", Store: "pg", Name: "cities", Table: "cities", Title: "Ciudades", SRS: "EPSG:4326"}
	if p := mustPlan(t, newReconciler(s), desired(), false); p.HasChanges() {
		t.Fatalf("no debería haber cambios: %+v", p.Changes)
	}
}

func TestPrune(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	r := newReconciler(s)
	if err := r.Apply(ctx, mustPlan(t, r, desired(), false), nil); err != nil {
		t.Fatal(err)
	}
	// Sobrantes: una capa y un datastore (con capa) dentro del workspace gestionado,
	// y un workspace ajeno que nunca debe tocarse.
	s.Layers["demo/pg/old"] = layer.Layer{Workspace: "demo", Store: "pg", Name: "old", SRS: "EPSG:4326"}
	s.Datastores["demo/extra"] = datastore.Datastore{Workspace: "demo", Name: "extra"}
	s.Layers["demo/extra/l"] = layer.Layer{Workspace: "demo", Store: "extra", Name: "l", SRS: "EPSG:4326"}
	s.Workspaces["ajeno"] = workspace.Workspace{Name: "ajeno"}
	s.Datastores["ajeno/x"] = datastore.Datastore{Workspace: "ajeno", Name: "x"}

	if p := mustPlan(t, r, desired(), false); p.HasChanges() {
		t.Fatalf("sin --prune no se borra nada: %+v", p.Changes)
	}

	p := mustPlan(t, r, desired(), true)
	var got []string
	for _, c := range p.Changes {
		got = append(got, string(c.Action)+" "+string(c.Kind)+" "+c.Address)
	}
	want := []string{"delete layer demo/pg/old", "delete datastore demo/extra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := r.Apply(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Layers["demo/pg/old"]; ok {
		t.Error("la capa sobrante sigue existiendo")
	}
	if _, ok := s.Datastores["demo/extra"]; ok {
		t.Error("el datastore sobrante sigue existiendo")
	}
	if _, ok := s.Datastores["ajeno/x"]; !ok {
		t.Error("se borró un recurso de un workspace no declarado")
	}
	if _, ok := s.Workspaces["ajeno"]; !ok {
		t.Error("se borró un workspace no declarado")
	}
}

func TestApply_StopsOnFirstErrorAndReportsProgress(t *testing.T) {
	s := memory.New()
	s.FailOn = "create datastore demo/pg"
	r := newReconciler(s)

	var done []string
	err := r.Apply(context.Background(), mustPlan(t, r, desired(), false), func(c application.Change) { done = append(done, c.Address) })
	if err == nil || !strings.Contains(err.Error(), "create datastore demo/pg") {
		t.Fatalf("error inesperado: %v", err)
	}
	if !reflect.DeepEqual(done, []string{"demo"}) {
		t.Errorf("progreso: %v", done)
	}
	if len(s.Layers) != 0 {
		t.Error("no debe aplicarse nada tras el error")
	}
}

func TestPlan_InvalidDesired(t *testing.T) {
	d := desired()
	d.Datastores = append(d.Datastores, d.Datastores[0])                                                    // duplicado
	d.Layers = append(d.Layers, layer.Layer{Workspace: "demo", Store: "nope", Name: "x", SRS: "EPSG:4326"}) // padre inexistente
	_, err := newReconciler(memory.New()).Plan(context.Background(), d, application.PlanOptions{})
	if err == nil || !strings.Contains(err.Error(), "duplicado") || !strings.Contains(err.Error(), "no está declarado") {
		t.Fatalf("se esperaban ambos errores, got %v", err)
	}
}

func TestPlan_EmptyTitleIsUnmanaged(t *testing.T) {
	s := memory.New()
	s.Workspaces["demo"] = workspace.Workspace{Name: "demo"}
	s.Datastores["demo/pg"] = datastore.Datastore{Workspace: "demo", Name: "pg",
		PostGIS: datastore.PostGIS{Host: "db", Database: "geo", User: "u"}}
	s.Layers["demo/pg/cities"] = layer.Layer{Workspace: "demo", Store: "pg", Name: "cities", Title: "cities", SRS: "EPSG:4326"}
	d := desired()
	d.Layers[0].Title = ""
	if p := mustPlan(t, newReconciler(s), d, false); p.HasChanges() {
		t.Fatalf("no debería haber cambios: %+v", p.Changes)
	}
}
