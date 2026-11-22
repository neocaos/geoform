package geoserver

import (
	"context"
	"sort"
	"strings"

	"github.com/neocaos/geoform/internal/domain/security"
	gs "github.com/neocaos/geoform/internal/geoserver"
)

// SecurityRepository adapta Client a security.Repository.
type SecurityRepository struct{ c *gs.Client }

func NewSecurityRepository(c *gs.Client) *SecurityRepository { return &SecurityRepository{c} }

func notFound(err error) error {
	if gs.IsNotFound(err) {
		return security.ErrNotFound
	}
	return err
}

func (r *SecurityRepository) ListRoles(ctx context.Context) ([]string, error) {
	return r.c.ListRoles(ctx)
}

func (r *SecurityRepository) CreateRole(ctx context.Context, name string) error {
	return r.c.CreateRole(ctx, name)
}

func (r *SecurityRepository) DeleteRole(ctx context.Context, name string) error {
	return notFound(r.c.DeleteRole(ctx, name))
}

func (r *SecurityRepository) ListUsers(ctx context.Context) ([]string, error) {
	users, err := r.c.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Name)
	}
	return names, nil
}

func (r *SecurityRepository) GetUser(ctx context.Context, name string) (*security.User, error) {
	u, err := r.c.GetUser(ctx, name)
	if err != nil {
		return nil, notFound(err)
	}
	roles, err := r.c.UserRoles(ctx, name)
	if err != nil {
		return nil, notFound(err)
	}
	return &security.User{Name: u.Name, Enabled: u.Enabled, Roles: roles}, nil
}

func (r *SecurityRepository) CreateUser(ctx context.Context, u security.User) error {
	return r.c.CreateUser(ctx, gs.User{Name: u.Name, Password: u.Password, Enabled: u.Enabled})
}

func (r *SecurityRepository) UpdateUser(ctx context.Context, u security.User) error {
	return notFound(r.c.UpdateUser(ctx, gs.User{Name: u.Name, Password: u.Password, Enabled: u.Enabled}))
}

func (r *SecurityRepository) DeleteUser(ctx context.Context, name string) error {
	return notFound(r.c.DeleteUser(ctx, name))
}

func (r *SecurityRepository) AssignRole(ctx context.Context, user, role string) error {
	return r.c.AssignRole(ctx, user, role)
}

func (r *SecurityRepository) UnassignRole(ctx context.Context, user, role string) error {
	return notFound(r.c.UnassignRole(ctx, user, role))
}

func (r *SecurityRepository) ListRules(ctx context.Context) ([]security.Rule, error) {
	m, err := r.c.LayerRules(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []security.Rule
	for _, k := range keys {
		rule, ok := security.ParseRuleKey(k)
		if !ok {
			continue // reglas de modo admin o con nombres con puntos: no gestionadas
		}
		rule.Roles = security.SortedRoles(strings.Split(m[k], ","))
		out = append(out, rule)
	}
	return out, nil
}

func (r *SecurityRepository) SaveRule(ctx context.Context, rule security.Rule) error {
	m, err := r.c.LayerRules(ctx)
	if err != nil {
		return err
	}
	if _, exists := m[rule.Key()]; exists {
		return r.c.UpdateLayerRule(ctx, rule.Key(), rule.Roles)
	}
	return r.c.CreateLayerRule(ctx, rule.Key(), rule.Roles)
}

func (r *SecurityRepository) DeleteRule(ctx context.Context, rule security.Rule) error {
	return notFound(r.c.DeleteLayerRule(ctx, rule.Key()))
}
