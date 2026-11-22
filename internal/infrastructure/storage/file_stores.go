package storage

import (
	"fmt"
	"strings"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/coverage"
	"github.com/neocaos/geoform/internal/domain/datastore"
)

type fileCoverageStore struct {
	Name      string         `yaml:"name"`
	Type      string         `yaml:"type,omitempty"` // geotiff (por defecto)
	URL       string         `yaml:"url"`
	Coverages []fileCoverage `yaml:"coverages,omitempty"`
}

type fileCoverage struct {
	Name       string `yaml:"name"`
	NativeName string `yaml:"native_name,omitempty"`
	Title      string `yaml:"title,omitempty"`
	SRS        string `yaml:"srs"`
}

type badFunc = func(path, format string, args ...any)

// buildDatastore valida y traduce un datastore del fichero según su tipo. Si
// ok es false ya se han registrado los errores; el Datastore devuelto solo
// lleva el tipo válido para que las referencias de las capas sigan resolviendo.
func buildDatastore(d fileDatastore, ws, dp string, getenv func(string) string, bad badFunc) (datastore.Datastore, bool) {
	typ := datastore.Type(strings.ToLower(strings.TrimSpace(d.Type)))
	switch typ {
	case "", datastore.TypePostGIS:
		if d.File != "" {
			bad(dp+".file", "solo aplica a los tipos geopackage y shapefile")
		}
		ok := d.File == ""
		for _, f := range []struct{ name, val string }{{"host", d.Host}, {"database", d.Database}, {"user", d.User}} {
			if strings.TrimSpace(f.val) == "" {
				bad(dp+"."+f.name, "es obligatorio")
				ok = false
			}
		}
		if d.Port < 0 || d.Port > 65535 {
			bad(dp+".port", "puerto %d fuera de rango (1-65535)", d.Port)
			ok = false
		}
		pass, err := resolvePassword(d, getenv)
		if err != nil {
			bad(dp, "%v", err)
			ok = false
		}
		return datastore.Datastore{
			Workspace: ws, Name: d.Name, Type: typ,
			PostGIS: datastore.PostGIS{Host: d.Host, Port: d.Port, Database: d.Database, Schema: d.Schema, User: d.User, Password: pass},
		}, ok
	case datastore.TypeGeoPackage, datastore.TypeShapefile:
		ok := true
		if strings.TrimSpace(d.File) == "" {
			bad(dp+".file", "es obligatorio para el tipo %s", typ)
			ok = false
		}
		for _, f := range []struct {
			name string
			set  bool
		}{
			{"host", d.Host != ""}, {"port", d.Port != 0}, {"database", d.Database != ""}, {"schema", d.Schema != ""},
			{"user", d.User != ""}, {"password", d.Password != ""}, {"password_env", d.PasswordEnv != ""},
		} {
			if f.set {
				bad(dp+"."+f.name, "no aplica al tipo %s (usa file)", typ)
				ok = false
			}
		}
		return datastore.Datastore{Workspace: ws, Name: d.Name, Type: typ, File: d.File}, ok
	default:
		bad(dp+".type", "tipo %q no soportado (postgis, geopackage o shapefile)", d.Type)
		return datastore.Datastore{Workspace: ws, Name: d.Name, Type: datastore.TypeShapefile}, false
	}
}

// buildCoverageStores traduce los almacenes raster de un workspace.
func buildCoverageStores(w fileWorkspace, wp string, out *application.Desired, checkName func(path, name string) bool, bad badFunc) {
	seen := map[string]bool{}
	for i, cs := range w.CoverageStores {
		cp := fmt.Sprintf("%s.coveragestores[%d]", wp, i)
		if checkName(cp+".name", cs.Name) {
			if seen[cs.Name] {
				bad(cp+".name", "coveragestore %q duplicado en el workspace %q", cs.Name, w.Name)
			}
			seen[cs.Name] = true
		}
		typ := coverage.StoreType(strings.ToLower(strings.TrimSpace(cs.Type)))
		if typ != "" && typ != coverage.TypeGeoTIFF {
			bad(cp+".type", "tipo %q no soportado (geotiff)", cs.Type)
		}
		if strings.TrimSpace(cs.URL) == "" {
			bad(cp+".url", "es obligatorio (p. ej. file:/data/dem.tif)")
		}
		out.CoverageStores = append(out.CoverageStores, coverage.Store{Workspace: w.Name, Name: cs.Name, Type: typ, URL: cs.URL})

		lySeen := map[string]bool{}
		for k, c := range cs.Coverages {
			lp := fmt.Sprintf("%s.coverages[%d]", cp, k)
			if checkName(lp+".name", c.Name) {
				if lySeen[c.Name] {
					bad(lp+".name", "coverage %q duplicada en el coveragestore %q", c.Name, cs.Name)
				}
				lySeen[c.Name] = true
			}
			switch {
			case c.SRS == "":
				bad(lp+".srs", "es obligatorio (p. ej. EPSG:4326)")
			case !srsPattern.MatchString(c.SRS):
				bad(lp+".srs", "SRS %q inválido, se espera el formato EPSG:<código>", c.SRS)
			}
			out.Coverages = append(out.Coverages, coverage.Coverage{
				Workspace: w.Name, Store: cs.Name, Name: c.Name, Native: c.NativeName, Title: c.Title, SRS: c.SRS,
			})
		}
	}
}
