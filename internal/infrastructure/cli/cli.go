// Package cli implementa la línea de comandos de Geoform (validate, plan,
// apply) sobre application.Reconciler.
package cli

import (
	"flag"
	"fmt"
	"io"
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
type Conn struct{ URL, User, Password string }

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
	client := gs.NewClient(c.URL, c.User, c.Password)
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
	file, server, user, password string
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
}

func (c *commonFlags) conn() Conn { return Conn{URL: c.server, User: c.user, Password: c.password} }

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
