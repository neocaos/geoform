package geoserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// DataStore representa un datastore de GeoServer. Params son los
// connectionParameters (host, port, database, ...), ya aplanados.
type DataStore struct {
	Name    string
	Type    string
	Enabled bool
	Params  map[string]string
}

type entry struct {
	Key   string `json:"@key"`
	Value string `json:"$"`
}

type dataStoreBody struct {
	Name                 string `json:"name"`
	Type                 string `json:"type,omitempty"`
	Enabled              bool   `json:"enabled"`
	ConnectionParameters struct {
		Entry []entry `json:"entry"`
	} `json:"connectionParameters"`
}

type dataStoreEnvelope struct {
	DataStore dataStoreBody `json:"dataStore"`
}

func dsPath(workspace, name string) string {
	return fmt.Sprintf("/workspaces/%s/datastores/%s", url.PathEscape(workspace), url.PathEscape(name))
}

// CreateDataStore crea un datastore en el workspace.
func (c *Client) CreateDataStore(ctx context.Context, workspace string, ds DataStore) error {
	b := dataStoreBody{Name: ds.Name, Type: ds.Type, Enabled: true}
	for k, v := range ds.Params {
		b.ConnectionParameters.Entry = append(b.ConnectionParameters.Entry, entry{Key: k, Value: v})
	}
	path := fmt.Sprintf("/workspaces/%s/datastores", url.PathEscape(workspace))
	return c.do(ctx, http.MethodPost, path, dataStoreEnvelope{DataStore: b}, nil, http.StatusCreated)
}

// GetDataStore obtiene un datastore. Devuelve ErrNotFound si no existe.
func (c *Client) GetDataStore(ctx context.Context, workspace, name string) (*DataStore, error) {
	var env dataStoreEnvelope
	path := dsPath(workspace, name) + ".json?quietOnNotFound=true"
	if err := c.do(ctx, http.MethodGet, path, nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	ds := &DataStore{
		Name:    env.DataStore.Name,
		Type:    env.DataStore.Type,
		Enabled: env.DataStore.Enabled,
		Params:  map[string]string{},
	}
	for _, e := range env.DataStore.ConnectionParameters.Entry {
		ds.Params[e.Key] = e.Value
	}
	return ds, nil
}

// DeleteDataStore elimina un datastore; recurse borra también sus capas.
func (c *Client) DeleteDataStore(ctx context.Context, workspace, name string, recurse bool) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("%s?recurse=%t", dsPath(workspace, name), recurse), nil, nil, http.StatusOK)
}
