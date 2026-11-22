package geoserver

import (
	"context"
	"encoding/json"
	"net/http"
)

// ListWorkspaces devuelve los nombres de todos los workspaces.
func (c *Client) ListWorkspaces(ctx context.Context) ([]string, error) {
	var resp struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if err := c.do(ctx, http.MethodGet, "/workspaces.json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	return decodeNameList(resp.Workspaces, "workspace")
}
