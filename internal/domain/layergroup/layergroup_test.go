package layergroup_test

import (
	"testing"

	"github.com/neocaos/geoform/internal/domain/layergroup"
)

func TestValidate(t *testing.T) {
	ok := layergroup.Group{Workspace: "w", Name: "g", Layers: []string{"a", "b"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if ok.EffectiveMode() != layergroup.ModeSingle {
		t.Errorf("modo por defecto = %s", ok.EffectiveMode())
	}
	for name, g := range map[string]layergroup.Group{
		"sin workspace": {Name: "g", Layers: []string{"a"}},
		"sin nombre":    {Workspace: "w", Layers: []string{"a"}},
		"sin capas":     {Workspace: "w", Name: "g"},
		"modo inválido": {Workspace: "w", Name: "g", Mode: "X", Layers: []string{"a"}},
		"capa repetida": {Workspace: "w", Name: "g", Layers: []string{"a", "a"}},
		"capa vacía":    {Workspace: "w", Name: "g", Layers: []string{""}},
	} {
		if g.Validate() == nil {
			t.Errorf("%s: esperaba error", name)
		}
	}
}
