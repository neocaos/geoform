// Package geoserver is a thin client for the GeoServer REST API.
package geoserver

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxErrorBody caps how much of an error response body is kept in an APIError.
const maxErrorBody = 4 << 10

// APIError is returned when GeoServer answers with an unexpected status code.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("geoserver: %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// redacted marca el lugar de un secreto eliminado de un mensaje.
const redacted = "[REDACTED]"

// redact sustituye cada secreto (no vacío) que aparezca en s.
func redact(s string, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, redacted)
		}
	}
	return s
}

// secretKeys son las claves JSON cuyo valor se considera secreto.
var secretKeys = map[string]bool{"password": true, "passwd": true, "pass": true}

// collectSecrets recorre un JSON y devuelve los valores de las claves secretas,
// incluido el formato de GeoServer {"@key":"passwd","$":"valor"}.
func collectSecrets(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		if k, _ := t["@key"].(string); secretKeys[strings.ToLower(k)] {
			if val, ok := t["$"].(string); ok {
				*out = append(*out, val)
			}
		}
		for k, val := range t {
			if str, ok := val.(string); ok && secretKeys[strings.ToLower(k)] {
				*out = append(*out, str)
				continue
			}
			collectSecrets(val, out)
		}
	case []any:
		for _, val := range t {
			collectSecrets(val, out)
		}
	}
}

// IsNotFound reports whether err is a GeoServer 404 response.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// Client talks to a single GeoServer instance.
type Client struct {
	restURL    string
	username   string
	password   string
	userAgent  string
	httpClient *http.Client
}

// WithTLSConfig fija la configuración TLS (CA propia, etc.). Solo debe usarse
// de forma explícita.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *Client) {
		c.httpClient.Transport = &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: cfg}
	}
}

// Option customizes a Client.
type Option func(*Client)

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// NewClient creates a client for the GeoServer at baseURL, e.g.
// "http://localhost:8080/geoserver". A trailing "/rest" is accepted too.
func NewClient(baseURL, username, password string, opts ...Option) *Client {
	base := strings.TrimRight(baseURL, "/")
	base = strings.TrimSuffix(base, "/rest")
	c := &Client{
		restURL:    base + "/rest",
		username:   username,
		password:   password,
		userAgent:  "geoform",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// do sends a request to path (relative to /rest). If in is non-nil it is sent
// as a JSON body; if out is non-nil the response body is decoded into it.
// Any status other than wantStatus is returned as an *APIError.
func (c *Client) do(ctx context.Context, method, path string, in, out any, wantStatus int) error {
	var body io.Reader
	// Secretos que no deben aparecer en errores: la contraseña REST y las
	// contraseñas enviadas en el cuerpo (GeoServer puede devolverlas en su error).
	// También la credencial Basic codificada: un eco de la cabecera Authorization
	// la dejaría reversible.
	secrets := []string{c.password, base64.StdEncoding.EncodeToString([]byte(c.username + ":" + c.password))}
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("geoserver: encoding %s %s request: %w", method, path, err)
		}
		var generic any
		if json.Unmarshal(payload, &generic) == nil {
			collectSecrets(generic, &secrets)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.restURL+path, body)
	if err != nil {
		return fmt.Errorf("geoserver: building %s %s request: %w", method, path, err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("geoserver: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: redact(strings.TrimSpace(string(msg)), secrets)}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("geoserver: decoding %s %s response: %w", method, path, err)
		}
	}
	return nil
}

// CheckCredentials comprueba si GeoServer acepta user/password. Un 401 significa
// credenciales inválidas (o usuario deshabilitado); un 403 significa que las
// credenciales son válidas pero el usuario no es administrador. La contraseña
// viaja solo en la cabecera Authorization, nunca en la URL.
func (c *Client) CheckCredentials(ctx context.Context, user, password string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restURL+"/about/version.json", nil)
	if err != nil {
		return false, fmt.Errorf("geoserver: building credentials check request: %w", err)
	}
	req.SetBasicAuth(user, password)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("geoserver: credentials check: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	return resp.StatusCode != http.StatusUnauthorized, nil
}
