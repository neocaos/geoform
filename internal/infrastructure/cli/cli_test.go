package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
	"github.com/neocaos/geoform/internal/infrastructure/cli"
	"github.com/neocaos/geoform/internal/infrastructure/memory"
)

const config = `
workspaces:
  - name: demo
    datastores:
      - name: pg
        host: db
        database: geo
        user: u
        password_env: PG_PASSWORD
        layers:
          - name: cities
            srs: EPSG:4326
`

type result struct {
	code        int
	out, errOut string
	conn        cli.Conn
}

func run(t *testing.T, s *memory.Store, stdin string, args ...string) result {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "geoform.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	var res result
	env := cli.Env{
		Args:   append([]string{args[0], "--file", path}, args[1:]...),
		Stdin:  strings.NewReader(stdin),
		Stdout: &out, Stderr: &errOut,
		Getenv: func(k string) string {
			return map[string]string{"PG_PASSWORD": "pw", "GEOSERVER_URL": "http://env:1/geoserver", "GEOSERVER_PASSWORD": "envpass"}[k]
		},
		NewReconciler: func(c cli.Conn) *application.Reconciler {
			res.conn = c
			return application.NewReconciler(workspace.NewService(s.Workspace()), datastore.NewService(s.Datastore()), layer.NewService(s.Layer()))
		},
	}
	res.code = cli.Run(env)
	res.out, res.errOut = out.String(), errOut.String()
	return res
}

func TestPlan_ExitCodes(t *testing.T) {
	s := memory.New()
	r := run(t, s, "", "plan")
	if r.code != cli.ExitDiff || !strings.Contains(r.out, "+ workspace demo") || !strings.Contains(r.out, "Plan: 3 a crear") {
		t.Fatalf("plan con diff: code=%d\n%s%s", r.code, r.out, r.errOut)
	}
	if len(s.Writes) != 0 {
		t.Fatalf("plan no debe escribir: %v", s.Writes)
	}

	if r := run(t, s, "", "apply", "--auto-approve"); r.code != cli.ExitOK {
		t.Fatalf("apply: %d\n%s%s", r.code, r.out, r.errOut)
	}
	if r := run(t, s, "", "plan"); r.code != cli.ExitOK || !strings.Contains(r.out, "Sin cambios") {
		t.Fatalf("plan sin diff: code=%d\n%s", r.code, r.out)
	}
}

func TestConnection_FlagsOverrideEnv(t *testing.T) {
	r := run(t, memory.New(), "", "plan", "--user", "bob")
	if r.conn.URL != "http://env:1/geoserver" || r.conn.User != "bob" || r.conn.Password != "envpass" {
		t.Errorf("conn: %+v", r.conn)
	}
	r = run(t, memory.New(), "", "plan", "--server", "http://flag", "--password", "p")
	if r.conn.URL != "http://flag" || r.conn.User != "admin" || r.conn.Password != "p" {
		t.Errorf("conn: %+v", r.conn)
	}
}

func TestApply_ConfirmationPrompt(t *testing.T) {
	s := memory.New()
	if r := run(t, s, "no\n", "apply"); r.code != cli.ExitError || len(s.Writes) != 0 {
		t.Fatalf("rechazar debe abortar sin escribir: code=%d writes=%v", r.code, s.Writes)
	}
	if r := run(t, s, "", "apply"); r.code != cli.ExitError || len(s.Writes) != 0 {
		t.Fatalf("EOF debe abortar: code=%d", r.code)
	}
	if r := run(t, s, "yes\n", "apply"); r.code != cli.ExitOK || len(s.Writes) != 3 {
		t.Fatalf("yes debe aplicar: code=%d writes=%v", r.code, s.Writes)
	}
}

func TestApply_PruneOffByDefault(t *testing.T) {
	s := memory.New()
	run(t, s, "", "apply", "--auto-approve")
	s.Layers["demo/pg/old"] = layer.Layer{Workspace: "demo", Store: "pg", Name: "old", SRS: "EPSG:4326"}

	if r := run(t, s, "", "apply", "--auto-approve"); r.code != cli.ExitOK {
		t.Fatalf("code %d", r.code)
	}
	if _, ok := s.Layers["demo/pg/old"]; !ok {
		t.Fatal("sin --prune no se debe borrar")
	}
	if r := run(t, s, "", "plan", "--prune"); r.code != cli.ExitDiff || !strings.Contains(r.out, "- layer demo/pg/old") {
		t.Fatalf("plan --prune: %d\n%s", r.code, r.out)
	}
	if r := run(t, s, "", "apply", "--prune", "--auto-approve"); r.code != cli.ExitOK {
		t.Fatalf("code %d", r.code)
	}
	if _, ok := s.Layers["demo/pg/old"]; ok {
		t.Fatal("con --prune se debe borrar")
	}
}

func TestValidate(t *testing.T) {
	if r := run(t, memory.New(), "", "validate"); r.code != cli.ExitOK || !strings.Contains(r.out, "1 workspace(s), 1 datastore(s), 1 capa(s)") {
		t.Fatalf("validate: %d\n%s%s", r.code, r.out, r.errOut)
	}
}

func TestInvalidUsage(t *testing.T) {
	for _, args := range [][]string{{"nope"}, {"plan", "--bogus"}, {"plan", "extra"}} {
		var errOut bytes.Buffer
		code := cli.Run(cli.Env{Args: args, Stdout: &bytes.Buffer{}, Stderr: &errOut, Getenv: func(string) string { return "" }})
		if code != cli.ExitInvalidArg {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if code := cli.Run(cli.Env{Stderr: &bytes.Buffer{}}); code != cli.ExitInvalidArg {
		t.Errorf("sin args: %d", code)
	}
}

func TestInvalidConfigExitCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(path, []byte("workspaces:\n  - name: 1x\n"), 0o600)
	var errOut bytes.Buffer
	code := cli.Run(cli.Env{Args: []string{"plan", "--file", path}, Stdout: &bytes.Buffer{}, Stderr: &errOut, Getenv: func(string) string { return "" }})
	if code != cli.ExitInvalidArg || !strings.Contains(errOut.String(), "workspaces[0].name") {
		t.Errorf("code %d: %s", code, errOut.String())
	}
}
