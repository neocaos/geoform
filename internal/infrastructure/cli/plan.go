package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/neocaos/geoform/internal/application"
	"github.com/neocaos/geoform/internal/infrastructure/storage"
)

// prepare carga la configuración y calcula el plan; compartido por plan y apply.
func prepare(ctx context.Context, e Env, c *commonFlags, prune bool) (*application.Reconciler, *application.Plan, int) {
	desired, err := storage.LoadFile(c.file, e.Getenv)
	if err != nil {
		fmt.Fprintf(e.Stderr, "Configuración inválida:\n%v\n", err)
		return nil, nil, ExitInvalidArg
	}
	r := e.NewReconciler(c.conn())
	plan, err := r.Plan(ctx, desired, application.PlanOptions{Prune: prune})
	if err != nil {
		fmt.Fprintf(e.Stderr, "Error calculando el plan: %v\n", err)
		return nil, nil, ExitError
	}
	return r, plan, ExitOK
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func runPlan(e Env, args []string) int {
	fs, c := newFlagSet(e, "plan")
	c.addConnection(fs, e)
	prune := fs.Bool("prune", false, "incluye los borrados que haría apply --prune")
	if code, done := parse(fs, args); done {
		return code
	}
	ctx, stop := signalContext()
	defer stop()

	_, plan, code := prepare(ctx, e, c, *prune)
	if code != ExitOK {
		return code
	}
	printPlan(e.Stdout, plan)
	if plan.HasChanges() {
		return ExitDiff
	}
	return ExitOK
}

func printPlan(w io.Writer, p *application.Plan) {
	if !p.HasChanges() {
		fmt.Fprintln(w, "Sin cambios: GeoServer ya coincide con la configuración.")
		return
	}
	for _, c := range p.Changes {
		fmt.Fprintf(w, "%s %s %s\n", symbol(c.Action), c.Kind, c.Address)
		for _, d := range c.Details {
			fmt.Fprintf(w, "      %s\n", d)
		}
	}
	fmt.Fprintf(w, "\nPlan: %d a crear, %d a modificar, %d a borrar.\n",
		p.Count(application.Create), p.Count(application.Update), p.Count(application.Delete))
}

func symbol(a application.Action) string {
	switch a {
	case application.Create:
		return "+"
	case application.Update:
		return "~"
	default:
		return "-"
	}
}
