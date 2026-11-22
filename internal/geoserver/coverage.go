package geoserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Coverage es una capa raster publicada desde un coverage store.
type Coverage struct {
	Name       string `json:"name"`
	NativeName string `json:"nativeName,omitempty"`
	// NativeCoverageName es el nombre de la cobertura dentro del fichero.
	NativeCoverageName string `json:"nativeCoverageName,omitempty"`
	Title              string `json:"title,omitempty"`
	// Abstract se envía siempre para poder vaciarlo.
	Abstract   string       `json:"abstract"`
	SRS        string       `json:"srs,omitempty"`
	Enabled    bool         `json:"enabled"`
	NativeBBox *BoundingBox `json:"nativeBoundingBox,omitempty"`
	LatLonBBox *BoundingBox `json:"latLonBoundingBox,omitempty"`
}

type coverageEnvelope struct {
	Coverage Coverage `json:"coverage"`
}

func coveragesPath(workspace, store string) string {
	return coverageStorePath(workspace, store) + "/coverages"
}

func coveragePath(workspace, store, name string) string {
	return coveragesPath(workspace, store) + "/" + url.PathEscape(name)
}

// GetCoverage devuelve una coverage; el error satisface IsNotFound si ella, su
// store o su workspace no existen.
func (c *Client) GetCoverage(ctx context.Context, workspace, store, name string) (*Coverage, error) {
	var env coverageEnvelope
	if err := c.do(ctx, http.MethodGet, coveragePath(workspace, store, name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	return &env.Coverage, nil
}

// CreateCoverage publica una coverage de store, lo que crea también su capa.
func (c *Client) CreateCoverage(ctx context.Context, workspace, store string, cv Coverage) error {
	return c.do(ctx, http.MethodPost, coveragesPath(workspace, store), coverageEnvelope{Coverage: cv}, nil, http.StatusCreated)
}

// UpdateCoverage modifica la coverage name; solo cambian los campos presentes.
func (c *Client) UpdateCoverage(ctx context.Context, workspace, store, name string, cv Coverage) error {
	return c.do(ctx, http.MethodPut, coveragePath(workspace, store, name), coverageEnvelope{Coverage: cv}, nil, http.StatusOK)
}

// DeleteCoverage despublica una coverage. Con recurse borra también su capa.
func (c *Client) DeleteCoverage(ctx context.Context, workspace, store, name string, recurse bool) error {
	path := coveragePath(workspace, store, name) + "?recurse=false"
	if recurse {
		path = coveragePath(workspace, store, name) + "?recurse=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil, http.StatusOK)
}

// ListCoverages devuelve los nombres de las coverages publicadas desde store.
func (c *Client) ListCoverages(ctx context.Context, workspace, store string) ([]string, error) {
	var resp struct {
		Coverages json.RawMessage `json:"coverages"`
	}
	if err := c.do(ctx, http.MethodGet, coveragesPath(workspace, store)+".json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	return decodeNameList(resp.Coverages, "coverage")
}
