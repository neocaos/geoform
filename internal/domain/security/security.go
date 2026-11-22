// Package security modela la seguridad de GeoServer: usuarios, roles y reglas
// de acceso a datos (lectura/escritura por workspace o capa).
package security

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrNotFound se devuelve cuando el usuario, rol o regla no existe.
var ErrNotFound = errors.New("security: no encontrado")

// Acceso soportado por las reglas.
const (
	Read  = "read"
	Write = "write"
)

// Any es el comodín de GeoServer (todos los workspaces, todas las capas).
const Any = "*"

// Roles especiales que GeoServer entiende en las reglas aunque no existan
// como rol propio.
var specialRoles = map[string]bool{Any: true, "ROLE_AUTHENTICATED": true, "ROLE_ANONYMOUS": true}

// Elementos creados por GeoServer que nunca se borran con prune.
var (
	protectedUsers = map[string]bool{"admin": true}
	protectedRoles = map[string]bool{"ADMIN": true, "GROUP_ADMIN": true}
)

var (
	roleRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	userRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]*$`)
	nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
)

// IsProtectedUser indica si el usuario es de sistema (no se poda).
func IsProtectedUser(name string) bool { return protectedUsers[name] }

// IsProtectedRole indica si el rol es de sistema (no se poda).
func IsProtectedRole(name string) bool { return protectedRoles[name] }

// IsKnownRole indica si el rol existe de serie en GeoServer o es un comodín.
func IsKnownRole(name string) bool { return protectedRoles[name] || specialRoles[name] }

// ValidRoleName comprueba el nombre de un rol.
func ValidRoleName(name string) error {
	if !roleRe.MatchString(name) {
		return fmt.Errorf("rol %q inválido: letras, dígitos y '_', empezando por letra", name)
	}
	return nil
}

// User es un usuario con sus roles. Password nunca se lee de GeoServer.
type User struct {
	Name     string
	Password string
	Enabled  bool
	Roles    []string
}

// Validate aplica las reglas mínimas del usuario.
func (u User) Validate() error {
	if !userRe.MatchString(u.Name) {
		return fmt.Errorf("usuario %q inválido: letras, dígitos y '_', '-', '.', '@'", u.Name)
	}
	for _, r := range u.Roles {
		if err := ValidRoleName(r); err != nil {
			return err
		}
	}
	return nil
}

// Rule es una regla de acceso a datos: los Roles tienen acceso Access
// (read|write) a la capa Layer del workspace Workspace ("*" = todos).
type Rule struct {
	Workspace string
	Layer     string
	Access    string
	Roles     []string
}

// Key devuelve la clave de GeoServer, p. ej. "demo.cities.r".
func (r Rule) Key() string {
	mode := "r"
	if r.Access == Write {
		mode = "w"
	}
	return r.Workspace + "." + r.Layer + "." + mode
}

// Validate aplica las reglas mínimas de la regla de acceso.
func (r Rule) Validate() error {
	if r.Workspace != Any && !nameRe.MatchString(r.Workspace) {
		return fmt.Errorf("workspace %q inválido en regla (sin puntos; usa '*' para todos)", r.Workspace)
	}
	if r.Layer != Any && !nameRe.MatchString(r.Layer) {
		return fmt.Errorf("capa %q inválida en regla (sin puntos; usa '*' para todas)", r.Layer)
	}
	if r.Workspace == Any && r.Layer != Any {
		return errors.New("una regla con capa concreta necesita un workspace concreto")
	}
	if r.Access != Read && r.Access != Write {
		return fmt.Errorf("acceso %q inválido, usa %q o %q", r.Access, Read, Write)
	}
	if len(r.Roles) == 0 {
		return errors.New("la regla necesita al menos un rol")
	}
	return nil
}

// ParseRuleKey interpreta una clave "ws.capa.modo" de GeoServer. Solo acepta
// los modos r y w con tres segmentos; el resto (p. ej. modo admin) no es
// gestionado por Geoform.
func ParseRuleKey(key string) (Rule, bool) {
	p := strings.Split(key, ".")
	if len(p) != 3 || p[0] == "" || p[1] == "" {
		return Rule{}, false
	}
	switch p[2] {
	case "r":
		return Rule{Workspace: p[0], Layer: p[1], Access: Read}, true
	case "w":
		return Rule{Workspace: p[0], Layer: p[1], Access: Write}, true
	}
	return Rule{}, false
}

// SortedRoles devuelve una copia ordenada y sin duplicados.
func SortedRoles(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// Repository abstrae la persistencia (implementada en infrastructure).
type Repository interface {
	ListRoles(ctx context.Context) ([]string, error)
	CreateRole(ctx context.Context, name string) error
	DeleteRole(ctx context.Context, name string) error

	ListUsers(ctx context.Context) ([]string, error)
	// GetUser devuelve el usuario con sus roles, sin contraseña.
	GetUser(ctx context.Context, name string) (*User, error)
	CreateUser(ctx context.Context, u User) error
	// UpdateUser cambia enabled y, si Password no está vacía, la contraseña.
	UpdateUser(ctx context.Context, u User) error
	DeleteUser(ctx context.Context, name string) error
	AssignRole(ctx context.Context, user, role string) error
	UnassignRole(ctx context.Context, user, role string) error

	ListRules(ctx context.Context) ([]Rule, error)
	// SaveRule crea la regla o, si existe, reemplaza sus roles.
	SaveRule(ctx context.Context, r Rule) error
	DeleteRule(ctx context.Context, r Rule) error
}

// Service contiene la lógica de negocio de seguridad.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) ListRoles(ctx context.Context) ([]string, error) { return s.repo.ListRoles(ctx) }
func (s *Service) ListUsers(ctx context.Context) ([]string, error) { return s.repo.ListUsers(ctx) }
func (s *Service) ListRules(ctx context.Context) ([]Rule, error)   { return s.repo.ListRules(ctx) }
func (s *Service) GetUser(ctx context.Context, name string) (*User, error) {
	return s.repo.GetUser(ctx, name)
}

// EnsureRole crea el rol si no existe.
func (s *Service) EnsureRole(ctx context.Context, name string) error {
	if err := ValidRoleName(name); err != nil {
		return err
	}
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return err
	}
	for _, r := range roles {
		if r == name {
			return nil
		}
	}
	return s.repo.CreateRole(ctx, name)
}

// RemoveRole borra un rol.
func (s *Service) RemoveRole(ctx context.Context, name string) error {
	return s.repo.DeleteRole(ctx, name)
}

// SaveUser crea o actualiza el usuario y sincroniza sus roles con u.Roles.
func (s *Service) SaveUser(ctx context.Context, u User) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("service: usuario inválido: %w", err)
	}
	cur, err := s.repo.GetUser(ctx, u.Name)
	var have []string
	switch {
	case errors.Is(err, ErrNotFound):
		if u.Password == "" {
			return fmt.Errorf("service: el usuario %q necesita contraseña al crearse", u.Name)
		}
		if err := s.repo.CreateUser(ctx, u); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		have = cur.Roles
		if err := s.repo.UpdateUser(ctx, u); err != nil {
			return err
		}
	}
	return s.syncRoles(ctx, u.Name, have, u.Roles)
}

func (s *Service) syncRoles(ctx context.Context, user string, have, want []string) error {
	h, w := map[string]bool{}, map[string]bool{}
	for _, r := range have {
		h[r] = true
	}
	for _, r := range want {
		w[r] = true
	}
	for _, r := range SortedRoles(want) {
		if !h[r] {
			if err := s.repo.AssignRole(ctx, user, r); err != nil {
				return fmt.Errorf("asignando rol %q: %w", r, err)
			}
		}
	}
	for _, r := range SortedRoles(have) {
		if !w[r] {
			if err := s.repo.UnassignRole(ctx, user, r); err != nil {
				return fmt.Errorf("retirando rol %q: %w", r, err)
			}
		}
	}
	return nil
}

// RemoveUser borra un usuario.
func (s *Service) RemoveUser(ctx context.Context, name string) error {
	return s.repo.DeleteUser(ctx, name)
}

// SaveRule crea o actualiza una regla de acceso.
func (s *Service) SaveRule(ctx context.Context, r Rule) error {
	if err := r.Validate(); err != nil {
		return fmt.Errorf("service: regla inválida: %w", err)
	}
	r.Roles = SortedRoles(r.Roles)
	return s.repo.SaveRule(ctx, r)
}

// RemoveRule borra una regla de acceso.
func (s *Service) RemoveRule(ctx context.Context, r Rule) error { return s.repo.DeleteRule(ctx, r) }
