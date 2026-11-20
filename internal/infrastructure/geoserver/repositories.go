// Package geoserver adapts the GeoServer REST client (internal/geoserver) to
// the domain repositories.
package geoserver

import (
	"context"
	"strconv"

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
	return r.c.CreateDataStore(ctx, d.Workspace, gs.DataStore{
		Name:    d.Name,
		Type:    "PostGIS",
		Enabled: true,
		ConnectionParameters: map[string]string{
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
	return &layer.Layer{Workspace: ws, Store: store, Name: ft.Name, Table: ft.NativeName, Title: ft.Title, SRS: ft.SRS}, nil
}

func (r *LayerRepository) Create(ctx context.Context, l layer.Layer) error {
	return r.c.CreateFeatureType(ctx, l.Workspace, l.Store, gs.FeatureType{
		Name: l.Name, NativeName: l.NativeTable(), Title: l.Title, SRS: l.SRS, Enabled: true,
	})
}

func (r *LayerRepository) Delete(ctx context.Context, ws, store, name string) error {
	err := r.c.DeleteFeatureType(ctx, ws, store, name, true)
	if gs.IsNotFound(err) {
		return layer.ErrNotFound
	}
	return err
}
