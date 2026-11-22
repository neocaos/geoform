// Package storage lee el estado deseado desde un fichero YAML y lo traduce a
// application.Desired.
package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/style"
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
//	            style: ciudades   # opcional, estilo por defecto (ver styles)
//	styles:                       # opcional, por workspace
//	  - name: ciudades
//	    file: styles/ciudades.sld # ruta relativa al YAML; o bien sld: | (inline)
//	layergroups:                  # opcional, por workspace
//	  - name: base
//	    title: Capa base          # opcional
//	    mode: SINGLE              # opcional, SINGLE por defecto
//	    layers: [regions, cities] # capas del workspace, en orden de dibujo
//	      # Datastores de fichero: type geopackage | shapefile + file (URL de
//	      # GeoServer, p. ej. file:/data/places.gpkg). No llevan host/user/...;
//	      # en las capas, table es la tabla del gpkg o el nombre del .shp.
//	      - name: places
//	        type: geopackage
//	        file: file:/data/places.gpkg
//	        layers: [{name: places, srs: EPSG:4326}]
//	    # Almacenes raster (GeoTIFF) y sus coverages:
//	    coveragestores:
//	      - name: dem
//	        type: geotiff       # opcional, único valor soportado
//	        url: file:/data/dem.tif
//	        coverages:
//	          - name: dem
//	            native_name: dem  # opcional, por defecto el nombre
//	            title: Relieve
//	            srs: EPSG:4326
type fileConfig struct {
	Workspaces []fileWorkspace `yaml:"workspaces"`
	Security   *fileSecurity   `yaml:"security,omitempty"`
}

type fileWorkspace struct {
	Name       string          `yaml:"name"`
	Isolated   bool            `yaml:"isolated"`
	Datastores []fileDatastore `yaml:"datastores,omitempty"`
	// Almacenes raster (GeoTIFF).
	CoverageStores []fileCoverageStore `yaml:"coveragestores,omitempty"`
	Styles         []fileStyle         `yaml:"styles,omitempty"`
	LayerGroups    []fileLayerGroup    `yaml:"layergroups,omitempty"`
}

type fileStyle struct {
	Name string `yaml:"name"`
	File string `yaml:"file,omitempty"`
	SLD  string `yaml:"sld,omitempty"`
}

type fileLayerGroup struct {
	Name   string   `yaml:"name"`
	Title  string   `yaml:"title,omitempty"`
	Mode   string   `yaml:"mode,omitempty"`
	Layers []string `yaml:"layers"`
}

type fileDatastore struct {
	Name        string      `yaml:"name"`
	Type        string      `yaml:"type,omitempty"` // postgis (por defecto), geopackage o shapefile
	File        string      `yaml:"file,omitempty"` // solo geopackage y shapefile
	Host        string      `yaml:"host,omitempty"`
	Port        int         `yaml:"port,omitempty"`
	Database    string      `yaml:"database,omitempty"`
	Schema      string      `yaml:"schema,omitempty"`
	User        string      `yaml:"user,omitempty"`
	Password    string      `yaml:"password,omitempty"`
	PasswordEnv string      `yaml:"password_env,omitempty"`
	Layers      []fileLayer `yaml:"layers,omitempty"`
}

type fileLayer struct {
	Name  string `yaml:"name"`
	Table string `yaml:"table,omitempty"`
	Title string `yaml:"title,omitempty"`
	SRS   string `yaml:"srs"`
	Style string `yaml:"style,omitempty"`
}

var (
	// Nombres válidos en GeoServer: letra o _ al inicio, sin espacios ni ':' o '/'.
	namePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	srsPattern  = regexp.MustCompile(`^EPSG:[0-9]+$`)
	// yamlQuoted detecta los valores entre acentos graves de los errores de yaml.v3.
	yamlQuoted = regexp.MustCompile("`[^`]*`")
)

