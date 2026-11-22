package importer_test

import (
	"context"
	"strings"
	"testing"

	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/importer"
	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

const importedSLD = `<sld:StyledLayerDescriptor xmlns:sld="http://www.opengis.net/sld" version="1.0.0"/>`

// styledSource añade estilos y grupos a fakeSource (workspace "demo").
type styledSource struct{ fakeSource }

func (styledSource) ListStyles(_ context.Context, ws string) ([]string, error) {
	if ws == "demo" {
		return []string{"rojo", "cities", "bad name", "css"}, nil
	}
	return nil, nil
}
func (styledSource) GetStyleSLD(_ context.Context, _, n string) (string, error) {
	if n == "css" {
		return "", &gs.APIError{StatusCode: 400}
	}
	return importedSLD, nil
}
func (styledSource) ListLayerGroups(_ context.Context, ws string) ([]string, error) {
	if ws == "demo" {
		return []string{"base", "mixto"}, nil
	}
	return nil, nil
}
func (styledSource) GetLayerGroup(_ context.Context, _, n string) (*gs.LayerGroup, error) {
	if n == "mixto" { // contiene una capa de otro workspace
		return &gs.LayerGroup{Name: n, Mode: "SINGLE", Layers: []string{"demo:cities", "otro:x"}}, nil
	}
	return &gs.LayerGroup{Name: n, Title: "Base", Mode: "NAMED", Layers: []string{"demo:alias", "demo:cities"}}, nil
}
func (styledSource) GetLayerDefaultStyle(_ context.Context, _, l string) (*gs.StyleRef, error) {
	switch l {
	case "cities":
		return &gs.StyleRef{Name: "rojo", Workspace: "demo"}, nil
	case "alias":
		return &gs.StyleRef{Name: "polygon"}, nil
	}
	return nil, nil
}

func TestImport_StylesAndGroups(t *testing.T) {
	res, err := importer.Import(context.Background(), styledSource{}, []string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Desired
	if len(d.Styles) != 2 || d.Styles[0].Name != "cities" || d.Styles[1].Name != "rojo" {
		t.Fatalf("styles: %+v", d.Styles)
	}
	styles := map[string]string{}
	for _, l := range d.Layers {
		styles[l.Name] = l.Style
	}
	if styles["cities"] != "rojo" || styles["alias"] != "polygon" || styles["odd"] != "" {
		t.Errorf("estilos de capa: %v", styles)
	}
	if len(d.LayerGroups) != 1 || d.LayerGroups[0].Name != "base" || d.LayerGroups[0].Mode != "NAMED" ||
		strings.Join(d.LayerGroups[0].Layers, ",") != "alias,cities" {
		t.Errorf("groups: %+v", d.LayerGroups)
	}
	warns := strings.Join(res.Warnings, "\n")
	for _, want := range []string{"bad name", "css", "mixto"} {
		if !strings.Contains(warns, want) {
			t.Errorf("falta aviso sobre %q en:\n%s", want, warns)
		}
	}

	// El YAML generado se vuelve a leer sin errores y con los mismos recursos.
	data, err := storage.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := storage.Parse(strings.NewReader(string(data)), func(string) string { return "x" })
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if len(back.Styles) != 2 || len(back.LayerGroups) != 1 || back.Layers[0].Style == "" {
		t.Errorf("round trip: %+v\n%s", back, data)
	}
}

func TestMarshal_OmitsEmptyStyleKeys(t *testing.T) {
	res, err := importer.Import(context.Background(), fakeSource{}, []string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := storage.Marshal(res.Desired)
	for _, k := range []string{"styles", "layergroups", "style:"} {
		if strings.Contains(string(data), k) {
			t.Errorf("el YAML no debería contener %q:\n%s", k, data)
		}
	}
}
