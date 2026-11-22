// Package cli implementa la línea de comandos de Geoform (validate, plan,
// apply) sobre application.Reconciler.
package cli

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/domain/coverage"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/security"
	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/domain/workspace"
	gs "github.com/neocaos/geoform/internal/geoserver"
	gsrepo "github.com/neocaos/geoform/internal/infrastructure/geoserver"
	"github.com/neocaos/geoform/internal/infrastructure/importer"
)

// Códigos de salida.
const (
	ExitOK         = 0 // éxito (plan sin cambios, apply completado, validate correcto)
	ExitError      = 1 // fallo en tiempo de ejecución (red, GeoServer, apply abortado)
	ExitDiff       = 2 // solo plan: hay cambios pendientes
	ExitInvalidArg = 3 // configuración inválida o uso incorrecto de la CLI
)

// Conn son los datos de conexión a GeoServer.
type Conn struct {
	URL, User, Password string
	// CAFile es un bundle PEM de CAs adicionales (solo si se pide con --ca-file).
	CAFile string
	// InsecureTLS desactiva la verificación del certificado (solo con --insecure-skip-tls-verify).
	InsecureTLS bool
}

// Env agrupa las dependencias del entorno, inyectables en tests.
type Env struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// NewReconciler construye el reconciliador para una conexión.
	NewReconciler func(Conn) *application.Reconciler
	// NewSource construye el lector de GeoServer para import; nil usa el cliente REST.
	NewSource func(Conn) importer.Source
}

// DefaultEnv devuelve el entorno real del proceso.
func DefaultEnv(args []string) Env {
	return Env{Args: args, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, NewReconciler: GeoServerReconciler}
}

// GeoServerReconciler conecta los servicios de dominio al cliente REST.
func GeoServerReconciler(c Conn) *application.Reconciler {
	client, _ := newClient(c) // la conexión ya se validó en checkConn
	return application.NewReconciler(
		workspace.NewService(gsrepo.NewWorkspaceRepository(client)),
		datastore.NewService(gsrepo.NewDatastoreRepository(client)),
		layer.NewService(gsrepo.NewLayerRepository(client)),
	).
		WithSecurity(security.NewService(gsrepo.NewSecurityRepository(client))).
		WithStyles(style.NewService(gsrepo.NewStyleRepository(client))).
		WithLayerGroups(layergroup.NewService(gsrepo.NewLayerGroupRepository(client))).
		WithCoverage(coverage.NewService(gsrepo.NewCoverageRepository(client)))
}

// newClient crea el cliente REST aplicando las opciones TLS explícitas.
func newClient(c Conn) (*gs.Client, error) {
	var opts []gs.Option
	if c.CAFile != "" || c.InsecureTLS {
		tc := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.InsecureTLS} // #nosec G402 -- solo se activa con --insecure-skip-tls-verify explícito (con aviso)
		if c.CAFile != "" {
			pem, err := os.ReadFile(c.CAFile)
			if err != nil {
				return nil, fmt.Errorf("leyendo --ca-file: %w", err)
			}
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("--ca-file %s no contiene certificados PEM válidos", c.CAFile)
			}
			tc.RootCAs = pool
		}
		opts = append(opts, gs.WithTLSConfig(tc))
	}
	return gs.NewClient(c.URL, c.User, c.Password, opts...), nil
}

// checkConn valida la conexión antes de usarla: rechaza credenciales dentro de
// la URL (acabarían en logs e historial), avisa del uso de http fuera de
// localhost y de la verificación TLS desactivada. Devuelve ExitOK si se puede seguir.
func checkConn(e Env, c Conn) int {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		// No se imprime la URL: podría contener credenciales.
		fmt.Fprintln(e.Stderr, "URL de GeoServer inválida: debe ser http(s)://host[:puerto]/geoserver")
		return ExitInvalidArg
	}
	if u.User != nil {
		fmt.Fprintln(e.Stderr, "La URL de GeoServer no puede incluir credenciales (user:pass@): usa --user y GEOSERVER_PASSWORD.")
		return ExitInvalidArg
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		fmt.Fprintf(e.Stderr, "aviso: %s usa http sin cifrar; la contraseña REST viaja en claro por la red. Usa https.\n", u.Host)
	}
	if c.InsecureTLS {
		fmt.Fprintln(e.Stderr, "aviso: --insecure-skip-tls-verify desactiva la verificación del certificado (vulnerable a MITM).")
	}
	if _, err := newClient(c); err != nil {
		fmt.Fprintln(e.Stderr, err)
		return ExitInvalidArg
	}
	return ExitOK
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

