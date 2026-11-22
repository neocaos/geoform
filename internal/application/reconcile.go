// Package application contiene los casos de uso de Geoform. Reconciler es el
// motor de diff/apply: compara el estado deseado (Desired) con el real, leído a
// través de los servicios de dominio, y produce un Plan que luego se aplica.
package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/workspace"
)

// Desired es el estado deseado, independiente del formato de configuración.
// Los datastores y capas referencian a su padre por nombre.
type Desired struct {
	Workspaces []workspace.Workspace
	Datastores []datastore.Datastore
	Layers     []layer.Layer
}

// Validate aplica las reglas de dominio y comprueba duplicados y referencias.
// Devuelve todos los errores encontrados, no solo el primero.
func (d Desired) Validate() error {
	var errs []error
	wss := map[string]bool{}
	for _, w := range d.Workspaces {
		if err := w.Validate(); err != nil {
			errs = append(errs, err)
		}
		if wss[w.Name] {
			errs = append(errs, fmt.Errorf("workspace %q duplicado", w.Name))
		}
		wss[w.Name] = true
	}
	dss := map[string]bool{}
	for _, ds := range d.Datastores {
		if err := ds.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("datastore %q: %w", ds.Name, err))
		}
		k := ds.Workspace + "/" + ds.Name
		if dss[k] {
			errs = append(errs, fmt.Errorf("datastore %q duplicado", k))
		}
		dss[k] = true
		if !wss[ds.Workspace] {
			errs = append(errs, fmt.Errorf("datastore %q: el workspace %q no está declarado", k, ds.Workspace))
		}
	}
	lys := map[string]bool{}
	for _, l := range d.Layers {
		k := l.Workspace + "/" + l.Store + "/" + l.Name
		if err := l.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("capa %q: %w", k, err))
		}
		if lys[k] {
			errs = append(errs, fmt.Errorf("capa %q duplicada", k))
		}
		lys[k] = true
		if !dss[l.Workspace+"/"+l.Store] {
			errs = append(errs, fmt.Errorf("capa %q: el datastore %q no está declarado", k, l.Workspace+"/"+l.Store))
		}
	}
	return errors.Join(errs...)
}

// Action es el tipo de cambio de un recurso.
type Action string

const (
	Create Action = "create"
	Update Action = "update"
	Delete Action = "delete"
)

// Kind es el tipo de recurso.
type Kind string

const (
	KindWorkspace Kind = "workspace"
	KindDatastore Kind = "datastore"
	KindLayer     Kind = "layer"
)

// Change es un cambio concreto. Lleva el recurso deseado (en Create/Update)
// para que Apply ejecute exactamente lo que el plan mostró.
type Change struct {
	Action Action
	Kind   Kind
	// Address identifica el recurso: "ws", "ws/store" o "ws/store/layer".
	Address string
	// Details describe qué cambia en un Update ("campo: antes -> después").
	Details []string

	Workspace workspace.Workspace
	Datastore datastore.Datastore
	Layer     layer.Layer
}

// Plan es la lista ordenada de cambios necesarios; vacía si no hay diferencias.
type Plan struct{ Changes []Change }

// HasChanges indica si el plan modifica algo.
func (p Plan) HasChanges() bool { return len(p.Changes) > 0 }

// Count devuelve cuántos cambios hay de cada acción.
func (p Plan) Count(a Action) int {
	n := 0
	for _, c := range p.Changes {
		if c.Action == a {
			n++
		}
	}
	return n
}

// Reconciler calcula y aplica planes sobre los servicios de dominio.
type Reconciler struct {
	ws *workspace.Service
	ds *datastore.Service
	ly *layer.Service
}

// NewReconciler inyecta los servicios de dominio.
func NewReconciler(ws *workspace.Service, ds *datastore.Service, ly *layer.Service) *Reconciler {
	return &Reconciler{ws: ws, ds: ds, ly: ly}
}

// PlanOptions controla el cálculo del plan.
type PlanOptions struct {
	// Prune añade el borrado de datastores y capas que existen en los
	// workspaces declarados pero no figuran en el estado deseado. Nunca se
	// borran workspaces ni se toca ningún workspace no declarado.
	Prune bool
}

