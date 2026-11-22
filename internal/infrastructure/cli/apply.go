package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/neocaos/geoform/internal/application"
)

func runApply(e Env, args []string) int {
	fs, c := newFlagSet(e, "apply")
	c.addConnection(fs, e)
	prune := fs.Bool("prune", false, "borra datastores y capas sobrantes de los workspaces declarados")
	auto := fs.Bool("auto-approve", false, "no pide confirmación")
	if code, done := parse(fs, args); done {
		return code
	}
	ctx, stop := signalContext()
	defer stop()

	r, plan, code := prepare(ctx, e, c, *prune)
	if code != ExitOK {
		return code
	}
	printPlan(e.Stdout, plan)
	if !plan.HasChanges() {
		return ExitOK
	}

	if !*auto {
		fmt.Fprint(e.Stdout, "\n¿Aplicar estos cambios? Escribe 'yes' para continuar: ")
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Fprintln(e.Stderr, "Apply cancelado: no se ha modificado nada.")
			return ExitError
		}
	}

	applied := 0
	err := r.Apply(ctx, plan, func(ch application.Change) {
		applied++
		fmt.Fprintf(e.Stdout, "%s %s %s: hecho\n", symbol(ch.Action), ch.Kind, ch.Address)
	})
	if err != nil {
		fmt.Fprintf(e.Stderr, "\nError: %v\nAplicados %d de %d cambios; vuelve a ejecutar apply para continuar (es idempotente).\n", err, applied, len(plan.Changes))
		return ExitError
	}
	fmt.Fprintf(e.Stdout, "\nApply completado: %d cambio(s).\n", applied)
	return ExitOK
}
