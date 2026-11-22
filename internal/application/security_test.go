package application_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/security"
	"github.com/neocaos/geoform/internal/infrastructure/memory"
)

func secReconciler(s *memory.Store, sec *memory.SecurityStore) *application.Reconciler {
	return newReconciler(s).WithSecurity(security.NewService(sec))
}

func secDesired() application.Desired {
	d := desired()
	d.Security = &application.SecurityDesired{
		Roles: []string{"ROLE_EDITOR", "ROLE_VIEWER"},
		Users: []security.User{{Name: "alice", Password: "s3cret-value", Enabled: true, Roles: []string{"ROLE_EDITOR"}}},
		Rules: []security.Rule{
			{Workspace: "demo", Layer: "*", Access: security.Read, Roles: []string{"ROLE_VIEWER", "ROLE_EDITOR"}},
			{Workspace: "demo", Layer: "cities", Access: security.Write, Roles: []string{"ROLE_EDITOR"}},
		},
	}
	return d
}

func TestSecurity_PlanApplyIdempotent(t *testing.T) {
	ctx := context.Background()
	s, sec := memory.New(), memory.NewSecurity()
	r := secReconciler(s, sec)

	plan := mustPlan(t, r, secDesired(), false)
	out := ""
	for _, c := range plan.Changes {
		out += string(c.Action) + " " + string(c.Kind) + " " + c.Address + " " + strings.Join(c.Details, ";") + "\n"
		if strings.Contains(out, "s3cret") {
			t.Fatal("el plan no debe mostrar contraseñas")
		}
	}
	for _, want := range []string{"create role ROLE_EDITOR", "create user alice", "create rule demo.*.r", "create rule demo.cities.w"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en:\n%s", want, out)
		}
	}
	if err := r.Apply(ctx, plan, nil); err != nil {
		t.Fatal(err)
	}
	if u := sec.Users["alice"]; u.Password != "s3cret-value" || !reflect.DeepEqual(u.Roles, []string{"ROLE_EDITOR"}) {
		t.Fatalf("usuario: %+v", u)
	}
	if got := sec.Rules["demo.*.r"].Roles; !reflect.DeepEqual(got, []string{"ROLE_EDITOR", "ROLE_VIEWER"}) {
		t.Fatalf("roles de la regla: %v", got)
	}
	if p := mustPlan(t, r, secDesired(), true); p.HasChanges() {
		t.Fatalf("el segundo plan debe estar vacío: %+v", p.Changes)
	}
}

func TestSecurity_UpdateUserAndRule(t *testing.T) {
	ctx := context.Background()
	s, sec := memory.New(), memory.NewSecurity()
	r := secReconciler(s, sec)
	if err := r.Apply(ctx, mustPlan(t, r, secDesired(), false), nil); err != nil {
		t.Fatal(err)
	}
	d := secDesired()
	d.Security.Users[0].Enabled = false
	d.Security.Users[0].Roles = []string{"ROLE_VIEWER"}
	d.Security.Rules[1].Roles = []string{"ROLE_EDITOR", "ROLE_VIEWER"}
	plan := mustPlan(t, r, d, false)
	if plan.Count(application.Update) != 2 {
		t.Fatalf("esperaba 2 updates: %+v", plan.Changes)
	}
	if err := r.Apply(ctx, plan, nil); err != nil {
		t.Fatal(err)
	}
	if u := sec.Users["alice"]; u.Enabled || !reflect.DeepEqual(u.Roles, []string{"ROLE_VIEWER"}) {
		t.Fatalf("usuario: %+v", u)
	}
}

