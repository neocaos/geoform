// Package geoserver adapts the GeoServer REST client (internal/geoserver) to
// the domain repositories.
package geoserver

import (
	"context"
	"strconv"
	"strings"

	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
	gs "github.com/neocaos/geoform/internal/geoserver"
)

// WorkspaceRepository adapta Client a workspace.Repository.
type WorkspaceRepository struct{ c *gs.Client }

func NewWorkspaceRepository(c *gs.Client) *WorkspaceRepository { return &WorkspaceRepository{c} }

func (r *WorkspaceRepository) Get(ctx context.Context, name string) (*workspace.Workspace, error) {
	ws, err := r.c.GetWorkspace(ctx, name)
	if gs.IsNotFound(err) {
		return nil, workspace.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &workspace.Workspace{Name: ws.Name, Isolated: ws.Isolated}, nil
}

func (r *WorkspaceRepository) Create(ctx context.Context, w workspace.Workspace) error {
	return r.c.CreateWorkspace(ctx, gs.Workspace{Name: w.Name, Isolated: w.Isolated})
}

func (r *WorkspaceRepository) Update(ctx context.Context, w workspace.Workspace) error {
	err := r.c.UpdateWorkspace(ctx, w.Name, gs.Workspace{Name: w.Name, Isolated: w.Isolated})
	if gs.IsNotFound(err) {
		return workspace.ErrNotFound
	}
	return err
}

func (r *WorkspaceRepository) Delete(ctx context.Context, name string, recurse bool) error {
	err := r.c.DeleteWorkspace(ctx, name, recurse)
	if gs.IsNotFound(err) {
		return workspace.ErrNotFound
	}
	return err
}

// DatastoreRepository adapta Client a datastore.Repository (PostGIS).
type DatastoreRepository struct{ c *gs.Client }

func NewDatastoreRepository(c *gs.Client) *DatastoreRepository { return &DatastoreRepository{c} }

func (r *DatastoreRepository) Get(ctx context.Context, ws, name string) (*datastore.Datastore, error) {
	ds, err := r.c.GetDataStore(ctx, ws, name)
	if gs.IsNotFound(err) {
		return nil, datastore.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(ds.ConnectionParameters["port"])
	return &datastore.Datastore{
		Workspace: ws,
		Name:      ds.Name,
		PostGIS: datastore.PostGIS{
			Host:     ds.ConnectionParameters["host"],
			Port:     port,
			Database: ds.ConnectionParameters["database"],
			Schema:   ds.ConnectionParameters["schema"],
			User:     ds.ConnectionParameters["user"],
			// GeoServer enmascara el password al leerlo; no se devuelve.
		},
	}, nil
}

func (r *DatastoreRepository) List(ctx context.Context, ws string) ([]string, error) {
	names, err := r.c.ListDataStores(ctx, ws)
	if gs.IsNotFound(err) {
		return nil, nil
	}
	return names, err
}

// postgisParams traduce los parámetros de dominio a los de GeoServer.
func postgisParams(p datastore.PostGIS) map[string]string {
	port := p.Port
	if port == 0 {
		port = 5432
	}
	schema := p.Schema
	if schema == "" {
		schema = "public"
	}
	return map[string]string{
		"dbtype":              "postgis",
		"host":                p.Host,
		"port":                strconv.Itoa(port),
		"database":            p.Database,
		"schema":              schema,
		"user":                p.User,
		"passwd":              p.Password,
		"Expose primary keys": "true",
	}
}

func (r *DatastoreRepository) Create(ctx context.Context, d datastore.Datastore) error {
	return r.c.CreateDataStore(ctx, d.Workspace, gs.DataStore{
		Name:                 d.Name,
		Type:                 "PostGIS",
		Enabled:              true,
		ConnectionParameters: postgisParams(d.PostGIS),
	})
}

// Update conserva los parámetros que Geoform no gestiona (pools, etc.) y
// sobrescribe los gestionados.
func (r *DatastoreRepository) Update(ctx context.Context, d datastore.Datastore) error {
	cur, err := r.c.GetDataStore(ctx, d.Workspace, d.Name)
	if gs.IsNotFound(err) {
		return datastore.ErrNotFound
	}
	if err != nil {
		return err
	}
	params := gs.ConnectionParameters{}
	for k, v := range cur.ConnectionParameters {
		params[k] = v
	}
	for k, v := range postgisParams(d.PostGIS) {
		params[k] = v
	}
	cur.ConnectionParameters = params
	return r.c.UpdateDataStore(ctx, d.Workspace, d.Name, *cur)
}

func (r *DatastoreRepository) Delete(ctx context.Context, ws, name string, recurse bool) error {
	err := r.c.DeleteDataStore(ctx, ws, name, recurse)
	if gs.IsNotFound(err) {
		return datastore.ErrNotFound
	}
	return err
}

// LayerRepository adapta Client a layer.Repository.
type LayerRepository struct{ c *gs.Client }

func NewLayerRepository(c *gs.Client) *LayerRepository { return &LayerRepository{c} }

func (r *LayerRepository) Get(ctx context.Context, ws, store, name string) (*layer.Layer, error) {
	ft, err := r.c.GetFeatureType(ctx, ws, store, name)
	if gs.IsNotFound(err) {
		return nil, layer.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	l := &layer.Layer{Workspace: ws, Store: store, Name: ft.Name, Table: ft.NativeName, Title: ft.Title, SRS: ft.SRS}
	ref, err := r.c.GetLayerDefaultStyle(ctx, ws, name)
	if err != nil && !gs.IsNotFound(err) {
		return nil, err
	}
	if ref != nil {
		l.Style = ref.Name
		if ref.Workspace != "" {
			l.Style = ref.Workspace + ":" + ref.Name
		}
	}
	return l, nil
}

func (r *LayerRepository) List(ctx context.Context, ws, store string) ([]string, error) {
	names, err := r.c.ListFeatureTypes(ctx, ws, store)
	if gs.IsNotFound(err) {
		return nil, nil
	}
	return names, err
}

// Update modifica solo los campos gestionados, conservando el resto
// (abstract, bounding boxes, etc.).
func (r *LayerRepository) Update(ctx context.Context, l layer.Layer) error {
	ft, err := r.c.GetFeatureType(ctx, l.Workspace, l.Store, l.Name)
	if gs.IsNotFound(err) {
		return layer.ErrNotFound
	}
	if err != nil {
		return err
	}
	ft.NativeName, ft.SRS = l.NativeTable(), l.SRS
	if l.Title != "" {
		ft.Title = l.Title
	}
	if err := r.c.UpdateFeatureType(ctx, l.Workspace, l.Store, l.Name, *ft, false); err != nil {
		return err
	}
	return r.setStyle(ctx, l)
}

// setStyle asigna el estilo por defecto si la capa lo declara.
func (r *LayerRepository) setStyle(ctx context.Context, l layer.Layer) error {
	if l.Style == "" {
		return nil
	}
	ref := gs.StyleRef{Name: l.Style}
	if ws, n, ok := strings.Cut(l.Style, ":"); ok {
		ref = gs.StyleRef{Name: n, Workspace: ws}
	}
	return r.c.SetLayerDefaultStyle(ctx, l.Workspace, l.Name, ref)
}

func (r *LayerRepository) Create(ctx context.Context, l layer.Layer) error {
	err := r.c.CreateFeatureType(ctx, l.Workspace, l.Store, gs.FeatureType{
		Name: l.Name, NativeName: l.NativeTable(), Title: l.Title, SRS: l.SRS, Enabled: true,
	})
	if err != nil {
		return err
	}
	return r.setStyle(ctx, l)
}

func (r *LayerRepository) Delete(ctx context.Context, ws, store, name string) error {
	err := r.c.DeleteFeatureType(ctx, ws, store, name, true)
	if gs.IsNotFound(err) {
		return layer.ErrNotFound
	}
	return err
}
