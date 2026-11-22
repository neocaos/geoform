package cli

import (
	"fmt"

	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

func runValidate(e Env, args []string) int {
	fs, c := newFlagSet(e, "validate")
	checkEnv := fs.Bool("check-env", false, "exige que las variables de password_env estén definidas")
	if code, done := parse(fs, args); done {
		return code
	}
	getenv := e.Getenv
	if !*checkEnv {
		// Validar la forma del fichero no requiere los secretos.
		getenv = func(string) string { return "-" }
	}
	d, err := storage.LoadFile(c.file, getenv)
	if err != nil {
		fmt.Fprintf(e.Stderr, "Configuración inválida:\n%v\n", err)
		return ExitInvalidArg
	}
	fmt.Fprintf(e.Stdout, "Configuración válida: %d workspace(s), %d datastore(s), %d capa(s).\n", len(d.Workspaces), len(d.Datastores), len(d.Layers))
	return ExitOK
}
