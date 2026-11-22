package geoserver

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// User es un usuario del servicio de usuarios/grupos por defecto de GeoServer.
// Password solo se envía; GeoServer nunca la devuelve.
type User struct {
	Name     string `json:"userName,omitempty"`
	Password string `json:"password,omitempty"`
	Enabled  bool   `json:"enabled"`
}

type userEnvelope struct {
	User User `json:"user"`
}

type usersResponse struct {
	Users []struct {
		Name    string `json:"userName"`
		Enabled bool   `json:"enabled"`
	} `json:"users"`
}

type rolesResponse struct {
	Roles []string `json:"roles"`
}

func segment(s string) string { return url.PathEscape(s) }

// ListUsers devuelve los usuarios (sin contraseña).
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	var resp usersResponse
	if err := c.do(ctx, http.MethodGet, "/security/usergroup/users.json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	out := make([]User, 0, len(resp.Users))
	for _, u := range resp.Users {
		out = append(out, User{Name: u.Name, Enabled: u.Enabled})
	}
	return out, nil
}

// GetUser busca un usuario por nombre. GeoServer no expone un GET individual
// fiable, así que se filtra el listado; si no existe devuelve un error que
// cumple IsNotFound.
func (c *Client) GetUser(ctx context.Context, name string) (*User, error) {
	users, err := c.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Name == name {
			return &u, nil
		}
	}
	return nil, &APIError{Method: http.MethodGet, Path: "/security/usergroup/user/" + segment(name), StatusCode: http.StatusNotFound, Body: "user not found"}
}

// CreateUser crea un usuario. Password es obligatoria.
func (c *Client) CreateUser(ctx context.Context, u User) error {
	return c.do(ctx, http.MethodPost, "/security/usergroup/users", userEnvelope{User: u}, nil, http.StatusCreated)
}

// UpdateUser modifica un usuario existente. Con Password vacía no se cambia.
func (c *Client) UpdateUser(ctx context.Context, u User) error {
	body := userEnvelope{User: User{Password: u.Password, Enabled: u.Enabled}}
	return c.do(ctx, http.MethodPost, "/security/usergroup/user/"+segment(u.Name), body, nil, http.StatusOK)
}

// DeleteUser borra un usuario.
func (c *Client) DeleteUser(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/security/usergroup/user/"+segment(name), nil, nil, http.StatusOK)
}

// ListRoles devuelve todos los roles.
func (c *Client) ListRoles(ctx context.Context) ([]string, error) {
	var resp rolesResponse
	if err := c.do(ctx, http.MethodGet, "/security/roles.json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	return resp.Roles, nil
}

// CreateRole crea un rol.
func (c *Client) CreateRole(ctx context.Context, role string) error {
	return c.do(ctx, http.MethodPost, "/security/roles/role/"+segment(role), nil, nil, http.StatusCreated)
}

// DeleteRole borra un rol.
func (c *Client) DeleteRole(ctx context.Context, role string) error {
	return c.do(ctx, http.MethodDelete, "/security/roles/role/"+segment(role), nil, nil, http.StatusOK)
}

// UserRoles devuelve los roles asignados a un usuario.
func (c *Client) UserRoles(ctx context.Context, user string) ([]string, error) {
	var resp rolesResponse
	if err := c.do(ctx, http.MethodGet, "/security/roles/user/"+segment(user)+".json", nil, &resp, http.StatusOK); err != nil {
		return nil, err
	}
	return resp.Roles, nil
}

// AssignRole asigna un rol a un usuario.
func (c *Client) AssignRole(ctx context.Context, user, role string) error {
	return c.do(ctx, http.MethodPost, "/security/roles/role/"+segment(role)+"/user/"+segment(user), nil, nil, http.StatusOK)
}

// UnassignRole retira un rol a un usuario.
func (c *Client) UnassignRole(ctx context.Context, user, role string) error {
	return c.do(ctx, http.MethodDelete, "/security/roles/role/"+segment(role)+"/user/"+segment(user), nil, nil, http.StatusOK)
}

// LayerRules devuelve las reglas de acceso a datos: clave "ws.capa.modo"
// (r, w o a; "*" como comodín) y valor con los roles separados por comas.
func (c *Client) LayerRules(ctx context.Context) (map[string]string, error) {
	rules := map[string]string{}
	if err := c.do(ctx, http.MethodGet, "/security/acl/layers.json", nil, &rules, http.StatusOK); err != nil {
		return nil, err
	}
	return rules, nil
}

// CreateLayerRule crea una regla nueva; GeoServer responde 409 si ya existe.
func (c *Client) CreateLayerRule(ctx context.Context, key string, roles []string) error {
	return c.do(ctx, http.MethodPost, "/security/acl/layers", map[string]string{key: strings.Join(roles, ",")}, nil, http.StatusOK)
}

// UpdateLayerRule cambia los roles de una regla existente.
func (c *Client) UpdateLayerRule(ctx context.Context, key string, roles []string) error {
	return c.do(ctx, http.MethodPut, "/security/acl/layers", map[string]string{key: strings.Join(roles, ",")}, nil, http.StatusOK)
}

// DeleteLayerRule borra una regla.
func (c *Client) DeleteLayerRule(ctx context.Context, key string) error {
	return c.do(ctx, http.MethodDelete, "/security/acl/layers/"+segment(key), nil, nil, http.StatusOK)
}
