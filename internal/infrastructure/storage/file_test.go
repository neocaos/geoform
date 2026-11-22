package storage_test

import (
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
