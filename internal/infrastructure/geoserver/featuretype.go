package geoserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// FeatureType es una capa vectorial publicada desde un datastore.
type FeatureType struct {
	Name       string `json:"name"`
	NativeName string `json:"nativeName,omitempty"`
	Title      string `json:"title,omitempty"`
	SRS        string `json:"srs,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type featureTypeEnvelope struct {
	FeatureType FeatureType `json:"featureType"`
}

func ftPath(workspace, store, name string) string {
	return fmt.Sprintf("/workspaces/%s/datastores/%s/featuretypes/%s",
		url.PathEscape(workspace), url.PathEscape(store), url.PathEscape(name))
}

// CreateFeatureType publica una tabla del datastore como capa.
func (c *Client) CreateFeatureType(ctx context.Context, workspace, store string, ft FeatureType) error {
	ft.Enabled = true
	path := fmt.Sprintf("/workspaces/%s/datastores/%s/featuretypes", url.PathEscape(workspace), url.PathEscape(store))
	return c.do(ctx, http.MethodPost, path, featureTypeEnvelope{FeatureType: ft}, nil, http.StatusCreated)
}

// GetFeatureType obtiene una capa publicada. Devuelve ErrNotFound si no existe.
func (c *Client) GetFeatureType(ctx context.Context, workspace, store, name string) (*FeatureType, error) {
	var env featureTypeEnvelope
	if err := c.do(ctx, http.MethodGet, ftPath(workspace, store, name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	return &env.FeatureType, nil
}

// DeleteFeatureType elimina la capa; recurse borra también el layer asociado.
func (c *Client) DeleteFeatureType(ctx context.Context, workspace, store, name string, recurse bool) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("%s?recurse=%t", ftPath(workspace, store, name), recurse), nil, nil, http.StatusOK)
}

// LayerExists indica si "workspace:name" existe como layer publicado
// (endpoint /layers, independiente del featuretype).
func (c *Client) LayerExists(ctx context.Context, workspace, name string) (bool, error) {
	path := fmt.Sprintf("/layers/%s:%s.json?quietOnNotFound=true", url.PathEscape(workspace), url.PathEscape(name))
	err := c.do(ctx, http.MethodGet, path, nil, nil, http.StatusOK)
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}