func TestSecurity_Prune(t *testing.T) {
	ctx := context.Background()
	s, sec := memory.New(), memory.NewSecurity()
	r := secReconciler(s, sec)
	if err := r.Apply(ctx, mustPlan(t, r, secDesired(), false), nil); err != nil {
		t.Fatal(err)
	}
	// Sobrantes: un usuario, un rol, una regla de un workspace declarado, una
	// regla global y una de un workspace no declarado.
	sec.Users["mallory"] = security.User{Name: "mallory"}
	sec.Roles["ROLE_OLD"] = true
	sec.Rules["demo.old.r"] = security.Rule{Workspace: "demo", Layer: "old", Access: security.Read, Roles: []string{"ROLE_OLD"}}
	sec.Rules["*.*.r"] = security.Rule{Workspace: "*", Layer: "*", Access: security.Read, Roles: []string{"*"}}
	sec.Rules["other.x.r"] = security.Rule{Workspace: "other", Layer: "x", Access: security.Read, Roles: []string{"*"}}

	if p := mustPlan(t, r, secDesired(), false); p.HasChanges() {
		t.Fatalf("sin prune no hay borrados: %+v", p.Changes)
	}
	plan := mustPlan(t, r, secDesired(), true)
	if plan.Count(application.Delete) != 3 {
		t.Fatalf("esperaba 3 borrados: %+v", plan.Changes)
	}
	if err := r.Apply(ctx, plan, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.Users["mallory"]; ok || sec.Roles["ROLE_OLD"] {
		t.Error("usuario/rol sobrantes debían borrarse")
	}
	if _, ok := sec.Rules["demo.old.r"]; ok {
		t.Error("la regla sobrante debía borrarse")
	}
	for _, keep := range []string{"*.*.r", "other.x.r"} {
		if _, ok := sec.Rules[keep]; !ok {
			t.Errorf("la regla %s no debía tocarse", keep)
		}
	}
	if _, ok := sec.Users["admin"]; !ok || !sec.Roles["ADMIN"] || !sec.Roles["GROUP_ADMIN"] {
		t.Error("los elementos de sistema no deben podarse")
	}
}

func TestSecurity_Validate(t *testing.T) {
	d := secDesired()
	d.Security.Users[0].Roles = []string{"ROLE_UNKNOWN"}
	d.Security.Rules[0].Workspace = "nope"
	d.Security.Rules = append(d.Security.Rules, d.Security.Rules[1])
	err := d.Validate()
	if err == nil {
		t.Fatal("esperaba errores")
	}
	for _, want := range []string{"ROLE_UNKNOWN", `workspace "nope"`, "duplicada"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("falta %q en %v", want, err)
		}
	}
}

func TestSecurity_NotManagedByDefault(t *testing.T) {
	s, sec := memory.New(), memory.NewSecurity()
	sec.Users["mallory"] = security.User{Name: "mallory"}
	r := secReconciler(s, sec)
	if p := mustPlan(t, r, desired(), true); p.Count(application.Delete) != 0 {
		t.Fatalf("sin sección security no se toca nada: %+v", p.Changes)
	}
	if _, err := newReconciler(s).Plan(context.Background(), secDesired(), application.PlanOptions{}); err == nil {
		t.Fatal("seguridad declarada sin servicio debe fallar")
	}
}

func TestSecurity_ApplyStopsOnError(t *testing.T) {
	s, sec := memory.New(), memory.NewSecurity()
	sec.FailOn = "create user alice"
	r := secReconciler(s, sec)
	err := r.Apply(context.Background(), mustPlan(t, r, secDesired(), false), nil)
	if err == nil || !strings.Contains(err.Error(), "user alice") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("error: %v", err)
	}
}

func TestSecurity_PasswordRotationIsPlannedAndApplied(t *testing.T) {
	ctx := context.Background()
	s, sec := memory.New(), memory.NewSecurity()
	r := secReconciler(s, sec)
	d := secDesired()
	if err := r.Apply(ctx, mustPlan(t, r, d, false), nil); err != nil {
		t.Fatal(err)
	}
	if plan := mustPlan(t, r, d, false); plan.HasChanges() {
		t.Fatalf("sin rotación no debe haber diff: %+v", plan.Changes)
	}

	d.Security.Users[0].Password = "otra-contraseña-nueva"
	plan := mustPlan(t, r, d, false)
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != application.KindUser || plan.Changes[0].Details[0] != "password: rotar" {
		t.Fatalf("rotación no detectada: %+v", plan.Changes)
	}
	for _, c := range plan.Changes {
		if strings.Contains(strings.Join(c.Details, " "), "otra-contraseña") {
			t.Fatal("el plan no debe mostrar la contraseña")
		}
	}
	if err := r.Apply(ctx, plan, nil); err != nil {
		t.Fatal(err)
	}
	if sec.Users["alice"].Password != "otra-contraseña-nueva" {
		t.Fatal("la contraseña no se rotó")
	}
	if plan := mustPlan(t, r, d, false); plan.HasChanges() {
		t.Fatalf("tras rotar no debe haber diff: %+v", plan.Changes)
	}
}
