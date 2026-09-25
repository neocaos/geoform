package geoserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
)

// DataStore is a GeoServer vector data store.
type DataStore struct {
	Name                 string               `json:"name"`
	Description          string               `json:"description"` // always sent, so it can be cleared
	Type                 string               `json:"type,omitempty"`
	Enabled              bool                 `json:"enabled"`
	ConnectionParameters ConnectionParameters `json:"connectionParameters"`
}

// ConnectionParameters are a store's connection settings. On the wire
// GeoServer encodes them as {"entry": [{"@key": k, "$": v}, ...]}, and
// collapses a single entry into an object instead of a one-element array.
type ConnectionParameters map[string]string

type connectionEntry struct {
	Key   string `json:"@key"`
	Value string `json:"$"`
}

// MarshalJSON encodes the parameters in GeoServer's entry format, sorted by
// key so request bodies are deterministic.
func (p ConnectionParameters) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]connectionEntry, 0, len(keys))
	for _, k := range keys {
		entries = append(entries, connectionEntry{Key: k, Value: p[k]})
	}
	return json.Marshal(struct {
		Entry []connectionEntry `json:"entry"`
	}{entries})
}

// UnmarshalJSON decodes GeoServer's entry format, accepting both an array of
// entries and a single entry object.
func (p *ConnectionParameters) UnmarshalJSON(data []byte) error {
	var raw struct {
		Entry json.RawMessage `json:"entry"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var entries []connectionEntry
	if len(raw.Entry) > 0 && raw.Entry[0] == '{' {
		var single connectionEntry
		if err := json.Unmarshal(raw.Entry, &single); err != nil {
			return err
		}
		entries = []connectionEntry{single}
	} else if len(raw.Entry) > 0 {
		if err := json.Unmarshal(raw.Entry, &entries); err != nil {
			return err
		}
	}
	*p = make(ConnectionParameters, len(entries))
	for _, e := range entries {
		(*p)[e.Key] = e.Value
	}
	return nil
}

type dataStoreEnvelope struct {
	DataStore DataStore `json:"dataStore"`
}

func dataStoresPath(workspace string) string {
	return workspacePath(workspace) + "/datastores"
}

func dataStorePath(workspace, name string) string {
	return dataStoresPath(workspace) + "/" + url.PathEscape(name)
}

// GetDataStore fetches a data store. It returns an error satisfying
// IsNotFound if the store or its workspace does not exist.
func (c *Client) GetDataStore(ctx context.Context, workspace, name string) (*DataStore, error) {
	var env dataStoreEnvelope
	if err := c.do(ctx, http.MethodGet, dataStorePath(workspace, name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	return &env.DataStore, nil
}

// CreateDataStore creates a data store in workspace.
func (c *Client) CreateDataStore(ctx context.Context, workspace string, ds DataStore) error {
	return c.do(ctx, http.MethodPost, dataStoresPath(workspace), dataStoreEnvelope{DataStore: ds}, nil, http.StatusCreated)
}

// UpdateDataStore replaces the data store called name in workspace with ds.
func (c *Client) UpdateDataStore(ctx context.Context, workspace, name string, ds DataStore) error {
	return c.do(ctx, http.MethodPut, dataStorePath(workspace, name), dataStoreEnvelope{DataStore: ds}, nil, http.StatusOK)
}

// DeleteDataStore deletes a data store. With recurse, its layers are deleted
// too; without it, GeoServer refuses to delete a store that has layers.
func (c *Client) DeleteDataStore(ctx context.Context, workspace, name string, recurse bool) error {
	path := dataStorePath(workspace, name) + "?recurse=false"
	if recurse {
		path = dataStorePath(workspace, name) + "?recurse=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil, http.StatusOK)
}

// ListAvailableFeatureTypes lists the tables of a data store that are not yet
// published as layers. GeoServer has to open a connection to answer, so this
// also verifies the store's connection parameters.
func (c *Client) ListAvailableFeatureTypes(ctx context.Context, workspace, name string) ([]string, error) {
	// GeoServer answers {"list":""} when empty, {"list":{"string":"a"}} for
	// one name and {"list":{"string":["a","b"]}} for several.
	var resp struct {
		List json.RawMessage `json:"list"`
	}
	if err := c.do(ctx, http.MethodGet, dataStorePath(workspace, name)+"/featuretypes.json?list=available", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	var list struct {
		String json.RawMessage `json:"string"`
	}
	if len(resp.List) == 0 || resp.List[0] != '{' {
		return nil, nil
	}
	if err := json.Unmarshal(resp.List, &list); err != nil {
		return nil, err
	}
	var names []string
	if len(list.String) > 0 && list.String[0] == '"' {
		var one string
		if err := json.Unmarshal(list.String, &one); err != nil {
			return nil, err
		}
		return []string{one}, nil
	}
	if len(list.String) > 0 {
		if err := json.Unmarshal(list.String, &names); err != nil {
			return nil, err
		}
	}
	return names, nil
}
