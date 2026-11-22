package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/neocaos/geoform/internal/domain/security"
)

// SecurityStore es un security.Repository en memoria para tests. Registra las
// escrituras en Writes (p. ej. "create user alice") y puede fallar con FailOn.
type SecurityStore struct {
	mu     sync.Mutex
	Roles  map[string]bool
	Users  map[string]security.User // sin contraseña real: se guarda para comprobar
	Rules  map[string]security.Rule // clave "ws.capa.modo"
	Writes []string
	FailOn string
}

// NewSecurity crea un almacén con los roles de sistema de GeoServer.
func NewSecurity() *SecurityStore {
	return &SecurityStore{
		Roles: map[string]bool{"ADMIN": true, "GROUP_ADMIN": true},
		Users: map[string]security.User{"admin": {Name: "admin", Enabled: true}},
		Rules: map[string]security.Rule{},
	}
}

func (s *SecurityStore) write(op string) error {
	s.Writes = append(s.Writes, op)
	if s.FailOn == op {
		return errFail(op)
	}
	return nil
}

func (s *SecurityStore) ListRoles(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for r := range s.Roles {
		out = append(out, r)
	}
	sort.Strings(out)
	return out, nil
}

func (s *SecurityStore) CreateRole(_ context.Context, n string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write("create role " + n); err != nil {
		return err
	}
	s.Roles[n] = true
	return nil
}

func (s *SecurityStore) DeleteRole(_ context.Context, n string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Roles[n] {
		return security.ErrNotFound
	}
	if err := s.write("delete role " + n); err != nil {
		return err
	}
	delete(s.Roles, n)
	return nil
}

func (s *SecurityStore) ListUsers(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for n := range s.Users {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (s *SecurityStore) GetUser(_ context.Context, n string) (*security.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.Users[n]
	if !ok {
		return nil, security.ErrNotFound
	}
	u.Password = ""
	u.Roles = append([]string(nil), u.Roles...)
	return &u, nil
}

func (s *SecurityStore) CreateUser(_ context.Context, u security.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write("create user " + u.Name); err != nil {
		return err
	}
	u.Roles = nil
	s.Users[u.Name] = u
	return nil
}

func (s *SecurityStore) UpdateUser(_ context.Context, u security.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.Users[u.Name]
	if !ok {
		return security.ErrNotFound
	}
	if err := s.write("update user " + u.Name); err != nil {
		return err
	}
	cur.Enabled = u.Enabled
	if u.Password != "" {
		cur.Password = u.Password
	}
	s.Users[u.Name] = cur
	return nil
}

func (s *SecurityStore) DeleteUser(_ context.Context, n string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Users[n]; !ok {
		return security.ErrNotFound
	}
	if err := s.write("delete user " + n); err != nil {
		return err
	}
	delete(s.Users, n)
	return nil
}

func (s *SecurityStore) AssignRole(_ context.Context, user, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write("assign " + role + " " + user); err != nil {
		return err
	}
	u := s.Users[user]
	u.Roles = append(u.Roles, role)
	s.Users[user] = u
	return nil
}

func (s *SecurityStore) UnassignRole(_ context.Context, user, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write("unassign " + role + " " + user); err != nil {
		return err
	}
	u := s.Users[user]
	var keep []string
	for _, r := range u.Roles {
		if r != role {
			keep = append(keep, r)
		}
	}
	u.Roles = keep
	s.Users[user] = u
	return nil
}

func (s *SecurityStore) ListRules(context.Context) ([]security.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []security.Rule
	for _, r := range s.Rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out, nil
}

func (s *SecurityStore) SaveRule(_ context.Context, r security.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write("save rule " + r.Key()); err != nil {
		return err
	}
	s.Rules[r.Key()] = r
	return nil
}

func (s *SecurityStore) DeleteRule(_ context.Context, r security.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Rules[r.Key()]; !ok {
		return security.ErrNotFound
	}
	if err := s.write("delete rule " + r.Key()); err != nil {
		return err
	}
	delete(s.Rules, r.Key())
	return nil
}
