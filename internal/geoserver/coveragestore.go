package geoserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Tipos de coverage store (raster) soportados.
const CoverageStoreTypeGeoTIFF = "GeoTIFF"

// CoverageStore es un origen de datos raster (p. ej. un GeoTIFF).
type CoverageStore struct {
	Name string `json:"name"`
	// Description se envía siempre para poder vaciarla.
	Description string `json:"description"`
	Type        string `json:"type,omitempty"`
	Enabled     bool   `json:"enabled"`
	// URL del fichero, p. ej. "file:/data/dem.tif".
	URL string `json:"url,omitempty"`
	// Workspace es obligatorio al crear (GeoServer no lo deduce de la ruta);
	// en lecturas llega como objeto {"name": ..., "href": ...}.
	Workspace *WorkspaceRef `json:"workspace,omitempty"`
}

// WorkspaceRef referencia a un workspace por nombre.
type WorkspaceRef struct {
	Name string `json:"name"`
}

type coverageStoreEnvelope struct {
	CoverageStore CoverageStore `json:"coverageStore"`
}

func coverageStoresPath(workspace string) string {
	return workspacePath(workspace) + "/coveragestores"
}

func coverageStorePath(workspace, name string) string {
	return coverageStoresPath(workspace) + "/" + url.PathEscape(name)
}

// GetCoverageStore devuelve un coverage store; el error satisface IsNotFound
// si no existe.
func (c *Client) GetCoverageStore(ctx context.Context, workspace, name string) (*CoverageStore, error) {
	var env coverageStoreEnvelope
	if err := c.do(ctx, http.MethodGet, coverageStorePath(workspace, name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	return &env.CoverageStore, nil
}

// CreateCoverageStore crea un coverage store en workspace.
func (c *Client) CreateCoverageStore(ctx context.Context, workspace string, cs CoverageStore) error {
	cs.Workspace = &WorkspaceRef{Name: workspace}
	return c.do(ctx, http.MethodPost, coverageStoresPath(workspace), coverageStoreEnvelope{CoverageStore: cs}, nil, http.StatusCreated)
}

// UpdateCoverageStore modifica el coverage store name.
func (c *Client) UpdateCoverageStore(ctx context.Context, workspace, name string, cs CoverageStore) error {
	return c.do(ctx, http.MethodPut, coverageStorePath(workspace, name), coverageStoreEnvelope{CoverageStore: cs}, nil, http.StatusOK)
}

// DeleteCoverageStore borra un coverage store. Con recurse borra también sus
// coverages; sin él GeoServer se niega si tiene alguno. Nunca se purga el
// fichero del disco (purge=none).
func (c *Client) DeleteCoverageStore(ctx context.Context, workspace, name string, recurse bool) error {
	path := coverageStorePath(workspace, name) + "?purge=none&recurse=false"
	if recurse {
		path = coverageStorePath(workspace, name) + "?purge=none&recurse=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil, http.StatusOK)
}

// ListCoverageStores devuelve los nombres de los coverage stores de workspace.
func (c *Client) ListCoverageStores(ctx context.Context, workspace string) ([]string, error) {
	var resp struct {
		CoverageStores json.RawMessage `json:"coverageStores"`
	}
	if err := c.do(ctx, http.MethodGet, coverageStoresPath(workspace)+".json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	return decodeNameList(resp.CoverageStores, "coverageStore")
}
