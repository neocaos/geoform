// Package importer lee el estado real de un GeoServer (workspaces, datastores
// PostGIS y capas) y lo traduce a application.Desired, para generar el fichero
// de configuración de geoform.
package importer

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
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
	}
	return res, nil
}

// toDatastore devuelve el datastore PostGIS o el motivo por el que no se puede
// representar. Equivale a datastore.Repository.Get, que no expone el tipo.
func toDatastore(ws string, ds *gs.DataStore) (datastore.Datastore, string) {
	p := ds.ConnectionParameters
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
