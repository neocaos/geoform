// Package storage lee el estado deseado desde un fichero YAML y lo traduce a
// application.Desired.
package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
)

// Formato del fichero (los campos desconocidos son un error):
//
//	workspaces:
//	  - name: demo
//	    isolated: false
//	    datastores:
//	      - name: postgis
//	        host: db
//	        port: 5432            # opcional, 5432
//	        database: geoform
//	        schema: geodata       # opcional, public
//	        user: geouser
//	        password_env: PG_PASSWORD   # o password: (no recomendado)
//	        layers:
//	          - name: cities
//	            table: cities     # opcional, por defecto el nombre
//	            title: Ciudades
//	            srs: EPSG:4326
type fileConfig struct {
	Workspaces []fileWorkspace `yaml:"workspaces"`
}

type fileWorkspace struct {
	Name       string          `yaml:"name"`
	Isolated   bool            `yaml:"isolated"`
	Datastores []fileDatastore `yaml:"datastores"`
}

type fileDatastore struct {
	Name        string      `yaml:"name"`
	Host        string      `yaml:"host"`
	Port        int         `yaml:"port"`
	Database    string      `yaml:"database"`
	Schema      string      `yaml:"schema"`
	User        string      `yaml:"user"`
	Password    string      `yaml:"password"`
	PasswordEnv string      `yaml:"password_env"`
	Layers      []fileLayer `yaml:"layers"`
}

type fileLayer struct {
	Name  string `yaml:"name"`
	Table string `yaml:"table"`
	Title string `yaml:"title"`
	SRS   string `yaml:"srs"`
}

var (
	// Nombres válidos en GeoServer: letra o _ al inicio, sin espacios ni ':' o '/'.
	namePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	srsPattern  = regexp.MustCompile(`^EPSG:[0-9]+$`)
)

// LoadFile lee y valida el fichero de configuración en path. getenv resuelve
// password_env (normalmente os.Getenv).
func LoadFile(path string, getenv func(string) string) (application.Desired, error) {
	f, err := os.Open(path)
	if err != nil {
		return application.Desired{}, fmt.Errorf("abriendo configuración: %w", err)
	}
	defer f.Close()
	d, err := Parse(f, getenv)
	if err != nil {
		return application.Desired{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// Parse lee y valida la configuración YAML de r. Devuelve todos los errores de
// validación juntos, cada uno con su ruta (p. ej. workspaces[0].datastores[1].port).
func Parse(r io.Reader, getenv func(string) string) (application.Desired, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return application.Desired{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return application.Desired{}, errors.New("el fichero está vacío")
	}
	var cfg fileConfig
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return application.Desired{}, fmt.Errorf("YAML inválido: %w", err)
	}
	if len(cfg.Workspaces) == 0 {
		return application.Desired{}, errors.New("no hay ningún workspace declarado (clave 'workspaces')")
	}
	return build(cfg, getenv)
}

func build(cfg fileConfig, getenv func(string) string) (application.Desired, error) {
	var (
		out  application.Desired
		errs []error
	)
	bad := func(path, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...)))
	}
	checkName := func(path, name string) bool {
		switch {
		case name == "":
			bad(path, "el nombre es obligatorio")
		case !namePattern.MatchString(name):
			bad(path, "nombre %q inválido: usa letras, dígitos, '_', '-' o '.', sin empezar por dígito", name)
		default:
			return true
		}
		return false
	}

	wsSeen := map[string]bool{}
	for i, w := range cfg.Workspaces {
		wp := fmt.Sprintf("workspaces[%d]", i)
		if checkName(wp+".name", w.Name) {
			if wsSeen[w.Name] {
				bad(wp+".name", "workspace %q duplicado", w.Name)
			}
			wsSeen[w.Name] = true
		}
		out.Workspaces = append(out.Workspaces, workspace.Workspace{Name: w.Name, Isolated: w.Isolated})

		dsSeen := map[string]bool{}
		for j, d := range w.Datastores {
			dp := fmt.Sprintf("%s.datastores[%d]", wp, j)
			if checkName(dp+".name", d.Name) {
				if dsSeen[d.Name] {
					bad(dp+".name", "datastore %q duplicado en el workspace %q", d.Name, w.Name)
				}
				dsSeen[d.Name] = true
			}
			for _, f := range []struct{ name, val string }{{"host", d.Host}, {"database", d.Database}, {"user", d.User}} {
				if strings.TrimSpace(f.val) == "" {
					bad(dp+"."+f.name, "es obligatorio")
				}
			}
			if d.Port < 0 || d.Port > 65535 {
				bad(dp+".port", "puerto %d fuera de rango (1-65535)", d.Port)
			}
			pass, err := resolvePassword(d, getenv)
			if err != nil {
				bad(dp, "%v", err)
			}
			out.Datastores = append(out.Datastores, datastore.Datastore{
				Workspace: w.Name, Name: d.Name,
				PostGIS: datastore.PostGIS{Host: d.Host, Port: d.Port, Database: d.Database, Schema: d.Schema, User: d.User, Password: pass},
			})

			lySeen := map[string]bool{}
			for k, l := range d.Layers {
				lp := fmt.Sprintf("%s.layers[%d]", dp, k)
				if checkName(lp+".name", l.Name) {
					if lySeen[l.Name] {
						bad(lp+".name", "capa %q duplicada en el datastore %q", l.Name, d.Name)
					}
					lySeen[l.Name] = true
				}
				switch {
				case l.SRS == "":
					bad(lp+".srs", "es obligatorio (p. ej. EPSG:4326)")
				case !srsPattern.MatchString(l.SRS):
					bad(lp+".srs", "SRS %q inválido, se espera el formato EPSG:<código>", l.SRS)
				}
				out.Layers = append(out.Layers, layer.Layer{Workspace: w.Name, Store: d.Name, Name: l.Name, Table: l.Table, Title: l.Title, SRS: l.SRS})
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return application.Desired{}, err
	}
	return out, nil
}

func resolvePassword(d fileDatastore, getenv func(string) string) (string, error) {
	switch {
	case d.Password != "" && d.PasswordEnv != "":
		return "", errors.New("usa password o password_env, no ambos")
	case d.PasswordEnv != "":
		v := getenv(d.PasswordEnv)
		if v == "" {
			return "", fmt.Errorf("la variable de entorno %s (password_env) no está definida", d.PasswordEnv)
		}
		return v, nil
	}
	return d.Password, nil
}
