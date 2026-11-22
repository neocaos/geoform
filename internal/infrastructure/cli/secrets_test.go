package cli_test

import (
	"bytes"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/neocaos/geoform/internal/infrastructure/cli"
)

// Secretos de prueba: ninguno debe aparecer jamás en stdout, stderr ni ficheros generados.
const (
	secRESTPass = "rest-S3cr3t-pw"
	secPGPass   = "pg-S3cr3t-pw"
	secUserPass = "user-S3cr3t-pw"
)

const secConfig = `
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
security:
  roles: [ROLE_A]
  users:
    - name: alice
      password_env: ALICE_PASSWORD
      roles: [ROLE_A]
`

// leakyGeoServer simula un GeoServer que, ante cualquier escritura, responde 500
// repitiendo el cuerpo de la petición y las cabeceras (peor caso de eco).
func leakyGeoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("fallo interno; petición: " + string(body) + " auth: " + r.Header.Get("Authorization") + " url: " + r.URL.String()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/workspaces/demo.json"):
			_, _ = w.Write([]byte(`{"workspace":{"name":"demo","isolated":false}}`))
		case strings.HasSuffix(r.URL.Path, "/security/usergroup/users.json"):
			_, _ = w.Write([]byte(`{"users":[]}`))
		case strings.HasSuffix(r.URL.Path, "/security/roles.json"):
			_, _ = w.Write([]byte(`{"roles":[]}`))
		case strings.HasSuffix(r.URL.Path, "/security/acl/layers.json"):
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runSecrets(t *testing.T, cfg string, args ...string) (int, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "geoform.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := cli.Run(cli.Env{
		Args:  append([]string{args[0], "--file", path}, args[1:]...),
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut,
		Getenv: func(k string) string {
			return map[string]string{"PG_PASSWORD": secPGPass, "ALICE_PASSWORD": secUserPass, "GEOSERVER_PASSWORD": secRESTPass}[k]
		},
		NewReconciler: cli.GeoServerReconciler,
	})
	all := out.String() + errOut.String()
	assertNoSecrets(t, all)
	return code, all
}

var basicAuth = regexp.MustCompile(`Basic [A-Za-z0-9+/=]{8,}`)

func assertNoSecrets(t *testing.T, s string) {
	t.Helper()
	for _, sec := range []string{secRESTPass, secPGPass, secUserPass} {
		if strings.Contains(s, sec) {
			t.Fatalf("la salida contiene un secreto (%q):\n%s", sec, s)
		}
	}
	// Tampoco la cabecera Basic ni el base64 de las credenciales REST.
	if basicAuth.MatchString(s) {
		t.Fatalf("la salida contiene una cabecera Authorization:\n%s", s)
	}
}

