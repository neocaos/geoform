// Package importer lee el estado real de un GeoServer (workspaces, datastores
// PostGIS y capas) y lo traduce a application.Desired, para generar el fichero
// de configuración de geoform.
package importer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/coverage"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/domain/workspace"
	gs "github.com/neocaos/geoform/internal/geoserver"
)

// Source es la parte del cliente REST que necesita el importador.
type Source interface {
	ListWorkspaces(ctx context.Context) ([]string, error)
	GetWorkspace(ctx context.Context, name string) (*gs.Workspace, error)
	ListDataStores(ctx context.Context, workspace string) ([]string, error)
	GetDataStore(ctx context.Context, workspace, name string) (*gs.DataStore, error)
	ListFeatureTypes(ctx context.Context, workspace, store string) ([]string, error)
	GetFeatureType(ctx context.Context, workspace, store, name string) (*gs.FeatureType, error)
}

// StyleSource es la parte opcional del cliente REST para importar estilos,
// grupos de capas y el estilo por defecto de cada capa. Si el Source no la
// implementa, esas secciones no se importan.
type StyleSource interface {
	ListStyles(ctx context.Context, workspace string) ([]string, error)
	GetStyleSLD(ctx context.Context, workspace, name string) (string, error)
	ListLayerGroups(ctx context.Context, workspace string) ([]string, error)
	GetLayerGroup(ctx context.Context, workspace, name string) (*gs.LayerGroup, error)
	GetLayerDefaultStyle(ctx context.Context, workspace, layer string) (*gs.StyleRef, error)
}

// CoverageSource es la parte opcional del cliente REST para importar los
// almacenes raster (GeoTIFF) y sus coverages. Si el Source no la implementa,
// el raster no se importa.
type CoverageSource interface {
	ListCoverageStores(ctx context.Context, workspace string) ([]string, error)
	GetCoverageStore(ctx context.Context, workspace, name string) (*gs.CoverageStore, error)
	ListCoverages(ctx context.Context, workspace, store string) ([]string, error)
	GetCoverage(ctx context.Context, workspace, store, name string) (*gs.Coverage, error)
}

// Result es el estado importado y los avisos de lo que no se pudo representar.
type Result struct {
	Desired  application.Desired
	Warnings []string
}

var (
	namePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	srsPattern  = regexp.MustCompile(`^EPSG:[0-9]+$`)
)

