//go:build integration

package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gs "github.com/neocaos/geoform/internal/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/cli"
	"github.com/neocaos/geoform/internal/testenv"
)

// Pruebas end-to-end de seguridad: tras aplicar roles, usuarios y reglas se
// comprueba que GeoServer las HACE CUMPLIR en los servicios OWS (WMS/WFS), no
// solo que estén guardadas.

const (
	e2eWS   = "geoform_sec_e2e"
	e2eView = "ROLE_E2E_VIEWER"
	e2eOth  = "ROLE_E2E_OTHER"
	e2eAnal = "ROLE_E2E_ANALYST"
)

const e2eConfig = `
workspaces:
  - name: geoform_sec_e2e
    datastores:
      - name: postgis
        host: db
        port: 5432
        database: geoform
        schema: geodata
        user: geouser
        password_env: E2E_PG_PASSWORD
        layers:
          - name: cities
            srs: EPSG:4326
          - name: regions
            srs: EPSG:4326
security:
  roles: [ROLE_E2E_VIEWER, ROLE_E2E_OTHER, ROLE_E2E_ANALYST]
  users:
    - name: e2e_alice
      password_env: %[1]s
      roles: [ROLE_E2E_VIEWER]
    - name: e2e_bob
      password_env: E2E_BOB_PW
      roles: [ROLE_E2E_OTHER]
    - name: e2e_carol
      password_env: E2E_CAROL_PW
      roles: [ROLE_E2E_ANALYST]
  rules:
    - workspace: geoform_sec_e2e
      access: read
      roles: [ROLE_E2E_VIEWER, ROLE_E2E_ANALYST]
    - workspace: geoform_sec_e2e
      layer: regions
      access: read
      roles: [ROLE_E2E_ANALYST]
`

type e2eEnv struct {
	t      *testing.T
	client *gs.Client
	url    string
	path   string
	passes map[string]string
}

func (e *e2eEnv) run(args ...string) (int, string) {
	e.t.Helper()
	var out, errOut bytes.Buffer
	full := append(args, "--file", e.path, "--server", e.url, "--user", testenv.User())
	code := cli.Run(cli.Env{
		Stdin: strings.NewReader(""), Args: full, Stdout: &out, Stderr: &errOut,
		Getenv: func(k string) string {
			if k == "GEOSERVER_PASSWORD" {
				return testenv.Pass()
			}
			return e.passes[k]
		},
		NewReconciler: cli.GeoServerReconciler,
	})
	all := out.String() + errOut.String()
	for _, pw := range e.passes {
		if strings.Contains(all, pw) {
			e.t.Fatalf("la salida contiene una contraseña (%s)", args[0])
		}
	}
	e.t.Logf("geoform %s -> %d\n%s", strings.Join(args, " "), code, all)
	return code, all
}

