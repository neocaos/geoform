package geoserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Content types accepted by GeoServer for SLD uploads.
const (
	sldContentType = "application/vnd.ogc.sld+xml" // SLD 1.0.0
	seContentType  = "application/vnd.ogc.se+xml"  // SLD 1.1.0 / Symbology Encoding
)

func stylesPath(workspace string) string { return workspacePath(workspace) + "/styles" }

func stylePath(workspace, name string) string {
	return stylesPath(workspace) + "/" + url.PathEscape(name)
}

// sldMediaType elige el content type según la versión declarada en el SLD.
func sldMediaType(sld string) string {
	if strings.Contains(sld, `version="1.1.0"`) || strings.Contains(sld, `version='1.1.0'`) {
		return seContentType
	}
	return sldContentType
}

// doRaw envía body tal cual (no JSON) y devuelve el cuerpo de la respuesta.
func (c *Client) doRaw(ctx context.Context, method, path, contentType, accept string, body []byte, wantStatus int) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.restURL+path, rd)
	if err != nil {
		return nil, fmt.Errorf("geoserver: building %s %s request: %w", method, path, err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("User-Agent", c.userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geoserver: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("geoserver: reading %s %s response: %w", method, path, err)
	}
	return out, nil
}

// GetStyleSLD downloads the SLD of a workspace style. It returns an error
// satisfying IsNotFound if the style or its workspace does not exist.
func (c *Client) GetStyleSLD(ctx context.Context, workspace, name string) (string, error) {
	out, err := c.doRaw(ctx, http.MethodGet, stylePath(workspace, name)+".sld?quietOnNotFound=true", "", "application/vnd.ogc.sld+xml, application/xml", nil, http.StatusOK)
	return string(out), err
}

// CreateStyle uploads sld as a new style called name in workspace.
func (c *Client) CreateStyle(ctx context.Context, workspace, name, sld string) error {
	_, err := c.doRaw(ctx, http.MethodPost, stylesPath(workspace)+"?name="+url.QueryEscape(name),
		sldMediaType(sld), "application/json", []byte(sld), http.StatusCreated)
	return err
}

// UpdateStyle replaces the SLD of an existing workspace style.
func (c *Client) UpdateStyle(ctx context.Context, workspace, name, sld string) error {
	_, err := c.doRaw(ctx, http.MethodPut, stylePath(workspace, name),
		sldMediaType(sld), "application/json", []byte(sld), http.StatusOK)
	return err
}

// DeleteStyle deletes a workspace style and its SLD file. GeoServer refuses to
// delete a style still used by a layer unless recurse is set, in which case the
// references are removed.
func (c *Client) DeleteStyle(ctx context.Context, workspace, name string, recurse bool) error {
	path := stylePath(workspace, name) + "?purge=true&recurse=false"
	if recurse {
		path = stylePath(workspace, name) + "?purge=true&recurse=true"
	}
	_, err := c.doRaw(ctx, http.MethodDelete, path, "", "application/json", nil, http.StatusOK)
	return err
}

// ListStyles returns the names of the styles of a workspace. It returns an
// error satisfying IsNotFound if the workspace does not exist.
func (c *Client) ListStyles(ctx context.Context, workspace string) ([]string, error) {
	var resp struct {
		Styles json.RawMessage `json:"styles"`
	}
	if err := c.do(ctx, http.MethodGet, stylesPath(workspace)+".json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	return decodeNameList(resp.Styles, "style")
}

// StyleRef references a style; Workspace is empty for global styles.
type StyleRef struct {
	Name      string `json:"name"`
	Workspace string `json:"workspace,omitempty"`
}

type layerEnvelope struct {
	Layer struct {
		DefaultStyle *StyleRef `json:"defaultStyle,omitempty"`
	} `json:"layer"`
}

func layerPath(workspace, name string) string {
	return "/layers/" + url.PathEscape(workspace) + ":" + url.PathEscape(name)
}

// GetLayerDefaultStyle returns the default style of layer "workspace:name", or
// nil if it has none. It returns an error satisfying IsNotFound if the layer
// does not exist.
func (c *Client) GetLayerDefaultStyle(ctx context.Context, workspace, name string) (*StyleRef, error) {
	var env layerEnvelope
	if err := c.do(ctx, http.MethodGet, layerPath(workspace, name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	ref := env.Layer.DefaultStyle
	if ref == nil {
		return nil, nil
	}
	// Algunas versiones devuelven "ws:estilo" en name; se normaliza.
	if ws, n, ok := strings.Cut(ref.Name, ":"); ok {
		return &StyleRef{Name: n, Workspace: ws}, nil
	}
	return ref, nil
}

// SetLayerDefaultStyle assigns the default style of layer "workspace:name".
func (c *Client) SetLayerDefaultStyle(ctx context.Context, workspace, name string, ref StyleRef) error {
	var env layerEnvelope
	env.Layer.DefaultStyle = &ref
	return c.do(ctx, http.MethodPut, layerPath(workspace, name), env, nil, http.StatusOK)
}
