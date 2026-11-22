package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/neocaos/geoform/internal/domain/security"
)

// SecurityDesired es el estado deseado de seguridad. Un puntero nil en
// Desired.Security significa "seguridad no gestionada": no se lee ni se toca.
type SecurityDesired struct {
	Roles []string
	Users []security.User
	Rules []security.Rule
}

// WithSecurity habilita la gestión de usuarios, roles y reglas de acceso.
func (r *Reconciler) WithSecurity(s *security.Service) *Reconciler {
	r.sec = s
	return r
}

func (s SecurityDesired) validate(workspaces map[string]bool) []error {
	var errs []error
	roles := map[string]bool{}
	for _, n := range s.Roles {
		if err := security.ValidRoleName(n); err != nil {
			errs = append(errs, err)
		}
		if roles[n] {
			errs = append(errs, fmt.Errorf("rol %q duplicado", n))
		}
		roles[n] = true
	}
	known := func(n string) bool { return roles[n] || security.IsKnownRole(n) }

	users := map[string]bool{}
	for _, u := range s.Users {
		if err := u.Validate(); err != nil {
			errs = append(errs, err)
		}
		if users[u.Name] {
			errs = append(errs, fmt.Errorf("usuario %q duplicado", u.Name))
		}
		users[u.Name] = true
		for _, r := range u.Roles {
			if !known(r) {
				errs = append(errs, fmt.Errorf("usuario %q: el rol %q no está declarado", u.Name, r))
			}
		}
	}
	rules := map[string]bool{}
	for _, r := range s.Rules {
		k := r.Key()
		if err := r.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("regla %q: %w", k, err))
		}
		if rules[k] {
			errs = append(errs, fmt.Errorf("regla %q duplicada", k))
		}
		rules[k] = true
		if r.Workspace != security.Any && !workspaces[r.Workspace] {
			errs = append(errs, fmt.Errorf("regla %q: el workspace %q no está declarado", k, r.Workspace))
		}
		for _, role := range r.Roles {
			if !known(role) {
				errs = append(errs, fmt.Errorf("regla %q: el rol %q no está declarado", k, role))
			}
		}
	}
	return errs
}

