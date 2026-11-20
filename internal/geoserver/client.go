// Package geoserver is a thin client for the GeoServer REST API.
package geoserver

import (
	"bytes"
	"context"
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

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

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
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("geoserver: encoding %s %s request: %w", method, path, err)
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
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("geoserver: decoding %s %s response: %w", method, path, err)
		}
	}
	return nil
}
