package security_test

import (
	"reflect"
	"testing"

	"github.com/neocaos/geoform/internal/domain/security"
)

func TestRule_KeyAndParse(t *testing.T) {
	r := security.Rule{Workspace: "demo", Layer: "*", Access: security.Write}
	if r.Key() != "demo.*.w" {
		t.Fatal(r.Key())
	}
	got, ok := security.ParseRuleKey("demo.cities.r")
	if !ok || got.Workspace != "demo" || got.Layer != "cities" || got.Access != security.Read {
		t.Fatalf("%+v %v", got, ok)
	}
	for _, k := range []string{"demo.cities.a", "a.b.c.r", "*.r", ".x.r"} {
		if _, ok := security.ParseRuleKey(k); ok {
			t.Errorf("%q no debería interpretarse", k)
		}
	}
}

func TestRule_Validate(t *testing.T) {
	ok := security.Rule{Workspace: "demo", Layer: "cities", Access: security.Read, Roles: []string{"ROLE_A"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]security.Rule{
		"punto en workspace": {Workspace: "a.b", Layer: "*", Access: security.Read, Roles: []string{"R"}},
		"capa sin workspace": {Workspace: "*", Layer: "x", Access: security.Read, Roles: []string{"R"}},
		"acceso":             {Workspace: "demo", Layer: "*", Access: "admin", Roles: []string{"R"}},
		"sin roles":          {Workspace: "demo", Layer: "*", Access: security.Read},
	}
	for name, r := range bad {
		if r.Validate() == nil {
			t.Errorf("%s: esperaba error", name)
		}
	}
}

func TestUser_Validate(t *testing.T) {
	if err := (security.User{Name: "alice@x.org", Roles: []string{"ROLE_A"}}).Validate(); err != nil {
		t.Fatal(err)
	}
	if (security.User{Name: "a b"}).Validate() == nil || (security.User{Name: "a", Roles: []string{"1x"}}).Validate() == nil {
		t.Error("esperaba errores")
	}
}

func TestSortedRoles(t *testing.T) {
	if got := security.SortedRoles([]string{"B", "A", "B", " "}); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatal(got)
	}
}