const usage = `Uso: geoform <comando> [opciones]

Comandos:
  validate   Comprueba el fichero de configuración (no contacta con GeoServer)
  plan       Muestra los cambios necesarios sin aplicarlos
  apply      Aplica los cambios (idempotente)
  import     Lee un GeoServer existente y genera el YAML (--output, --workspace, --force)

Opciones comunes (plan, apply):
  --file      Fichero YAML (por defecto geoform.yaml)
  --server    URL de GeoServer             [GEOSERVER_URL, por defecto http://localhost:8080/geoserver]
  --user      Usuario de la API REST       [GEOSERVER_USERNAME, por defecto admin]
  --password  Contraseña de la API REST    [GEOSERVER_PASSWORD]; mejor por entorno que por flag
  --ca-file   Bundle PEM con CAs adicionales para https [GEOSERVER_CA_FILE]
  --insecure-skip-tls-verify  No verifica el certificado https (inseguro; solo si se pide explícitamente)

La URL no admite credenciales (user:pass@). Con http fuera de localhost se emite un aviso.

Opciones de apply:
  --prune         Borra datastores, almacenes raster, capas, estilos, grupos y, si hay sección security, usuarios, roles y reglas sobrantes
  --auto-approve  No pide confirmación

Códigos de salida: 0 ok / plan sin cambios, 1 error, 2 plan con cambios, 3 configuración o uso inválido.
`

// Run ejecuta la CLI y devuelve el código de salida.
func Run(e Env) int {
	if len(e.Args) == 0 {
		fmt.Fprint(e.Stderr, usage)
		return ExitInvalidArg
	}
	switch cmd, args := e.Args[0], e.Args[1:]; cmd {
	case "validate":
		return runValidate(e, args)
	case "plan":
		return runPlan(e, args)
	case "apply":
		return runApply(e, args)
	case "import":
		return runImport(e, args)
	case "help", "-h", "--help":
		fmt.Fprint(e.Stdout, usage)
		return ExitOK
	default:
		fmt.Fprintf(e.Stderr, "comando desconocido %q\n\n%s", cmd, usage)
		return ExitInvalidArg
	}
}

// commonFlags son las opciones compartidas por plan y apply.
type commonFlags struct {
	file, server, user, password, caFile string
	insecure                             bool
}

func newFlagSet(e Env, name string) (*flag.FlagSet, *commonFlags) {
	fs := flag.NewFlagSet("geoform "+name, flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	c := &commonFlags{}
	fs.StringVar(&c.file, "file", "geoform.yaml", "fichero de configuración YAML")
	return fs, c
}

func (c *commonFlags) addConnection(fs *flag.FlagSet, e Env) {
	def := func(key, fallback string) string {
		if v := e.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	fs.StringVar(&c.server, "server", def("GEOSERVER_URL", "http://localhost:8080/geoserver"), "URL de GeoServer (GEOSERVER_URL)")
	fs.StringVar(&c.user, "user", def("GEOSERVER_USERNAME", "admin"), "usuario REST (GEOSERVER_USERNAME)")
	fs.StringVar(&c.password, "password", e.Getenv("GEOSERVER_PASSWORD"), "contraseña REST (GEOSERVER_PASSWORD)")
	fs.StringVar(&c.caFile, "ca-file", e.Getenv("GEOSERVER_CA_FILE"), "bundle PEM con CAs adicionales (GEOSERVER_CA_FILE)")
	fs.BoolVar(&c.insecure, "insecure-skip-tls-verify", false, "no verifica el certificado TLS (inseguro)")
}

func (c *commonFlags) conn() Conn {
	return Conn{URL: c.server, User: c.user, Password: c.password, CAFile: c.caFile, InsecureTLS: c.insecure}
}

// parse devuelve (código, true) si hay que terminar ya.
func parse(fs *flag.FlagSet, args []string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return ExitOK, true
		}
		return ExitInvalidArg, true
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(fs.Output(), "argumentos inesperados: %v\n", fs.Args())
		return ExitInvalidArg, true
	}
	return 0, false
}
