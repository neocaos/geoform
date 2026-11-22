package style_test

import (
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/domain/style"
)

const sld = `<StyledLayerDescriptor version="1.0.0"/>`

func TestValidate(t *testing.T) {
	if err := (style.Style{Workspace: "w", Name: "s", SLD: sld}).Validate(); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]style.Style{
		"sin workspace": {Name: "s", SLD: sld},
		"sin nombre":    {Workspace: "w", SLD: sld},
		"sld vacío":     {Workspace: "w", Name: "s", SLD: "  \n"},
		"no es sld":     {Workspace: "w", Name: "s", SLD: "<html/>"},
	} {
		if s.Validate() == nil {
			t.Errorf("%s: esperaba error", name)
		}
	}
}

func TestSameSLD(t *testing.T) {
	if !style.SameSLD(sld+"\n", "\r\n"+sld) {
		t.Error("los espacios en los extremos no deben contar")
	}
	// Reserialización de GeoServer: prefijos, nombres reescritos, schemaLocation.
	uploaded := `<?xml version="1.0"?><StyledLayerDescriptor version="1.0.0" xmlns="http://www.opengis.net/sld"
	  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="a b">
	  <!-- c --><NamedLayer><Name>mio</Name><UserStyle><Title>T</Title><FeatureTypeStyle><Rule>
	  <Name>r</Name><PointSymbolizer><Size>8</Size></PointSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`
	stored := `<?xml version="1.0" encoding="UTF-8"?><sld:StyledLayerDescriptor xmlns:sld="http://www.opengis.net/sld" version="1.0.0">
	  <sld:NamedLayer><sld:Name>Default Styler</sld:Name><sld:UserStyle><sld:Name>Default Styler</sld:Name><sld:Title>T</sld:Title>
	  <sld:FeatureTypeStyle><sld:Name>name</sld:Name><sld:Rule><sld:Name>r</sld:Name><sld:PointSymbolizer>
	  <sld:Size>8</sld:Size></sld:PointSymbolizer></sld:Rule></sld:FeatureTypeStyle></sld:UserStyle></sld:NamedLayer></sld:StyledLayerDescriptor>`
	if !style.SameSLD(uploaded, stored) {
		t.Error("la reserialización de GeoServer no debe contar como cambio")
	}
	if style.SameSLD(uploaded, strings.Replace(stored, "<sld:Size>8", "<sld:Size>9", 1)) {
		t.Error("un cambio de contenido debe detectarse")
	}
	if style.SameSLD(uploaded, strings.Replace(stored, "<sld:Name>r<", "<sld:Name>q<", 1)) {
		t.Error("el nombre de una regla es contenido")
	}
	if style.SameSLD("<a", "<b") {
		t.Error("texto distinto no válido")
	}
}
