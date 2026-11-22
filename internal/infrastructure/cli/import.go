package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/neocaos/geoform/internal/infrastructure/importer"
	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

// header se antepone al YAML generado.
const importHeader = `# Generado por 'geoform import'. Los passwords no se leen de GeoServer: define
# cada variable password_env antes de ejecutar plan/apply.
`

func runImport(e Env, args []string) int {
	fs, c := newFlagSet(e, "import")
	c.addConnection(fs, e)
	out := fs.String("output", "-", "fichero de salida ('-' = stdout)")
	force := fs.Bool("force", false, "sobrescribe el fichero de salida si existe")
	only := fs.String("workspace", "", "lista de workspaces separados por comas (por defecto todos)")
	// --file no aplica a import; se usa --output.
	if code, done := parse(fs, args); done {
		return code
	}
	var filter []string
	for _, w := range strings.Split(*only, ",") {
		if w = strings.TrimSpace(w); w != "" {
			filter = append(filter, w)
		}
	}
	if *out != "-" && !*force {
		if _, err := os.Stat(*out); err == nil {
			fmt.Fprintf(e.Stderr, "%s ya existe; usa --force para sobrescribirlo\n", *out)
			return ExitInvalidArg
		}
	}

	ctx, stop := signalContext()
	defer stop()
	conn := c.conn()
	if code := checkConn(e, conn); code != ExitOK {
		return code
	}
	var src importer.Source
	if e.NewSource != nil {
		src = e.NewSource(conn)
	} else {
		client, _ := newClient(conn) // validado en checkConn
		src = client
	}
	res, err := importer.Import(ctx, src, filter)
	if err != nil {
		fmt.Fprintf(e.Stderr, "Error importando: %v\n", err)
		return ExitError
	}
	data, err := storage.Marshal(res.Desired)
	if err != nil {
		fmt.Fprintf(e.Stderr, "Error generando el YAML: %v\n", err)
		return ExitError
	}
	data = append([]byte(importHeader), data...)
	for _, w := range res.Warnings {
		fmt.Fprintln(e.Stderr, "aviso:", w)
	}
	if *out == "-" {
		_, _ = e.Stdout.Write(data)
		return ExitOK
	}
	if err := os.WriteFile(*out, data, 0o600); err != nil {
		fmt.Fprintf(e.Stderr, "Error escribiendo %s: %v\n", *out, err)
		return ExitError
	}
	fmt.Fprintf(e.Stderr, "Importado: %d workspace(s), %d datastore(s), %d capa(s) -> %s\n",
		len(res.Desired.Workspaces), len(res.Desired.Datastores), len(res.Desired.Layers), *out)
	return ExitOK
}
