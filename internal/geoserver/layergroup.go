package geoserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// LayerGroup is a workspace layer group. Layers are published layer names
// ("workspace:name"), in drawing order.
type LayerGroup struct {
	Name   string
	Title  string
	Mode   string
	Layers []string
}

type layerGroupWire struct {
	Name      string `json:"name"`
	Title     string `json:"title"` // always sent, so it can be cleared
	Mode      string `json:"mode,omitempty"`
	Workspace *struct {
		Name string `json:"name"`
	} `json:"workspace,omitempty"`
	Publishables struct {
		Published json.RawMessage `json:"published"`
	} `json:"publishables"`
	// GeoServer exige un estilo por capa al actualizar el grupo.
	// Se lee como crudo: en las respuestas un solo estilo llega como objeto.
	Styles json.RawMessage `json:"styles,omitempty"`
}

type layerGroupEnvelope struct {
	LayerGroup layerGroupWire `json:"layerGroup"`
}

func layerGroupsPath(workspace string) string { return workspacePath(workspace) + "/layergroups" }

func layerGroupPath(workspace, name string) string {
	return layerGroupsPath(workspace) + "/" + url.PathEscape(name)
}

func (c *Client) envelope(ctx context.Context, workspace string, g LayerGroup) (layerGroupEnvelope, error) {
	env, err := g.envelope(workspace)
	if err != nil {
		return env, err
	}
	// Se fija el estilo por defecto actual de cada capa miembro. Los estilos de
	// workspace se refieren como "ws:estilo" (name y workspace aparte no los
	// acepta el grupo).
	styles := make([]StyleRef, 0, len(g.Layers))
	for _, l := range g.Layers {
		ws, name, ok := strings.Cut(l, ":")
		if !ok {
			ws, name = workspace, l
		}
		ref, err := c.GetLayerDefaultStyle(ctx, ws, name)
		if err != nil {
			return env, err
		}
		if ref == nil {
			return env, fmt.Errorf("geoserver: la capa %q no tiene estilo por defecto", l)
		}
		if ref.Workspace != "" {
			ref = &StyleRef{Name: ref.Workspace + ":" + ref.Name}
		}
		styles = append(styles, *ref)
	}
	raw, err := json.Marshal(struct {
		Style []StyleRef `json:"style"`
	}{styles})
	env.LayerGroup.Styles = raw
	return env, err
}

func (g LayerGroup) envelope(workspace string) (layerGroupEnvelope, error) {
	type pub struct {
		Type string `json:"@type"`
		Name string `json:"name"`
	}
	pubs := make([]pub, 0, len(g.Layers))
	for _, l := range g.Layers {
		pubs = append(pubs, pub{Type: "layer", Name: l})
	}
	raw, err := json.Marshal(pubs)
	if err != nil {
		return layerGroupEnvelope{}, err
	}
	var w layerGroupWire
	w.Name, w.Title, w.Mode = g.Name, g.Title, g.Mode
	w.Workspace = &struct {
		Name string `json:"name"`
	}{workspace}
	w.Publishables.Published = raw
	return layerGroupEnvelope{LayerGroup: w}, nil
}

// GetLayerGroup fetches a workspace layer group. It returns an error
// satisfying IsNotFound if it or its workspace does not exist.
func (c *Client) GetLayerGroup(ctx context.Context, workspace, name string) (*LayerGroup, error) {
	var env layerGroupEnvelope
	if err := c.do(ctx, http.MethodGet, layerGroupPath(workspace, name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	g := &LayerGroup{Name: env.LayerGroup.Name, Title: env.LayerGroup.Title, Mode: env.LayerGroup.Mode}
	// "published" llega como objeto si hay un solo elemento, y "" si no hay.
	raw := env.LayerGroup.Publishables.Published
	if len(raw) > 0 && raw[0] == '{' {
		raw = append(append([]byte("["), raw...), ']')
	}
	if len(raw) > 0 && raw[0] == '[' {
		var items []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			g.Layers = append(g.Layers, it.Name)
		}
	}
	return g, nil
}

// CreateLayerGroup creates a layer group in workspace. Bounds are computed by
// GeoServer from the member layers.
func (c *Client) CreateLayerGroup(ctx context.Context, workspace string, g LayerGroup) error {
	env, err := c.envelope(ctx, workspace, g)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, layerGroupsPath(workspace), env, nil, http.StatusCreated)
}

// UpdateLayerGroup replaces the members, title and mode of the group called name.
func (c *Client) UpdateLayerGroup(ctx context.Context, workspace, name string, g LayerGroup) error {
	env, err := c.envelope(ctx, workspace, g)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPut, layerGroupPath(workspace, name), env, nil, http.StatusOK)
}

// DeleteLayerGroup deletes a workspace layer group.
func (c *Client) DeleteLayerGroup(ctx context.Context, workspace, name string) error {
	return c.do(ctx, http.MethodDelete, layerGroupPath(workspace, name), nil, nil, http.StatusOK)
}

// ListLayerGroups returns the names of the layer groups of a workspace.
func (c *Client) ListLayerGroups(ctx context.Context, workspace string) ([]string, error) {
	var resp struct {
		LayerGroups json.RawMessage `json:"layerGroups"`
	}
	if err := c.do(ctx, http.MethodGet, layerGroupsPath(workspace)+".json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	return decodeNameList(resp.LayerGroups, "layerGroup")
}
