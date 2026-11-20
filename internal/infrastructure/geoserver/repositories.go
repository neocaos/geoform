package geoserver

import (
	"context"
	"strconv"

	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
)

// WorkspaceRepository adapta Client a workspace.Repository.
type WorkspaceRepository struct{ c *Client }

func NewWorkspaceRepository(c *Client) *WorkspaceRepository { return &WorkspaceRepository{c} }

func (r *WorkspaceRepository) Get(ctx context.Context, name string) (*workspace.Workspace, error) {
	ws, err := r.c.GetWorkspace(ctx, name)
	if IsNotFound(err) {
		return nil, workspace.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w := &workspace.Workspace{Name: ws.Name}
	if ws.Isolated != nil {
		w.Isolated = *ws.Isolated
	}
	return w, nil
}

func (r *WorkspaceRepository) Create(ctx context.Context, w workspace.Workspace) error {
	return r.c.CreateWorkspace(ctx, w.Name)
}

func (r *WorkspaceRepository) Delete(ctx context.Context, name string, recurse bool) error {
	err := r.c.DeleteWorkspace(ctx, name, recurse)
	if IsNotFound(err) {
		return workspace.ErrNotFound
	}
	return err
}

// DatastoreRepository adapta Client a datastore.Repository (PostGIS).
type DatastoreRepository struct{ c *Client }

func NewDatastoreRepository(c *Client) *DatastoreRepository { return &DatastoreRepository{c} }

func (r *DatastoreRepository) Get(ctx context.Context, ws, name string) (*datastore.Datastore, error) {
	ds, err := r.c.GetDataStore(ctx, ws, name)
	if IsNotFound(err) {
		return nil, datastore.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(ds.Params["port"])
	return &datastore.Datastore{
		Workspace: ws,
		Name:      ds.Name,
		PostGIS: datastore.PostGIS{
			Host:     ds.Params["host"],
			Port:     port,
			Database: ds.Params["database"],
			Schema:   ds.Params["schema"],
			User:     ds.Params["user"],
			// GeoServer enmascara el password al leerlo; no se devuelve.
		},
	}, nil
}

func (r *DatastoreRepository) Create(ctx context.Context, d datastore.Datastore) error {
	p := d.PostGIS
	port := p.Port
	if port == 0 {
		port = 5432
	}
	schema := p.Schema
	if schema == "" {
		schema = "public"
	}
	return r.c.CreateDataStore(ctx, d.Workspace, DataStore{
		Name: d.Name,
		Type: "PostGIS",
		Params: map[string]string{
			"dbtype":              "postgis",
			"host":                p.Host,
			"port":                strconv.Itoa(port),
			"database":            p.Database,
			"schema":              schema,
			"user":                p.User,
			"passwd":              p.Password,
			"Expose primary keys": "true",
		},
	})
}

func (r *DatastoreRepository) Delete(ctx context.Context, ws, name string, recurse bool) error {
	err := r.c.DeleteDataStore(ctx, ws, name, recurse)
	if IsNotFound(err) {
		return datastore.ErrNotFound
	}
	return err
}

// LayerRepository adapta Client a layer.Repository.
type LayerRepository struct{ c *Client }

func NewLayerRepository(c *Client) *LayerRepository { return &LayerRepository{c} }

func (r *LayerRepository) Get(ctx context.Context, ws, store, name string) (*layer.Layer, error) {
	ft, err := r.c.GetFeatureType(ctx, ws, store, name)
	if IsNotFound(err) {
		return nil, layer.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &layer.Layer{Workspace: ws, Store: store, Name: ft.Name, Table: ft.NativeName, Title: ft.Title, SRS: ft.SRS}, nil
}

func (r *LayerRepository) Create(ctx context.Context, l layer.Layer) error {
	return r.c.CreateFeatureType(ctx, l.Workspace, l.Store, FeatureType{
		Name: l.Name, NativeName: l.NativeTable(), Title: l.Title, SRS: l.SRS,
	})
}

func (r *LayerRepository) Delete(ctx context.Context, ws, store, name string) error {
	err := r.c.DeleteFeatureType(ctx, ws, store, name, true)
	if IsNotFound(err) {
		return layer.ErrNotFound
	}
	return err
}