// Plan compara el estado deseado con el real sin escribir nada.
func (r *Reconciler) Plan(ctx context.Context, want Desired, opts PlanOptions) (*Plan, error) {
	want = normalize(want)
	if err := want.Validate(); err != nil {
		return nil, fmt.Errorf("estado deseado inválido: %w", err)
	}

	plan := &Plan{}
	var deletes []Change
	wsExists := map[string]bool{}
	dsExists := map[string]bool{}

	for _, w := range want.Workspaces {
		cur, err := r.ws.Get(ctx, w.Name)
		switch {
		case errors.Is(err, workspace.ErrNotFound):
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindWorkspace, Address: w.Name, Workspace: w})
		case err != nil:
			return nil, fmt.Errorf("leyendo workspace %q: %w", w.Name, err)
		default:
			wsExists[w.Name] = true
			if cur.Isolated != w.Isolated {
				plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindWorkspace, Address: w.Name, Workspace: w,
					Details: []string{fmt.Sprintf("isolated: %t -> %t", cur.Isolated, w.Isolated)}})
			}
		}
	}

	for _, d := range want.Datastores {
		addr := d.Workspace + "/" + d.Name
		cur, err := r.ds.Get(ctx, d.Workspace, d.Name)
		switch {
		case errors.Is(err, datastore.ErrNotFound):
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindDatastore, Address: addr, Datastore: d})
		case err != nil:
			return nil, fmt.Errorf("leyendo datastore %q: %w", addr, err)
		default:
			dsExists[addr] = true
			if diff := diffDatastore(*cur, d); len(diff) > 0 {
				plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindDatastore, Address: addr, Datastore: d, Details: diff})
			}
		}
	}

	for _, l := range want.Layers {
		addr := l.Workspace + "/" + l.Store + "/" + l.Name
		cur, err := r.ly.Get(ctx, l.Workspace, l.Store, l.Name)
		switch {
		case errors.Is(err, layer.ErrNotFound):
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindLayer, Address: addr, Layer: l})
		case err != nil:
			return nil, fmt.Errorf("leyendo capa %q: %w", addr, err)
		default:
			if diff := diffLayer(*cur, l); len(diff) > 0 {
				plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindLayer, Address: addr, Layer: l, Details: diff})
			}
		}
	}

	if opts.Prune {
		var err error
		if deletes, err = r.prune(ctx, want, wsExists, dsExists); err != nil {
			return nil, err
		}
	}
	plan.Changes = append(plan.Changes, deletes...)
	return plan, nil
}

// prune lista los recursos sobrantes dentro de los workspaces declarados que ya
// existen. Los borrados salen ordenados: capas primero, luego datastores.
func (r *Reconciler) prune(ctx context.Context, want Desired, wsExists, dsExists map[string]bool) ([]Change, error) {
	wantDS := map[string]bool{}
	for _, d := range want.Datastores {
		wantDS[d.Workspace+"/"+d.Name] = true
	}
	wantLy := map[string]bool{}
	for _, l := range want.Layers {
		wantLy[l.Workspace+"/"+l.Store+"/"+l.Name] = true
	}

	var layers, stores []Change
	for _, w := range want.Workspaces {
		if !wsExists[w.Name] {
			continue
		}
		names, err := r.ds.List(ctx, w.Name)
		if err != nil {
			return nil, fmt.Errorf("listando datastores de %q: %w", w.Name, err)
		}
		sort.Strings(names)
		for _, n := range names {
			addr := w.Name + "/" + n
			if !wantDS[addr] {
				stores = append(stores, Change{Action: Delete, Kind: KindDatastore, Address: addr,
					Datastore: datastore.Datastore{Workspace: w.Name, Name: n}})
				continue
			}
			if !dsExists[addr] {
				continue
			}
			lnames, err := r.ly.List(ctx, w.Name, n)
			if err != nil {
				return nil, fmt.Errorf("listando capas de %q: %w", addr, err)
			}
			sort.Strings(lnames)
			for _, ln := range lnames {
				if !wantLy[addr+"/"+ln] {
					layers = append(layers, Change{Action: Delete, Kind: KindLayer, Address: addr + "/" + ln,
						Layer: layer.Layer{Workspace: w.Name, Store: n, Name: ln}})
				}
			}
		}
	}
	return append(layers, stores...), nil
}