func TestSecrets_ApplyFailureDoesNotEchoPasswords(t *testing.T) {
	srv := leakyGeoServer(t)
	url := srv.URL + "/geoserver"

	// Fallo creando el datastore (el cuerpo lleva el password de PostGIS).
	code, out := runSecrets(t, secConfig, "apply", "--auto-approve", "--server", url, "--password", secRESTPass)
	if code != cli.ExitError || !strings.Contains(out, "500") {
		t.Fatalf("se esperaba fallo con 500: %d\n%s", code, out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("el eco de GeoServer debe quedar redactado, no omitido:\n%s", out)
	}
}

func TestSecrets_UserCreationFailureDoesNotEchoPassword(t *testing.T) {
	srv := leakyGeoServer(t)
	cfg := `
workspaces:
  - name: demo
security:
  users:
    - name: alice
      password_env: ALICE_PASSWORD
`
	code, out := runSecrets(t, cfg, "apply", "--auto-approve", "--server", srv.URL+"/geoserver", "--password", secRESTPass)
	if code != cli.ExitError {
		t.Fatalf("se esperaba fallo: %d\n%s", code, out)
	}
}

func TestSecrets_PlanSuccessAndInvalidConfig(t *testing.T) {
	srv := leakyGeoServer(t)
	code, out := runSecrets(t, secConfig, "plan", "--server", srv.URL+"/geoserver")
	if code != cli.ExitDiff {
		t.Fatalf("plan: %d\n%s", code, out)
	}
	// Valor incorrecto en un campo: el error de YAML no debe citar valores.
	bad := strings.Replace(secConfig, "host: db", "host: [pg-S3cr3t-pw]\n        port: pg-S3cr3t-pw", 1)
	if code, out := runSecrets(t, bad, "validate"); code != cli.ExitInvalidArg {
		t.Fatalf("validate: %d\n%s", code, out)
	}
	// Variable de entorno ausente: se nombra la variable, no su valor.
	if code, out := runSecrets(t, strings.Replace(secConfig, "ALICE_PASSWORD", "MISSING_VAR", 1), "plan", "--server", srv.URL+"/geoserver"); code != cli.ExitInvalidArg || !strings.Contains(out, "MISSING_VAR") {
		t.Fatalf("env ausente: %d\n%s", code, out)
	}
}

func TestSecrets_ImportFailureAndSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error; auth: " + r.Header.Get("Authorization") + " " + secRESTPass))
	}))
	defer srv.Close()
	code, out := runSecrets(t, secConfig, "import", "--server", srv.URL+"/geoserver", "--password", secRESTPass)
	if code != cli.ExitError {
		t.Fatalf("import: %d\n%s", code, out)
	}
}

func TestConn_RejectsCredentialsInURL(t *testing.T) {
	code, out := runSecrets(t, secConfig, "plan", "--server", "http://admin:"+secRESTPass+"@127.0.0.1:1/geoserver")
	if code != cli.ExitInvalidArg || !strings.Contains(out, "credenciales") {
		t.Fatalf("se esperaba rechazo: %d\n%s", code, out)
	}
}

func TestConn_WarnsOnPlainHTTPNonLoopback(t *testing.T) {
	// Servidor remoto inexistente: el aviso sale antes de intentar conectar.
	_, out := runSecrets(t, secConfig, "plan", "--server", "http://geoserver.example.invalid:1/geoserver")
	if !strings.Contains(out, "http sin cifrar") {
		t.Fatalf("falta el aviso de http:\n%s", out)
	}
	_, out = runSecrets(t, secConfig, "plan", "--server", "http://127.0.0.1:1/geoserver")
	if strings.Contains(out, "http sin cifrar") {
		t.Fatalf("loopback no debe avisar:\n%s", out)
	}
}

func TestConn_TLSOptionsAreExplicit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Por defecto el certificado autofirmado se rechaza.
	code, out := runSecrets(t, secConfig, "plan", "--server", srv.URL+"/geoserver")
	if code != cli.ExitError || !strings.Contains(out, "certificate") {
		t.Fatalf("sin flags debe fallar la verificación TLS: %d\n%s", code, out)
	}
	// --insecure-skip-tls-verify avisa y permite continuar.
	_, out = runSecrets(t, secConfig, "plan", "--server", srv.URL+"/geoserver", "--insecure-skip-tls-verify")
	if !strings.Contains(out, "desactiva la verificación") || strings.Contains(out, "certificate") {
		t.Fatalf("insecure: %s", out)
	}
	// --ca-file con la CA del servidor funciona sin desactivar la verificación.
	pemPath := filepath.Join(t.TempDir(), "ca.pem")
	data := pemEncode(srv.Certificate().Raw)
	if err := os.WriteFile(pemPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, out = runSecrets(t, secConfig, "plan", "--server", srv.URL+"/geoserver", "--ca-file", pemPath)
	if strings.Contains(out, "certificate") || strings.Contains(out, "desactiva") {
		t.Fatalf("ca-file: %s", out)
	}
	// Un --ca-file inválido es un error de uso.
	if code, _ := runSecrets(t, secConfig, "plan", "--server", srv.URL+"/geoserver", "--ca-file", filepath.Join(t.TempDir(), "no.pem")); code != cli.ExitInvalidArg {
		t.Fatalf("ca-file inexistente: %d", code)
	}
}

func pemEncode(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
