// Comando geoform: CLI independiente para gestionar GeoServer desde un
// fichero YAML (validate, plan, apply).
package main

import (
	"os"

	"github.com/neocaos/geoform/internal/infrastructure/cli"
)

func main() {
	os.Exit(cli.Run(cli.DefaultEnv(os.Args[1:])))
}
