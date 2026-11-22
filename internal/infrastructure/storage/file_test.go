package storage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

const valid = `
workspaces:
  - name: demo
    isolated: true
    datastores:
      - name: postgis
        host: db
        database: geoform
        schema: geodata
        user: geouser
        password_env: PG_PASSWORD
        layers:
          - name: cities
            title: Ciudades
            srs: EPSG:4326
          - name: carreteras
            table: roads
            srs: EPSG:4326
`

func TestParse_Valid(t *testing.T) {
	d, err := storage.Parse(strings.NewReader(valid), env(map[string]string{"PG_PASSWORD": "s3"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Workspaces) != 1 || !d.Workspaces[0].Isolated {
		t.Errorf("workspaces: %+v", d.Workspaces)
	}
	if len(d.Datastores) != 1 || d.Datastores[0].PostGIS.Password != "s3" || d.Datastores[0].PostGIS.Schema != "geodata" || d.Datastores[0].Workspace != "demo" {
		t.Errorf("datastores: %+v", d.Datastores)
	}
	if len(d.Layers) != 2 || d.Layers[1].Table != "roads" || d.Layers[1].Store != "postgis" || d.Layers[0].NativeTable() != "cities" {
		t.Errorf("layers: %+v", d.Layers)
	}
}

func TestParse_Errors(t *testing.T) {
	cases := map[string]struct {
		yaml string
		env  map[string]string
		want []string // fragmentos que deben aparecer en el error
	}{
		"vacío":             {"", nil, []string{"vacío"}},
		"sin workspaces":    {"workspaces: []", nil, []string{"ningún workspace"}},
		"campo desconocido": {"workspaces:\n  - name: a\n    colour: red\n", nil, []string{"colour"}},
		"yaml roto":         {"workspaces: [\n", nil, []string{"YAML inválido"}},
		"password_env sin definir": {strings.Replace(valid, "PG_PASSWORD", "NOPE", 1), nil,
			[]string{"workspaces[0].datastores[0]", "NOPE"}},
		"password y password_env": {strings.Replace(valid, "password_env: PG_PASSWORD", "password_env: X\n        password: y", 1),
			map[string]string{"X": "1"}, []string{"no ambos"}},
		"varios errores a la vez": {`
workspaces:
  - name: "1mal"
    datastores:
      - name: ds
        port: 70000
        layers:
          - name: l
            srs: 4326
  - name: dup
  - name: dup
`, nil, []string{
			`workspaces[0].name: nombre "1mal" inválido`,
			"workspaces[0].datastores[0].host: es obligatorio",
			"workspaces[0].datastores[0].database: es obligatorio",
			"workspaces[0].datastores[0].user: es obligatorio",
			"workspaces[0].datastores[0].port: puerto 70000",
			`workspaces[0].datastores[0].layers[0].srs: SRS "4326" inválido`,
			`workspaces[2].name: workspace "dup" duplicado`,
		}},
		"srs obligatorio": {strings.Replace(valid, "title: Ciudades\n            srs: EPSG:4326", "title: Ciudades", 1),
			map[string]string{"PG_PASSWORD": "x"}, []string{"layers[0].srs: es obligatorio"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := storage.Parse(strings.NewReader(tc.yaml), env(tc.env))
			if err == nil {
				t.Fatal("se esperaba un error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("el error no contiene %q:\n%v", w, err)
				}
			}
		})
	}
}

func TestLoadFile_Missing(t *testing.T) {
	if _, err := storage.LoadFile("/no/existe.yaml", env(nil)); err == nil {
		t.Fatal("se esperaba un error")
	}
}

const withSecurity = valid + `
security:
  roles: [ROLE_EDITOR]
  users:
    - name: alice
      password_env: ALICE_PW
      roles: [ROLE_EDITOR]
    - name: bob
      password_env: ALICE_PW
      enabled: false
  rules:
    - workspace: demo
      layer: cities
      access: write
      roles: [ROLE_EDITOR]
    - workspace: demo
      access: read
      roles: ["*"]
`

func TestParse_Security(t *testing.T) {
	e := env(map[string]string{"PG_PASSWORD": "x", "ALICE_PW": "pw-alice"})
	d, err := storage.Parse(strings.NewReader(withSecurity), e)
	if err != nil {
		t.Fatal(err)
	}
	s := d.Security
	if s == nil || len(s.Roles) != 1 || len(s.Users) != 2 || len(s.Rules) != 2 {
		t.Fatalf("security: %+v", s)
	}
	if !s.Users[0].Enabled || s.Users[1].Enabled || s.Users[0].Password != "pw-alice" {
		t.Errorf("users: %+v", s.Users)
	}
	if s.Rules[1].Layer != "*" || s.Rules[0].Key() != "demo.cities.w" {
		t.Errorf("rules: %+v", s.Rules)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	// Sin sección security no se gestiona.
	d, err = storage.Parse(strings.NewReader(valid), e)
	if err != nil || d.Security != nil {
		t.Fatalf("security debe ser nil: %+v %v", d.Security, err)
	}
}

func TestParse_SecurityErrors(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"sin password_env":  {valid + "security:\n  users:\n    - name: a\n", "security.users[0].password_env"},
		"password en claro": {valid + "security:\n  users:\n    - name: a\n      password: x\n", "password"},
		"env sin definir":   {valid + "security:\n  users:\n    - name: a\n      password_env: NOPE\n", "NOPE"},
		"acceso":            {valid + "security:\n  rules:\n    - workspace: demo\n      access: admin\n      roles: [R]\n", "security.rules[0].access"},
	}
	for name, c := range cases {
		_, err := storage.Parse(strings.NewReader(c.yaml), env(map[string]string{"PG_PASSWORD": "x"}))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (esperaba %q)", name, err, c.want)
		}
	}
}

func TestParse_StylesAndLayerGroups(t *testing.T) {
	const cfg = `
workspaces:
  - name: demo
    styles:
      - name: rojo
        sld: |
          <StyledLayerDescriptor version="1.0.0"/>
    layergroups:
      - name: base
        title: Base
        mode: NAMED
        layers: [cities]
    datastores:
      - name: pg
        host: db
        database: geoform
        user: u
        layers:
          - name: cities
            srs: EPSG:4326
            style: rojo
`
	d, err := storage.Parse(strings.NewReader(cfg), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Styles) != 1 || d.Styles[0].Workspace != "demo" || !strings.Contains(d.Styles[0].SLD, "StyledLayerDescriptor") {
		t.Errorf("styles: %+v", d.Styles)
	}
	if len(d.LayerGroups) != 1 || d.LayerGroups[0].Mode != "NAMED" || d.LayerGroups[0].Layers[0] != "cities" {
		t.Errorf("layergroups: %+v", d.LayerGroups)
	}
	if d.Layers[0].Style != "rojo" {
		t.Errorf("layer style: %q", d.Layers[0].Style)
	}
}

func TestLoadFile_StyleFileRelativeToConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "styles", "a.sld"), []byte(`<StyledLayerDescriptor/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "workspaces:\n  - name: demo\n    styles:\n      - name: a\n        file: styles/a.sld\n"
	path := filepath.Join(dir, "geoform.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := storage.LoadFile(path, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Styles) != 1 || d.Styles[0].SLD != `<StyledLayerDescriptor/>` {
		t.Errorf("styles: %+v", d.Styles)
	}
}

func TestParse_StyleAndGroupErrors(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"file y sld":  {"workspaces:\n  - name: d\n    styles:\n      - name: a\n        file: x.sld\n        sld: y\n", "usa file o sld, no ambos"},
		"sin origen":  {"workspaces:\n  - name: d\n    styles:\n      - name: a\n", "file o sld es obligatorio"},
		"file falta":  {"workspaces:\n  - name: d\n    styles:\n      - name: a\n        file: /no/existe.sld\n", "no se puede leer el SLD"},
		"estilo dup":  {"workspaces:\n  - name: d\n    styles:\n      - {name: a, sld: x}\n      - {name: a, sld: x}\n", `estilo "a" duplicado`},
		"grupo vacío": {"workspaces:\n  - name: d\n    layergroups:\n      - name: g\n", "al menos una capa"},
		"modo malo":   {"workspaces:\n  - name: d\n    layergroups:\n      - {name: g, mode: X, layers: [a]}\n", "modo"},
		"grupo dup":   {"workspaces:\n  - name: d\n    layergroups:\n      - {name: g, layers: [a]}\n      - {name: g, layers: [a]}\n", `grupo "g" duplicado`},
	}
	for name, c := range cases {
		_, err := storage.Parse(strings.NewReader(c.yaml), env(nil))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}