// planSecurity añade los cambios de seguridad: creaciones/modificaciones en
// plan.Changes y, con prune, devuelve los borrados (reglas, usuarios, roles).
func (r *Reconciler) planSecurity(ctx context.Context, want Desired, prune bool, plan *Plan) ([]Change, error) {
	if want.Security == nil {
		return nil, nil
	}
	if r.sec == nil {
		return nil, errors.New("la seguridad está declarada pero el reconciliador no tiene servicio de seguridad")
	}
	sec := want.Security

	haveRoles, err := r.sec.ListRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("listando roles: %w", err)
	}
	roleSet := toSet(haveRoles)
	for _, n := range sec.Roles {
		if !roleSet[n] {
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindRole, Address: n, Role: n})
		}
	}

	haveUsers, err := r.sec.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listando usuarios: %w", err)
	}
	userSet := toSet(haveUsers)
	for _, u := range sec.Users {
		u.Roles = security.SortedRoles(u.Roles)
		if !userSet[u.Name] {
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindUser, Address: u.Name, User: u,
				Details: []string{"roles: " + listOrNone(u.Roles)}})
			continue
		}
		cur, err := r.sec.GetUser(ctx, u.Name)
		if err != nil {
			return nil, fmt.Errorf("leyendo usuario %q: %w", u.Name, err)
		}
		// GeoServer no devuelve la contraseña: si el repositorio sabe
		// verificarla (login real), una rotación aparece como cambio. Un usuario
		// deshabilitado no puede iniciar sesión, así que no se comprueba.
		var diff []string
		if cur.Enabled && u.Enabled {
			ok, supported, err := r.sec.PasswordCurrent(ctx, u.Name, u.Password)
			if err != nil {
				return nil, fmt.Errorf("verificando contraseña de %q: %w", u.Name, err)
			}
			if supported && !ok {
				diff = append(diff, "password: rotar")
			}
		}
		if cur.Enabled != u.Enabled {
			diff = append(diff, fmt.Sprintf("enabled: %t -> %t", cur.Enabled, u.Enabled))
		}
		if a, b := security.SortedRoles(cur.Roles), u.Roles; !equal(a, b) {
			diff = append(diff, fmt.Sprintf("roles: %s -> %s", listOrNone(a), listOrNone(b)))
		}
		if len(diff) > 0 {
			plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindUser, Address: u.Name, User: u, Details: diff})
		}
	}

	haveRules, err := r.sec.ListRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("listando reglas de acceso: %w", err)
	}
	ruleByKey := map[string]security.Rule{}
	for _, ru := range haveRules {
		ruleByKey[ru.Key()] = ru
	}
	wantRule := map[string]bool{}
	for _, ru := range sec.Rules {
		ru.Roles = security.SortedRoles(ru.Roles)
		k := ru.Key()
		wantRule[k] = true
		cur, ok := ruleByKey[k]
		switch {
		case !ok:
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindRule, Address: k, Rule: ru,
				Details: []string{"roles: " + listOrNone(ru.Roles)}})
		case !equal(cur.Roles, ru.Roles):
			plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindRule, Address: k, Rule: ru,
				Details: []string{fmt.Sprintf("roles: %s -> %s", listOrNone(cur.Roles), listOrNone(ru.Roles))}})
		}
	}

	if !prune {
		return nil, nil
	}
	var out []Change
	// Reglas: solo las de workspaces declarados; las globales (*.*.r) no se tocan.
	declared := map[string]bool{}
	for _, w := range want.Workspaces {
		declared[w.Name] = true
	}
	for _, ru := range haveRules {
		if declared[ru.Workspace] && !wantRule[ru.Key()] {
			out = append(out, Change{Action: Delete, Kind: KindRule, Address: ru.Key(), Rule: ru})
		}
	}
	wantUser := map[string]bool{}
	for _, u := range sec.Users {
		wantUser[u.Name] = true
	}
	sort.Strings(haveUsers)
	for _, n := range haveUsers {
		if !wantUser[n] && !security.IsProtectedUser(n) {
			out = append(out, Change{Action: Delete, Kind: KindUser, Address: n, User: security.User{Name: n}})
		}
	}
	wantRole := toSet(sec.Roles)
	sort.Strings(haveRoles)
	for _, n := range haveRoles {
		if !wantRole[n] && !security.IsProtectedRole(n) {
			out = append(out, Change{Action: Delete, Kind: KindRole, Address: n, Role: n})
		}
	}
	return out, nil
}

func (r *Reconciler) applySecurity(ctx context.Context, c Change) (bool, error) {
	switch c.Kind {
	case KindRole:
		switch c.Action {
		case Create:
			return true, r.sec.EnsureRole(ctx, c.Role)
		case Delete:
			return true, ignoreNotFound(r.sec.RemoveRole(ctx, c.Role), security.ErrNotFound)
		}
	case KindUser:
		switch c.Action {
		case Create, Update:
			return true, r.sec.SaveUser(ctx, c.User)
		case Delete:
			return true, ignoreNotFound(r.sec.RemoveUser(ctx, c.User.Name), security.ErrNotFound)
		}
	case KindRule:
		switch c.Action {
		case Create, Update:
			return true, r.sec.SaveRule(ctx, c.Rule)
		case Delete:
			return true, ignoreNotFound(r.sec.RemoveRule(ctx, c.Rule), security.ErrNotFound)
		}
	default:
		return false, nil
	}
	return true, errors.New("cambio no soportado")
}

func toSet(in []string) map[string]bool {
	m := make(map[string]bool, len(in))
	for _, s := range in {
		m[s] = true
	}
	return m
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "(ninguno)"
	}
	return strings.Join(l, ",")
}