// LoadFile lee y valida el fichero de configuración en path. getenv resuelve
// password_env (normalmente os.Getenv).
func LoadFile(path string, getenv func(string) string) (application.Desired, error) {
	f, err := os.Open(path) // #nosec G304 -- la ruta es el --file que el usuario pasa a la CLI
	if err != nil {
		return application.Desired{}, fmt.Errorf("abriendo configuración: %w", err)
	}
	defer f.Close()
	d, err := parse(f, getenv, filepath.Dir(path))
	if err != nil {
		return application.Desired{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// Parse lee y valida la configuración YAML de r. Devuelve todos los errores de
// validación juntos, cada uno con su ruta (p. ej. workspaces[0].datastores[1].port).
func Parse(r io.Reader, getenv func(string) string) (application.Desired, error) {
	return parse(r, getenv, "")
}

// parse es Parse con el directorio base para resolver styles[].file.
func parse(r io.Reader, getenv func(string) string, baseDir string) (application.Desired, error) {
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
		// yaml.v3 cita el valor ofensivo (`valor`); se oculta por si es un secreto.
		return application.Desired{}, fmt.Errorf("YAML inválido: %s", yamlQuoted.ReplaceAllString(err.Error(), "`…`"))
	}
	if len(cfg.Workspaces) == 0 {
		return application.Desired{}, errors.New("no hay ningún workspace declarado (clave 'workspaces')")
	}
	return build(cfg, getenv, baseDir)
}

func build(cfg fileConfig, getenv func(string) string, baseDir string) (application.Desired, error) {
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
			// Si hay errores se devuelven todos y el resultado se descarta.
			dstore, _ := buildDatastore(d, w.Name, dp, getenv, bad)
			out.Datastores = append(out.Datastores, dstore)

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
				out.Layers = append(out.Layers, layer.Layer{Workspace: w.Name, Store: d.Name, Name: l.Name, Table: l.Table, Title: l.Title, SRS: l.SRS, Style: l.Style})
			}
		}
		buildCoverageStores(w, wp, &out, checkName, bad)
	}
	if cfg.Security != nil {
		sec, secErrs := buildSecurity(*cfg.Security, getenv)
		out.Security = &sec
		errs = append(errs, secErrs...)
	}
	for i, w := range cfg.Workspaces {
		wp := fmt.Sprintf("workspaces[%d]", i)
		buildStyles(&out, w, wp, baseDir, bad, checkName)
		buildLayerGroups(&out, w, wp, bad, checkName)
	}
	if err := errors.Join(errs...); err != nil {
		return application.Desired{}, err
	}
	return out, nil
}

func buildStyles(out *application.Desired, w fileWorkspace, wp, baseDir string,
	bad func(path, format string, args ...any), checkName func(path, name string) bool) {
	seen := map[string]bool{}
	for i, st := range w.Styles {
		sp := fmt.Sprintf("%s.styles[%d]", wp, i)
		if checkName(sp+".name", st.Name) {
			if seen[st.Name] {
				bad(sp+".name", "estilo %q duplicado en el workspace %q", st.Name, w.Name)
			}
			seen[st.Name] = true
		}
		sld := st.SLD
		switch {
		case st.File != "" && st.SLD != "":
			bad(sp, "usa file o sld, no ambos")
			continue
		case st.File != "":
			p := st.File
			if !filepath.IsAbs(p) {
				p = filepath.Join(baseDir, p)
			}
			b, err := os.ReadFile(p) // #nosec G304 -- el YAML es configuración de confianza; la ruta del SLD la elige quien lo escribe
			if err != nil {
				bad(sp+".file", "no se puede leer el SLD: %v", err)
				continue
			}
			sld = string(b)
		case st.SLD == "":
			bad(sp, "file o sld es obligatorio")
			continue
		}
		out.Styles = append(out.Styles, style.Style{Workspace: w.Name, Name: st.Name, SLD: sld})
	}
	// Las referencias layers[].style se validan en el dominio (Desired.Validate).
}

func buildLayerGroups(out *application.Desired, w fileWorkspace, wp string,
	bad func(path, format string, args ...any), checkName func(path, name string) bool) {
	seen := map[string]bool{}
	for i, g := range w.LayerGroups {
		gp := fmt.Sprintf("%s.layergroups[%d]", wp, i)
		if checkName(gp+".name", g.Name) {
			if seen[g.Name] {
				bad(gp+".name", "grupo %q duplicado en el workspace %q", g.Name, w.Name)
			}
			seen[g.Name] = true
		}
		if len(g.Layers) == 0 {
			bad(gp+".layers", "debe incluir al menos una capa")
		}
		grp := layergroup.Group{Workspace: w.Name, Name: g.Name, Title: g.Title, Mode: g.Mode, Layers: g.Layers}
		// Las reglas de modo y capas repetidas son del dominio.
		if err := grp.Validate(); err != nil && len(g.Layers) > 0 {
			bad(gp, "%v", err)
		}
		out.LayerGroups = append(out.LayerGroups, grp)
	}
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
