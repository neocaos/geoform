package storage

import (
	"fmt"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/security"
)

// Sección opcional de seguridad del fichero (si falta, no se gestiona):
//
//	security:
//	  roles: [ROLE_EDITOR, ROLE_VIEWER]
//	  users:
//	    - name: alice
//	      password_env: ALICE_PASSWORD   # obligatorio; nunca en claro
//	      enabled: true                  # opcional, true
//	      roles: [ROLE_EDITOR]
//	  rules:                             # reglas de acceso a datos
//	    - workspace: demo
//	      layer: cities                  # opcional, "*" = todas
//	      access: read                   # read | write
//	      roles: [ROLE_VIEWER, ROLE_EDITOR]
type fileSecurity struct {
	Roles []string       `yaml:"roles"`
	Users []fileUser     `yaml:"users"`
	Rules []fileRuleSpec `yaml:"rules"`
}

type fileUser struct {
	Name        string   `yaml:"name"`
	PasswordEnv string   `yaml:"password_env"`
	Enabled     *bool    `yaml:"enabled"`
	Roles       []string `yaml:"roles"`
}

type fileRuleSpec struct {
	Workspace string   `yaml:"workspace"`
	Layer     string   `yaml:"layer"`
	Access    string   `yaml:"access"`
	Roles     []string `yaml:"roles"`
}

// buildSecurity traduce la sección security. Las contraseñas solo se aceptan
// por variable de entorno y sus valores no aparecen en ningún mensaje.
func buildSecurity(f fileSecurity, getenv func(string) string) (application.SecurityDesired, []error) {
	var (
		out  application.SecurityDesired
		errs []error
	)
	bad := func(path, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...)))
	}
	out.Roles = append(out.Roles, f.Roles...)

	for i, u := range f.Users {
		p := fmt.Sprintf("security.users[%d]", i)
		if u.Name == "" {
			bad(p+".name", "el nombre es obligatorio")
		}
		pass := ""
		if u.PasswordEnv == "" {
			bad(p+".password_env", "es obligatorio (nombre de la variable de entorno con la contraseña)")
		} else if pass = getenv(u.PasswordEnv); pass == "" {
			bad(p, "la variable de entorno %s (password_env) no está definida", u.PasswordEnv)
		}
		enabled := true
		if u.Enabled != nil {
			enabled = *u.Enabled
		}
		out.Users = append(out.Users, security.User{Name: u.Name, Password: pass, Enabled: enabled, Roles: u.Roles})
	}

	for i, r := range f.Rules {
		p := fmt.Sprintf("security.rules[%d]", i)
		if r.Workspace == "" {
			bad(p+".workspace", "es obligatorio (usa '*' para todos)")
		}
		if r.Layer == "" {
			r.Layer = security.Any
		}
		if r.Access != security.Read && r.Access != security.Write {
			bad(p+".access", "debe ser %q o %q", security.Read, security.Write)
		}
		if len(r.Roles) == 0 {
			bad(p+".roles", "necesita al menos un rol")
		}
		out.Rules = append(out.Rules, security.Rule{Workspace: r.Workspace, Layer: r.Layer, Access: r.Access, Roles: r.Roles})
	}
	return out, errs
}