func (e *e2eEnv) writeCfg(aliceEnv string) {
	e.t.Helper()
	if err := os.WriteFile(e.path, []byte(fmt.Sprintf(e2eConfig, aliceEnv)), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// get hace una petición HTTP con basic auth opcional (user == "" => anónimo).
func (e *e2eEnv) get(user, pass, path string, q url.Values) (int, string) {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.url+path+"?"+q.Encode(), nil)
	if err != nil {
		e.t.Fatal(err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b)
}

func wfsFeature(layer string) url.Values {
	return url.Values{"service": {"WFS"}, "version": {"2.0.0"}, "request": {"GetFeature"},
		"typeNames": {e2eWS + ":" + layer}, "count": {"1"}, "outputFormat": {"application/json"}}
}

func wmsMap(layer string) url.Values {
	return url.Values{"service": {"WMS"}, "version": {"1.1.1"}, "request": {"GetMap"}, "layers": {e2eWS + ":" + layer},
		"bbox": {"-10,35,5,45"}, "width": {"64"}, "height": {"64"}, "srs": {"EPSG:4326"}, "format": {"image/png"}}
}

func capabilities(svc string) url.Values {
	return url.Values{"service": {svc}, "request": {"GetCapabilities"}}
}

// eventually repite check hasta que devuelva "" (GeoServer puede tardar un
// instante en aplicar cambios de seguridad) o se agote el plazo.
func eventually(t *testing.T, what string, check func() string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last string
	for {
		if last = check(); last == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %s", what, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (e *e2eEnv) expectStatus(user, pass, path string, q url.Values, want ...int) {
	e.t.Helper()
	eventually(e.t, fmt.Sprintf("%s %s?%s como %q", "GET", path, q.Get("request"), user), func() string {
		code, body := e.get(user, pass, path, q)
		for _, w := range want {
			if code == w {
				return ""
			}
		}
		return fmt.Sprintf("status %d (esperado %v): %.200s", code, want, body)
	})
}

func (e *e2eEnv) setCatalogMode(mode string) {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodPut, e.url+"/rest/security/acl/catalog", strings.NewReader(`{"mode":"`+mode+`"}`))
	req.SetBasicAuth(testenv.User(), testenv.Pass())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("catalog mode %s: %d", mode, resp.StatusCode)
	}
}

func TestIntegration_SecurityEnforcement(t *testing.T) {
	ctx := context.Background()
	// GeoServer cachea las autenticaciones (usuario+contraseña) durante un rato
	// aunque el usuario se borre y recree; un sufijo único por ejecución evita
	// falsos positivos entre ejecuciones consecutivas sobre el mismo stack.
	nonce := fmt.Sprintf("%x", time.Now().UnixNano()%0xffffffff)
	e := &e2eEnv{
		t: t, url: testenv.URL(), path: filepath.Join(t.TempDir(), "geoform.yaml"),
		client: gs.NewClient(testenv.URL(), testenv.User(), testenv.Pass()),
		passes: map[string]string{
			"E2E_PG_PASSWORD": "geopassword", "E2E_ALICE_PW": "e2e-Alice-pw-1-" + nonce, "E2E_ALICE_PW2": "e2e-Alice-pw-2-rotated-" + nonce,
			"E2E_BOB_PW": "e2e-Bob-pw-1-" + nonce, "E2E_CAROL_PW": "e2e-Carol-pw-1-" + nonce,
		},
	}

	cleanup := func() {
		for _, k := range []string{e2eWS + ".*.r", e2eWS + ".regions.r", e2eWS + ".old.r"} {
			_ = e.client.DeleteLayerRule(ctx, k)
		}
		for _, u := range []string{"e2e_alice", "e2e_bob", "e2e_carol", "e2e_extra"} {
			_ = e.client.DeleteUser(ctx, u)
		}
		for _, r := range []string{e2eView, e2eOth, e2eAnal} {
			_ = e.client.DeleteRole(ctx, r)
		}
		_ = e.client.DeleteWorkspace(ctx, e2eWS, true)
	}
	cleanup()
	t.Cleanup(cleanup)
	t.Cleanup(func() { e.setCatalogMode("HIDE") })

	alice, bob, carol := "e2e_alice", "e2e_bob", "e2e_carol"
	_ = nonce
	e.writeCfg("E2E_ALICE_PW")
	if code, out := e.run("plan"); code != cli.ExitDiff || !strings.Contains(out, "+ user "+alice) {
		t.Fatalf("plan inicial: %d", code)
	}
	if code, _ := e.run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatalf("apply: %d", code)
	}
	if code, out := e.run("plan"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras apply debe ser vacío: %d", code)
	}

	// --- Modo CHALLENGE: el acceso denegado se manifiesta como 401/403.
	e.setCatalogMode("CHALLENGE")
	apw, bpw, cpw := e.passes["E2E_ALICE_PW"], e.passes["E2E_BOB_PW"], e.passes["E2E_CAROL_PW"]
	for _, svc := range []struct {
		name string
		q    url.Values
		path string
	}{
		{"WFS GetFeature", wfsFeature("cities"), "/" + e2eWS + "/wfs"},
		{"WMS GetMap", wmsMap("cities"), "/" + e2eWS + "/wms"},
		{"WFS GetFeature global", wfsFeature("cities"), "/wfs"},
	} {
		t.Run("challenge/"+svc.name, func(t *testing.T) {
			e.t = t
			e.expectStatus("", "", svc.path, svc.q, 401, 403)                    // anónimo
			e.expectStatus(alice, "mala", svc.path, svc.q, 401)                  // credenciales incorrectas
			e.expectStatus(bob, bpw, svc.path, svc.q, 401, 403)                  // autenticado sin rol
			e.expectStatus(alice, apw, svc.path, svc.q, 200)                     // rol correcto
			e.expectStatus(carol, cpw, svc.path, svc.q, 200)                     // otro rol permitido
			e.expectStatus(testenv.User(), testenv.Pass(), svc.path, svc.q, 200) // admin
		})
	}
	e.t = t
	// Regla específica de capa: alice (VIEWER) no lee 'regions', carol (ANALYST) sí.
	e.expectStatus(alice, apw, "/"+e2eWS+"/wfs", wfsFeature("regions"), 401, 403)
	e.expectStatus(carol, cpw, "/"+e2eWS+"/wfs", wfsFeature("regions"), 200)
	e.expectStatus("", "", "/"+e2eWS+"/wfs", wfsFeature("regions"), 401, 403)

	// El contenido servido al autorizado son datos reales.
	if _, body := e.get(alice, apw, "/"+e2eWS+"/wfs", wfsFeature("cities")); !strings.Contains(body, `"features"`) {
		t.Fatalf("alice debía recibir features: %.200s", body)
	}

	// --- Modo HIDE (por defecto): la capa restringida no se lista ni se sirve a anónimos.
	e.setCatalogMode("HIDE")
	for _, svc := range []string{"WMS", "WFS"} {
		path := "/" + e2eWS + "/" + strings.ToLower(svc)
		eventually(t, svc+" GetCapabilities anónimo", func() string {
			code, body := e.get("", "", path, capabilities(svc))
			if code == 200 && strings.Contains(body, "cities") {
				return "el anónimo ve la capa restringida"
			}
			return ""
		})
		eventually(t, svc+" GetCapabilities alice", func() string {
			code, body := e.get(alice, apw, path, capabilities(svc))
			if code != 200 || !strings.Contains(body, "cities") {
				return fmt.Sprintf("alice no ve la capa: %d", code)
			}
			return ""
		})
	}
	eventually(t, "GetFeature anónimo en HIDE", func() string {
		code, body := e.get("", "", "/"+e2eWS+"/wfs", wfsFeature("cities"))
		if code == 200 && strings.Contains(body, `"features"`) {
			return "el anónimo recibe datos"
		}
		return ""
	})
	e.setCatalogMode("CHALLENGE")

	// --- Rotación de contraseña vía password_env distinto.
	e.writeCfg("E2E_ALICE_PW2")
	if code, out := e.run("plan"); code != cli.ExitDiff || !strings.Contains(out, "password: rotar") {
		t.Fatalf("la rotación debe aparecer en el plan: %d\n%s", code, out)
	}
	if code, _ := e.run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatal("apply rotación")
	}
	apw2 := e.passes["E2E_ALICE_PW2"]
	e.expectStatus(alice, apw2, "/"+e2eWS+"/wfs", wfsFeature("cities"), 200)
	// OJO: no se afirma que la contraseña antigua de alice falle aquí. GeoServer
	// cachea las autenticaciones correctas (también tras rotar, desactivar o
	// borrar al usuario) hasta que caduca su caché; ver docs/testing.md. El
	// rechazo de la contraseña antigua se prueba con un usuario nunca autenticado
	// antes de rotar (TestIntegration_PasswordRotationRejectsOldPassword).
	if code, out := e.run("plan"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras rotar: %d", code)
	}
	if code, out := e.run("apply", "--auto-approve"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("re-apply debe ser no-op: %d\n%s", code, out)
	}

	// --- prune: usuario, rol y regla fuera de la configuración.
	if err := e.client.CreateUser(ctx, gs.User{Name: "e2e_extra", Password: "e2e-Extra-pw-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.client.CreateLayerRule(ctx, e2eWS+".old.r", []string{e2eView}); err != nil {
		t.Fatal(err)
	}
	if code, out := e.run("plan", "--prune"); code != cli.ExitDiff || !strings.Contains(out, "- user e2e_extra") || !strings.Contains(out, e2eWS+".old") {
		t.Fatalf("plan --prune: %d\n%s", code, out)
	}
	if code, _ := e.run("apply", "--prune", "--auto-approve"); code != cli.ExitOK {
		t.Fatal("apply --prune")
	}
	if _, err := e.client.GetUser(ctx, "e2e_extra"); !gs.IsNotFound(err) {
		t.Fatalf("e2e_extra debía borrarse: %v", err)
	}
	if rules, _ := e.client.LayerRules(ctx); rules[e2eWS+".old.r"] != "" {
		t.Fatalf("la regla sobrante debía borrarse: %v", rules)
	}
	if code, out := e.run("apply", "--prune", "--auto-approve"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("re-apply --prune debe ser no-op: %d", code)
	}
	// El acceso de los usuarios declarados sigue intacto tras el prune.
	e.expectStatus(alice, apw2, "/"+e2eWS+"/wfs", wfsFeature("cities"), 200)
	e.expectStatus("", "", "/"+e2eWS+"/wfs", wfsFeature("cities"), 401, 403)
}

