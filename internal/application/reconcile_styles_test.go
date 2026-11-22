package application_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/infrastructure/memory"
)

const sldA = `<StyledLayerDescriptor version="1.0.0"><A/></StyledLayerDescriptor>`
const sldB = `<StyledLayerDescriptor version="1.0.0"><B/></StyledLayerDescriptor>`

func newStyledReconciler(s *memory.Store) *application.Reconciler {
	return newReconciler(s).
		WithStyles(style.NewService(s.Style())).
		WithLayerGroups(layergroup.NewService(s.LayerGroup()))
}

func styledDesired() application.Desired {
	d := desired()
	d.Styles = []style.Style{{Workspace: "demo", Name: "rojo", SLD: sldA}}
	d.Layers[0].Style = "rojo" // sin prefijo: resuelve al estilo del workspace
	d.LayerGroups = []layergroup.Group{{Workspace: "demo", Name: "base", Layers: []string{"cities"}}}
	return d
}

func changes(p *application.Plan) []string {
	var got []string
	for _, c := range p.Changes {
		got = append(got, string(c.Action)+" "+string(c.Kind)+" "+c.Address)
	}
	return got
}

func TestPlan_StylesAndGroupsOrder(t *testing.T) {
	s := memory.New()
	p := mustPlan(t, newStyledReconciler(s), styledDesired(), false)
	want := []string{
		"create workspace demo", "create datastore demo/pg", "create style demo/rojo",
		"create layer demo/pg/cities", "create layergroup demo/base",
	}
	if got := changes(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := p.Changes[3].Layer.Style; got != "demo:rojo" {
		t.Errorf("estilo de la capa = %q, want demo:rojo", got)
	}
}

func TestApply_StylesAndGroupsIdempotent(t *testing.T) {
	s := memory.New()
	r := newStyledReconciler(s)
	d := styledDesired()
	if err := r.Apply(context.Background(), mustPlan(t, r, d, false), nil); err != nil {
		t.Fatal(err)
	}
	if s.Styles["demo/rojo"].SLD != sldA || s.Layers["demo/pg/cities"].Style != "demo:rojo" || len(s.LayerGroups) != 1 {
		t.Fatalf("estado: %+v %+v %+v", s.Styles, s.Layers, s.LayerGroups)
	}
	if p := mustPlan(t, r, d, true); p.HasChanges() {
		t.Fatalf("segundo plan debería estar vacío: %v", changes(p))
	}
}

func TestPlan_StyleAndGroupUpdates(t *testing.T) {
	s := memory.New()
	r := newStyledReconciler(s)
	d := styledDesired()
	if err := r.Apply(context.Background(), mustPlan(t, r, d, false), nil); err != nil {
		t.Fatal(err)
	}
	d.Styles[0].SLD = sldB
	d.Layers[0].Style = "azul"
	d.Styles = append(d.Styles, style.Style{Workspace: "demo", Name: "azul", SLD: sldB})
	d.LayerGroups[0].Title = "Base"
	d.LayerGroups[0].Mode = layergroup.ModeNamed
	p := mustPlan(t, r, d, false)
	want := []string{"update style demo/rojo", "create style demo/azul", "update layer demo/pg/cities", "update layergroup demo/base"}
	if got := changes(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !strings.Contains(strings.Join(p.Changes[2].Details, ";"), `style: "demo:rojo" -> "demo:azul"`) {
		t.Errorf("detalles de la capa: %v", p.Changes[2].Details)
	}
	if err := r.Apply(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if s.Layers["demo/pg/cities"].Style != "demo:azul" || s.LayerGroups["demo/base"].Mode != layergroup.ModeNamed {
		t.Fatalf("estado: %+v %+v", s.Layers, s.LayerGroups)
	}
}

func TestPlan_GlobalStyleNotValidated(t *testing.T) {
	d := desired()
	d.Layers[0].Style = "polygon"
	p := mustPlan(t, newReconciler(memory.New()), d, false)
	if p.Changes[2].Layer.Style != "polygon" {
		t.Errorf("estilo = %q", p.Changes[2].Layer.Style)
	}
}

func TestPrune_StylesAndGroupsDeletedInOrder(t *testing.T) {
	s := memory.New()
	r := newStyledReconciler(s)
	if err := r.Apply(context.Background(), mustPlan(t, r, styledDesired(), false), nil); err != nil {
		t.Fatal(err)
	}
	s.Styles["demo/viejo"] = style.Style{Workspace: "demo", Name: "viejo", SLD: sldA}
	s.LayerGroups["demo/viejo"] = layergroup.Group{Workspace: "demo", Name: "viejo", Layers: []string{"cities"}}
	s.Layers["demo/pg/old"] = layer.Layer{Workspace: "demo", Store: "pg", Name: "old", SRS: "EPSG:4326"}

	if p := mustPlan(t, r, styledDesired(), false); p.HasChanges() {
		t.Fatalf("sin prune no se borra nada: %v", changes(p))
	}
	p := mustPlan(t, r, styledDesired(), true)
	want := []string{"delete layergroup demo/viejo", "delete layer demo/pg/old", "delete style demo/viejo"}
	if got := changes(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := r.Apply(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Styles["demo/viejo"]; ok || len(s.Styles) != 1 || len(s.LayerGroups) != 1 {
		t.Fatalf("estado: %+v %+v", s.Styles, s.LayerGroups)
	}
}

func TestPlan_StyleValidation(t *testing.T) {
	d := styledDesired()
	d.Layers[0].Style = "demo:noexiste"
	d.LayerGroups = append(d.LayerGroups, layergroup.Group{Workspace: "demo", Name: "mal", Layers: []string{"fantasma"}})
	d.Styles = append(d.Styles, style.Style{Workspace: "otro", Name: "x", SLD: "<html/>"})
	_, err := newStyledReconciler(memory.New()).Plan(context.Background(), d, application.PlanOptions{})
	if err == nil {
		t.Fatal("esperaba error")
	}
	for _, frag := range []string{`el estilo "demo:noexiste" no está declarado`, `la capa "fantasma" no está declarada`, `workspace "otro" no está declarado`, "no parece un SLD"} {
		if !strings.Contains(err.Error(), frag) {
			t.Errorf("falta %q en %v", frag, err)
		}
	}
}

func TestPlan_StylesWithoutServiceFails(t *testing.T) {
	_, err := newReconciler(memory.New()).Plan(context.Background(), styledDesired(), application.PlanOptions{})
	if err == nil || !strings.Contains(err.Error(), "estilos") {
		t.Fatalf("err = %v", err)
	}
}
