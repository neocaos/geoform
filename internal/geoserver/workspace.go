package geoserver

import (
	"context"
	"net/http"
	"net/url"
)

// Workspace is a GeoServer workspace.
type Workspace struct {
	Name     string `json:"name"`
	Isolated bool   `json:"isolated"`
}

type workspaceEnvelope struct {
	Workspace Workspace `json:"workspace"`
}

func workspacePath(name string) string {
	return "/workspaces/" + url.PathEscape(name)
}

// GetWorkspace fetches a workspace by name. It returns an error satisfying
// IsNotFound if the workspace does not exist.
func (c *Client) GetWorkspace(ctx context.Context, name string) (*Workspace, error) {
	// The explicit .json extension keeps names containing dots from being
	// parsed as a format suffix by GeoServer.
	var env workspaceEnvelope
	if err := c.do(ctx, http.MethodGet, workspacePath(name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	return &env.Workspace, nil
}

// CreateWorkspace creates a workspace.
func (c *Client) CreateWorkspace(ctx context.Context, ws Workspace) error {
	return c.do(ctx, http.MethodPost, "/workspaces", workspaceEnvelope{Workspace: ws}, nil, http.StatusCreated)
}

// UpdateWorkspace replaces the workspace currently called name with ws.
// Setting ws.Name to a different value renames the workspace.
func (c *Client) UpdateWorkspace(ctx context.Context, name string, ws Workspace) error {
	return c.do(ctx, http.MethodPut, workspacePath(name), workspaceEnvelope{Workspace: ws}, nil, http.StatusOK)
}

// DeleteWorkspace deletes a workspace. With recurse, everything it contains
// (stores, layers, styles) is deleted too; without it, GeoServer refuses to
// delete a non-empty workspace.
func (c *Client) DeleteWorkspace(ctx context.Context, name string, recurse bool) error {
	path := workspacePath(name) + "?recurse=false"
	if recurse {
		path = workspacePath(name) + "?recurse=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil, http.StatusOK)
}