const e2eRotationConfig = `
workspaces:
  - name: geoform_sec_e2e
security:
  roles: [ROLE_E2E_VIEWER]
  users:
    - name: e2e_dave
      password_env: %s
      roles: [ROLE_E2E_VIEWER]
  rules:
    - workspace: geoform_sec_e2e
      access: read
      roles: [ROLE_E2E_VIEWER]
`

// Rotación de contraseña con password_env distinto: el usuario entra con la
// nueva y no con la antigua. El usuario no se autentica nunca antes de rotar
// para que la caché de autenticación de GeoServer no enmascare el resultado.
func TestIntegration_PasswordRotationRejectsOldPassword(t *testing.T) {
	ctx := context.Background()
	nonce := fmt.Sprintf("%x", time.Now().UnixNano()%0xffffffff)
	e := &e2eEnv{
		t: t, url: testenv.URL(), path: filepath.Join(t.TempDir(), "geoform.yaml"),
		client: gs.NewClient(testenv.URL(), testenv.User(), testenv.Pass()),
		passes: map[string]string{"E2E_DAVE_PW1": "e2e-Dave-pw-1-" + nonce, "E2E_DAVE_PW2": "e2e-Dave-pw-2-" + nonce},
	}
	cleanup := func() {
		_ = e.client.DeleteLayerRule(ctx, e2eWS+".*.r")
		_ = e.client.DeleteUser(ctx, "e2e_dave")
		_ = e.client.DeleteRole(ctx, e2eView)
		_ = e.client.DeleteWorkspace(ctx, e2eWS, true)
	}
	cleanup()
	t.Cleanup(cleanup)
	t.Cleanup(func() { e.setCatalogMode("HIDE") })
	e.setCatalogMode("CHALLENGE")

	write := func(env string) {
		if err := os.WriteFile(e.path, []byte(fmt.Sprintf(e2eRotationConfig, env)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("E2E_DAVE_PW1")
	if code, _ := e.run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatal("apply inicial")
	}
	write("E2E_DAVE_PW2")
	if code, out := e.run("plan"); code != cli.ExitDiff || !strings.Contains(out, "password: rotar") {
		t.Fatalf("la rotación debe aparecer en el plan: %d\n%s", code, out)
	}
	if code, _ := e.run("apply", "--auto-approve"); code != cli.ExitOK {
		t.Fatal("apply rotación")
	}
	// Con el stack aún sin workspace publicado, la ruta REST basta para validar login:
	// 403 = credenciales válidas sin ser admin, 401 = credenciales rechazadas.
	status := func(pw string) int {
		code, _ := e.get("e2e_dave", pw, "/rest/about/version.json", url.Values{})
		return code
	}
	eventually(t, "contraseña nueva", func() string {
		if c := status(e.passes["E2E_DAVE_PW2"]); c != 403 {
			return fmt.Sprintf("la contraseña nueva da %d", c)
		}
		return ""
	})
	if c := status(e.passes["E2E_DAVE_PW1"]); c != 401 {
		t.Fatalf("la contraseña antigua debe rechazarse (401), dio %d", c)
	}
	if code, out := e.run("plan"); code != cli.ExitOK || !strings.Contains(out, "Sin cambios") {
		t.Fatalf("plan tras rotar: %d", code)
	}
}