// Import lee GeoServer. Si only no está vacío, se limita a esos workspaces.
// Lo que Geoform no gestiona (stores no PostGIS, capas sin SRS EPSG...) se
// omite con un aviso. Los passwords nunca se leen.
func Import(ctx context.Context, src Source, only []string) (*Result, error) {
	names, err := src.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("listando workspaces: %w", err)
	}
	if len(only) > 0 {
		have := map[string]bool{}
		for _, n := range names {
			have[n] = true
		}
		for _, o := range only {
			if !have[o] {
				return nil, fmt.Errorf("el workspace %q no existe en GeoServer", o)
			}
		}
		names = only
	}
	sort.Strings(names)

	res := &Result{}
	warn := func(format string, args ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, args...)) }
	for _, wn := range names {
		if !namePattern.MatchString(wn) {
			warn("workspace %q omitido: nombre no válido para la configuración", wn)
			continue
		}
		ws, err := src.GetWorkspace(ctx, wn)
		if err != nil {
			return nil, fmt.Errorf("leyendo workspace %q: %w", wn, err)
		}
		res.Desired.Workspaces = append(res.Desired.Workspaces, workspace.Workspace{Name: wn, Isolated: ws.Isolated})

		stores, err := src.ListDataStores(ctx, wn)
		if err != nil {
			return nil, fmt.Errorf("listando datastores de %q: %w", wn, err)
		}
		sort.Strings(stores)
		for _, sn := range stores {
			ds, err := src.GetDataStore(ctx, wn, sn)
			if err != nil {
				return nil, fmt.Errorf("leyendo datastore %s/%s: %w", wn, sn, err)
			}
			d, reason := toDatastore(wn, ds)
			if reason != "" {
				warn("datastore %s/%s omitido: %s", wn, sn, reason)
				continue
			}
			res.Desired.Datastores = append(res.Desired.Datastores, d)

			layers, err := src.ListFeatureTypes(ctx, wn, sn)
			if err != nil {
				return nil, fmt.Errorf("listando capas de %s/%s: %w", wn, sn, err)
			}
			sort.Strings(layers)
			for _, ln := range layers {
				ft, err := src.GetFeatureType(ctx, wn, sn, ln)
				if err != nil {
					return nil, fmt.Errorf("leyendo capa %s/%s/%s: %w", wn, sn, ln, err)
				}
				if !srsPattern.MatchString(ft.SRS) {
					warn("capa %s/%s/%s omitida: SRS %q no es EPSG:<código>", wn, sn, ln, ft.SRS)
					continue
				}
				res.Desired.Layers = append(res.Desired.Layers, layer.Layer{
					Workspace: wn, Store: sn, Name: ft.Name, Table: ft.NativeName, Title: ft.Title, SRS: ft.SRS,
				})
			}
		}
		if cs, ok := src.(CoverageSource); ok {
			if err := importCoverages(ctx, cs, wn, res, warn); err != nil {
				return nil, err
			}
		}
		if ss, ok := src.(StyleSource); ok {
			if err := importStyles(ctx, ss, wn, res, warn); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// importStyles lee los estilos y grupos de capas de wn y el estilo por defecto
// de las capas ya importadas del workspace. Lo que no se puede representar se
// omite con un aviso. Un error de API (p. ej. un estilo que no es SLD) es un
// aviso; un error de red o de decodificación aborta la importación.
func importStyles(ctx context.Context, src StyleSource, wn string, res *Result, warn func(string, ...any)) error {
	d := &res.Desired
	names, err := src.ListStyles(ctx, wn)
	if err != nil {
		return fmt.Errorf("listando estilos de %q: %w", wn, err)
	}
	sort.Strings(names)
	wsStyles := map[string]bool{}
	for _, n := range names {
		if !namePattern.MatchString(n) {
			warn("estilo %s/%s omitido: nombre no válido para la configuración", wn, n)
			continue
		}
		sld, err := src.GetStyleSLD(ctx, wn, n)
		var apiErr *gs.APIError
		if errors.As(err, &apiErr) {
			warn("estilo %s/%s omitido: no se pudo leer como SLD (%d)", wn, n, apiErr.StatusCode)
			continue
		}
		if err != nil {
			return fmt.Errorf("leyendo estilo %s/%s: %w", wn, n, err)
		}
		st := style.Style{Workspace: wn, Name: n, SLD: sld}
		if err := st.Validate(); err != nil {
			warn("estilo %s/%s omitido: %v", wn, n, err)
			continue
		}
		wsStyles[n] = true
		d.Styles = append(d.Styles, st)
	}

	inWS := map[string]bool{}
	for _, c := range d.Coverages {
		if c.Workspace == wn {
			inWS[c.Name] = true
		}
	}
	for i, l := range d.Layers {
		if l.Workspace != wn {
			continue
		}
		inWS[l.Name] = true
		ref, err := src.GetLayerDefaultStyle(ctx, wn, l.Name)
		if err != nil && !gs.IsNotFound(err) {
			return fmt.Errorf("leyendo estilo de la capa %s/%s: %w", wn, l.Name, err)
		}
		if ref == nil {
			continue
		}
		switch {
		case ref.Workspace == wn && wsStyles[ref.Name]:
			d.Layers[i].Style = ref.Name
		case ref.Workspace == wn:
			warn("capa %s/%s: su estilo %q no se importó; queda sin gestionar", wn, l.Name, ref.Name)
		case ref.Workspace != "":
			d.Layers[i].Style = ref.Workspace + ":" + ref.Name
		case wsStyles[ref.Name]:
			// Un nombre sin prefijo se resolvería al estilo del workspace.
			warn("capa %s/%s: el estilo global %q queda oculto por un estilo del workspace con el mismo nombre; queda sin gestionar", wn, l.Name, ref.Name)
		default:
			d.Layers[i].Style = ref.Name
		}
	}

	groups, err := src.ListLayerGroups(ctx, wn)
	if err != nil {
		return fmt.Errorf("listando grupos de capas de %q: %w", wn, err)
	}
	sort.Strings(groups)
	for _, gn := range groups {
		if !namePattern.MatchString(gn) {
			warn("grupo %s/%s omitido: nombre no válido para la configuración", wn, gn)
			continue
		}
		g, err := src.GetLayerGroup(ctx, wn, gn)
		if err != nil {
			return fmt.Errorf("leyendo grupo %s/%s: %w", wn, gn, err)
		}
		grp := layergroup.Group{Workspace: wn, Name: gn, Title: g.Title, Mode: g.Mode}
		ok := true
		for _, l := range g.Layers {
			local := strings.TrimPrefix(l, wn+":")
			if !inWS[local] {
				warn("grupo %s/%s omitido: contiene %q, que no es una capa importada del workspace", wn, gn, l)
				ok = false
				break
			}
			grp.Layers = append(grp.Layers, local)
		}
		if !ok {
			continue
		}
		if err := grp.Validate(); err != nil {
			warn("grupo %s/%s omitido: %v", wn, gn, err)
			continue
		}
		d.LayerGroups = append(d.LayerGroups, grp)
	}
	return nil
}

// importCoverages lee los almacenes GeoTIFF de wn y sus coverages. Los
// almacenes de otro formato y las coverages sin SRS EPSG se omiten con aviso.
func importCoverages(ctx context.Context, src CoverageSource, wn string, res *Result, warn func(string, ...any)) error {
	stores, err := src.ListCoverageStores(ctx, wn)
	if err != nil {
		return fmt.Errorf("listando coveragestores de %q: %w", wn, err)
	}
	sort.Strings(stores)
	for _, sn := range stores {
		cs, err := src.GetCoverageStore(ctx, wn, sn)
		if err != nil {
			return fmt.Errorf("leyendo coveragestore %s/%s: %w", wn, sn, err)
		}
		if cs.Type != gs.CoverageStoreTypeGeoTIFF {
			warn("coveragestore %s/%s omitido: no es GeoTIFF (tipo %q)", wn, sn, cs.Type)
			continue
		}
		st := coverage.Store{Workspace: wn, Name: sn, Type: coverage.TypeGeoTIFF, URL: cs.URL}
		if err := st.Validate(); err != nil {
			warn("coveragestore %s/%s omitido: %v", wn, sn, err)
			continue
		}
		res.Desired.CoverageStores = append(res.Desired.CoverageStores, st)

		names, err := src.ListCoverages(ctx, wn, sn)
		if err != nil {
			return fmt.Errorf("listando coverages de %s/%s: %w", wn, sn, err)
		}
		sort.Strings(names)
		for _, cn := range names {
			cv, err := src.GetCoverage(ctx, wn, sn, cn)
			if err != nil {
				return fmt.Errorf("leyendo coverage %s/%s/%s: %w", wn, sn, cn, err)
			}
			if !srsPattern.MatchString(cv.SRS) {
				warn("coverage %s/%s/%s omitida: SRS %q no es EPSG:<código>", wn, sn, cn, cv.SRS)
				continue
			}
			res.Desired.Coverages = append(res.Desired.Coverages, coverage.Coverage{
				Workspace: wn, Store: sn, Name: cv.Name, Native: cv.NativeName, Title: cv.Title, SRS: cv.SRS,
			})
		}
	}
	return nil
}

// toDatastore devuelve el datastore (PostGIS, GeoPackage o shapefile) o el
// motivo por el que no se puede representar. Equivale a datastore.Repository.Get.
func toDatastore(ws string, ds *gs.DataStore) (datastore.Datastore, string) {
	p := ds.ConnectionParameters
	switch {
	case strings.EqualFold(p["dbtype"], gs.DBTypeGeoPackage):
		d := datastore.Datastore{Workspace: ws, Name: ds.Name, Type: datastore.TypeGeoPackage, File: p["database"]}
		if err := d.Validate(); err != nil {
			return datastore.Datastore{}, "GeoPackage incompleto: " + err.Error()
		}
		return d, ""
	case p["dbtype"] == "" && p["url"] != "":
		d := datastore.Datastore{Workspace: ws, Name: ds.Name, Type: datastore.TypeShapefile, File: p["url"]}
		if err := d.Validate(); err != nil {
			return datastore.Datastore{}, "shapefile incompleto: " + err.Error()
		}
		return d, ""
	}
	if !strings.EqualFold(p["dbtype"], "postgis") {
		return datastore.Datastore{}, fmt.Sprintf("no es PostGIS (tipo %q)", ds.Type)
	}
	port, _ := strconv.Atoi(p["port"])
	d := datastore.Datastore{Workspace: ws, Name: ds.Name, PostGIS: datastore.PostGIS{
		Host: p["host"], Port: port, Database: p["database"], Schema: p["schema"], User: p["user"],
	}}
	if err := d.Validate(); err != nil {
		return datastore.Datastore{}, "conexión incompleta (¿JNDI?): " + err.Error()
	}
	return d, ""
}
