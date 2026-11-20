package geoserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// do ejecuta una request REST y valida el status. Devuelve ErrNotFound en 404.
// Si out != nil, decodifica el cuerpo JSON de la respuesta.
func (c *Client) do(ctx context.Context, method, path string, in, out any, okStatus ...int) error {
	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("geoserver: error serializando %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("geoserver: error creando request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("geoserver: error en request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	for _, s := range okStatus {
		if resp.StatusCode == s {
			if out == nil {
				return nil
			}
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("geoserver: error parseando respuesta de %s %s: %w", method, path, err)
			}
			return nil
		}
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("geoserver: %s %s devolvió %d: %s", method, path, resp.StatusCode, string(b))
}