// Apply ejecuta los cambios del plan en orden. Se detiene en el primer error.
// progress, si no es nil, se invoca tras aplicar cada cambio con éxito.
// Borrar un recurso que ya no existe no es un error.
func (r *Reconciler) Apply(ctx context.Context, plan *Plan, progress func(Change)) error {
	for _, c := range plan.Changes {
		if err := r.applyOne(ctx, c); err != nil {
			return fmt.Errorf("%s %s %s: %w", c.Action, c.Kind, c.Address, err)
		}
		if progress != nil {
			progress(c)
		}
	}
	return nil
}

func (r *Reconciler) applyOne(ctx context.Context, c Change) error {
	switch c.Kind {
	case KindWorkspace:
		switch c.Action {
		case Create:
			_, err := r.ws.Ensure(ctx, c.Workspace)
			return err
		case Update:
			return r.ws.Update(ctx, c.Workspace)
		}
	case KindDatastore:
		switch c.Action {
		case Create:
			_, err := r.ds.Ensure(ctx, c.Datastore)
			return err
		case Update:
			return r.ds.Update(ctx, c.Datastore)
		case Delete:
			return ignoreNotFound(r.ds.Remove(ctx, c.Datastore.Workspace, c.Datastore.Name, true), datastore.ErrNotFound)
		}
	case KindLayer:
		switch c.Action {
		case Create:
			_, err := r.ly.Ensure(ctx, c.Layer)
			return err
		case Update:
			return r.ly.Update(ctx, c.Layer)
		case Delete:
			return ignoreNotFound(r.ly.Remove(ctx, c.Layer.Workspace, c.Layer.Store, c.Layer.Name), layer.ErrNotFound)
		}
	}
	return fmt.Errorf("cambio no soportado")
}

func ignoreNotFound(err, notFound error) error {
	if errors.Is(err, notFound) {
		return nil
	}
	return err
}

// normalize aplica los valores por defecto para que deseado y real sean
// comparables (GeoServer siempre devuelve puerto y schema).
func normalize(d Desired) Desired {
	out := Desired{
		Workspaces: append([]workspace.Workspace(nil), d.Workspaces...),
		Datastores: append([]datastore.Datastore(nil), d.Datastores...),
		Layers:     append([]layer.Layer(nil), d.Layers...),
	}
	for i := range out.Datastores {
		out.Datastores[i].PostGIS = normalizePostGIS(out.Datastores[i].PostGIS)
	}
	for i := range out.Layers {
		out.Layers[i].Table = out.Layers[i].NativeTable()
	}
	return out
}

func normalizePostGIS(p datastore.PostGIS) datastore.PostGIS {
	if p.Port == 0 {
		p.Port = 5432
	}
	if p.Schema == "" {
		p.Schema = "public"
	}
	return p
}

// diffDatastore compara los campos observables. El password no se compara:
// GeoServer no lo devuelve en claro, así que un cambio solo de password no se
// detecta (se aplica cuando cambia algo más).
func diffDatastore(cur, want datastore.Datastore) []string {
	c, w := normalizePostGIS(cur.PostGIS), want.PostGIS
	var out []string
	add := func(field string, a, b any) {
		if a != b {
			out = append(out, fmt.Sprintf("%s: %v -> %v", field, a, b))
		}
	}
	add("host", c.Host, w.Host)
	add("port", c.Port, w.Port)
	add("database", c.Database, w.Database)
	add("schema", c.Schema, w.Schema)
	add("user", c.User, w.User)
	return out
}

func diffLayer(cur, want layer.Layer) []string {
	var out []string
	add := func(field string, a, b string) {
		if a != b {
			out = append(out, fmt.Sprintf("%s: %q -> %q", field, a, b))
		}
	}
	add("table", cur.NativeTable(), want.NativeTable())
	// GeoServer rellena el título con el nombre si no se indica; un título
	// vacío en la configuración significa "no gestionado".
	if want.Title != "" {
		add("title", cur.Title, want.Title)
	}
	add("srs", cur.SRS, want.SRS)
	return out
}
