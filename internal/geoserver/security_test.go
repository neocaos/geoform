package geoserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/geoserver"
)

func TestListUsers_NeverExposesPassword(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/geoserver/rest/security/usergroup/users.json" {
			t.Errorf("path: %s", r.URL.Path)
		}
		io.WriteString(w, `{"users":[{"userName":"admin","password":"crypt1:xyz","enabled":true},{"userName":"bob","password":null,"enabled":false}]}`)
	})
	users, err := c.ListUsers(context.Background())
	if err != nil || len(users) != 2 || users[1].Name != "bob" || users[1].Enabled || users[0].Password != "" {
		t.Fatalf("users=%+v err=%v", users, err)
	}
	if _, err := c.GetUser(context.Background(), "ghost"); !geoserver.IsNotFound(err) {
		t.Fatalf("esperaba not found: %v", err)
	}
	if u, err := c.GetUser(context.Background(), "bob"); err != nil || u.Name != "bob" {
		t.Fatalf("GetUser: %+v %v", u, err)
	}
}

func TestCreateAndUpdateUser(t *testing.T) {
	var calls []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		if strings.HasSuffix(r.URL.Path, "/users") {
			w.WriteHeader(http.StatusCreated)
		}
	})
	ctx := context.Background()
	if err := c.CreateUser(ctx, geoserver.User{Name: "alice", Password: "pw", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// Con contraseña vacía no se envía el campo (no se cambia).
	if err := c.UpdateUser(ctx, geoserver.User{Name: "alice", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`POST /geoserver/rest/security/usergroup/users {"user":{"userName":"alice","password":"pw","enabled":true}}`,
		`POST /geoserver/rest/security/usergroup/user/alice {"user":{"enabled":false}}`,
	}
	for i, w := range want {
		if calls[i] != w {
			t.Errorf("call %d:\n got %s\nwant %s", i, calls[i], w)
		}
	}
}

func TestRolesAndAssignments(t *testing.T) {
	var calls []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet:
			io.WriteString(w, `{"roles":["ROLE_A"]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/role/ROLE_A"):
			w.WriteHeader(http.StatusCreated)
		}
	})
	ctx := context.Background()
	if roles, err := c.ListRoles(ctx); err != nil || len(roles) != 1 {
		t.Fatalf("ListRoles: %v %v", roles, err)
	}
	if roles, err := c.UserRoles(ctx, "alice"); err != nil || roles[0] != "ROLE_A" {
		t.Fatalf("UserRoles: %v %v", roles, err)
	}
	for _, err := range []error{
		c.CreateRole(ctx, "ROLE_A"), c.AssignRole(ctx, "alice", "ROLE_A"),
		c.UnassignRole(ctx, "alice", "ROLE_A"), c.DeleteRole(ctx, "ROLE_A"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"GET /geoserver/rest/security/roles.json",
		"GET /geoserver/rest/security/roles/user/alice.json",
		"POST /geoserver/rest/security/roles/role/ROLE_A",
		"POST /geoserver/rest/security/roles/role/ROLE_A/user/alice",
		"DELETE /geoserver/rest/security/roles/role/ROLE_A/user/alice",
		"DELETE /geoserver/rest/security/roles/role/ROLE_A",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s", strings.Join(calls, "\n"))
	}
}

func TestLayerRules(t *testing.T) {
	var got []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+string(body))
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"*.*.r":"*","demo.cities.w":"ROLE_A,ROLE_B"}`)
		}
	})
	ctx := context.Background()
	rules, err := c.LayerRules(ctx)
	if err != nil || rules["demo.cities.w"] != "ROLE_A,ROLE_B" {
		t.Fatalf("rules=%v err=%v", rules, err)
	}
	if err := c.CreateLayerRule(ctx, "demo.*.r", []string{"ROLE_A", "ROLE_B"}); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateLayerRule(ctx, "demo.*.r", []string{"ROLE_A"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteLayerRule(ctx, "demo.*.r"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`POST /geoserver/rest/security/acl/layers {"demo.*.r":"ROLE_A,ROLE_B"}`,
		`PUT /geoserver/rest/security/acl/layers {"demo.*.r":"ROLE_A"}`,
		`DELETE /geoserver/rest/security/acl/layers/demo.%2A.r `,
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i+1], strings.SplitN(w, " ", 3)[0]+" ") {
			t.Errorf("call %d: %s", i, got[i+1])
		}
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(strings.SplitN(got[1], " ", 3)[2]), &m); err != nil || m["demo.*.r"] != "ROLE_A,ROLE_B" {
		t.Errorf("body: %v %v", m, err)
	}
}
