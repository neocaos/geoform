package geoserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound se devuelve cuando GeoServer responde 404 para un recurso.
var ErrNotFound = errors.New("geoserver: recurso no encontrado")

// IsNotFound indica si el error corresponde a un recurso inexistente (404).
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// Client es un cliente REST delgado para la API de GeoServer.
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// NewClient crea un cliente apuntando a baseURL, ej:
// "http://localhost:8080/geoserver/rest"
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Workspace representa el recurso workspace de GeoServer.
type Workspace struct {
	Name     string `json:"name"`
	Isolated *bool  `json:"isolated,omitempty"`
}

type workspaceEnvelope struct {
	Workspace Workspace `json:"workspace"`
}

type createWorkspaceRequest struct {
	Workspace struct {
		Name string `json:"name"`
	} `json:"workspace"`
}

// GetWorkspace obtiene un workspace por nombre.
// Devuelve ErrNotFound si no existe (404).
func (c *Client) GetWorkspace(ctx context.Context, name string) (*Workspace, error) {
	u := fmt.Sprintf("%s/workspaces/%s.json?quietOnNotFound=true", c.baseURL, url.PathEscape(name))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("geoserver: error creando request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geoserver: error en request GET workspace %q: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("geoserver: GET workspace %q devolvió %d: %s", name, resp.StatusCode, string(body))
	}

	var env workspaceEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("geoserver: error parseando respuesta de workspace %q: %w", name, err)
	}

	return &env.Workspace, nil
}

// CreateWorkspace crea un nuevo workspace con el nombre dado.
func (c *Client) CreateWorkspace(ctx context.Context, name string) error {
	body := createWorkspaceRequest{}
	body.Workspace.Name = name

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("geoserver: error serializando workspace %q: %w", name, err)
	}

	u := fmt.Sprintf("%s/workspaces", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("geoserver: error creando request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("geoserver: error en request POST workspace %q: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("geoserver: POST workspace %q devolvió %d: %s", name, resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteWorkspace elimina un workspace. Si recurse es true, elimina también
// todo su contenido (datastores, coveragestores, etc.).
func (c *Client) DeleteWorkspace(ctx context.Context, name string, recurse bool) error {
	u := fmt.Sprintf("%s/workspaces/%s?recurse=%t", c.baseURL, url.PathEscape(name), recurse)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("geoserver: error creando request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("geoserver: error en request DELETE workspace %q: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("geoserver: DELETE workspace %q devolvió %d: %s", name, resp.StatusCode, string(body))
	}

	return nil
}