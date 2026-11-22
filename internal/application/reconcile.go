// Package application contiene los casos de uso de Geoform. Reconciler es el
// motor de diff/apply: compara el estado deseado (Desired) con el real, leído a
// través de los servicios de dominio, y produce un Plan que luego se aplica.
package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/neocaos/geoform/internal/domain/coverage"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/security"
	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/domain/workspace"
)

// Desired es el estado deseado, independiente del formato de configuración.
// Los datastores y capas referencian a su padre por nombre.
type Desired struct {
	Workspaces []workspace.Workspace
	Datastores []datastore.Datastore
	Layers     []layer.Layer
	// Security, si no es nil, activa la gestión de usuarios, roles y reglas.
	Security *SecurityDesired
	// Estilos y grupos de capas, con alcance de workspace.
	Styles      []style.Style
	LayerGroups []layergroup.Group
	// Almacenes raster y sus coverages (requieren Reconciler.WithCoverage).
	CoverageStores []coverage.Store
	Coverages      []coverage.Coverage
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
	if d.Security != nil {
		errs = append(errs, d.Security.validate(wss)...)
	}
	errs = append(errs, d.validateStylesAndGroups(wss, lys)...)
	errs = append(errs, d.validateCoverages(wss, lys)...)
	return errors.Join(errs...)
}

// validateStylesAndGroups valida estilos, grupos y las referencias a estilos
// desde las capas. wss son los workspaces declarados y lys las capas.
func (d Desired) validateStylesAndGroups(wss, lys map[string]bool) []error {
	var errs []error
	sts := map[string]bool{}
	for _, st := range d.Styles {
		k := st.Workspace + "/" + st.Name
		if err := st.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("estilo %q: %w", k, err))
		}
		if sts[k] {
			errs = append(errs, fmt.Errorf("estilo %q duplicado", k))
		}
		sts[k] = true
		if !wss[st.Workspace] {
			errs = append(errs, fmt.Errorf("estilo %q: el workspace %q no está declarado", k, st.Workspace))
		}
	}
	// Una referencia "ws:nombre" al propio workspace de la capa debe estar
	// declarada; un nombre sin prefijo no declarado se asume global.
	for _, l := range d.Layers {
		if ws, n, ok := strings.Cut(l.Style, ":"); ok && ws == l.Workspace && !sts[ws+"/"+n] {
			errs = append(errs, fmt.Errorf("capa %q: el estilo %q no está declarado", l.Workspace+"/"+l.Store+"/"+l.Name, l.Style))
		}
	}
	// Nombre de capa -> existe en el workspace (para los grupos).
	inWS := map[string]bool{}
	for _, l := range d.Layers {
		inWS[l.Workspace+"/"+l.Name] = true
	}
	for _, c := range d.Coverages {
		inWS[c.Workspace+"/"+c.Name] = true
	}
	grps := map[string]bool{}
	for _, g := range d.LayerGroups {
		k := g.Workspace + "/" + g.Name
		if err := g.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("grupo %q: %w", k, err))
		}
		if grps[k] {
			errs = append(errs, fmt.Errorf("grupo %q duplicado", k))
		}
		grps[k] = true
		if !wss[g.Workspace] {
			errs = append(errs, fmt.Errorf("grupo %q: el workspace %q no está declarado", k, g.Workspace))
		}
		for _, ln := range g.Layers {
			if !inWS[g.Workspace+"/"+ln] {
				errs = append(errs, fmt.Errorf("grupo %q: la capa %q no está declarada en el workspace", k, ln))
			}
		}
	}
	return errs
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
	KindWorkspace  Kind = "workspace"
	KindDatastore  Kind = "datastore"
	KindLayer      Kind = "layer"
	KindStyle      Kind = "style"
	KindLayerGroup Kind = "layergroup"
	KindRole       Kind = "role"
	KindUser       Kind = "user"
	KindRule       Kind = "rule"
	// Raster: almacenes (GeoTIFF) y sus coverages.
	KindCoverageStore Kind = "coveragestore"
	KindCoverage      Kind = "coverage"
)

// Change es un cambio concreto. Lleva el recurso deseado (en Create/Update)
// para que Apply ejecute exactamente lo que el plan mostró.
type Change struct {
	Action Action
	Kind   Kind
	// Address identifica el recurso: "ws", "ws/store", "ws/store/layer",
	// "ws/style" o "ws/layergroup".
	Address string
	// Details describe qué cambia en un Update ("campo: antes -> después").
	Details []string

	Workspace  workspace.Workspace
	Datastore  datastore.Datastore
	Layer      layer.Layer
	Style      style.Style
	LayerGroup layergroup.Group
	Role       string
	User       security.User
	Rule       security.Rule

	CoverageStore coverage.Store
	Coverage      coverage.Coverage
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
	ws  *workspace.Service
	ds  *datastore.Service
	ly  *layer.Service
	sec *security.Service
	// Opcionales: ver WithStyles y WithLayerGroups.
	st *style.Service
	lg *layergroup.Service
	cv *coverage.Service // opcional: sin él no se gestiona raster
}

// NewReconciler inyecta los servicios de dominio.
func NewReconciler(ws *workspace.Service, ds *datastore.Service, ly *layer.Service) *Reconciler {
	return &Reconciler{ws: ws, ds: ds, ly: ly}
}

// WithStyles habilita la gestión de estilos y devuelve el propio Reconciler.
func (r *Reconciler) WithStyles(st *style.Service) *Reconciler { r.st = st; return r }

// WithLayerGroups habilita la gestión de grupos de capas.
func (r *Reconciler) WithLayerGroups(lg *layergroup.Service) *Reconciler { r.lg = lg; return r }

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

	if len(want.Styles) > 0 && r.st == nil {
		return nil, errors.New("la configuración declara estilos pero el Reconciler no tiene servicio de estilos")
	}
	if len(want.LayerGroups) > 0 && r.lg == nil {
		return nil, errors.New("la configuración declara grupos de capas pero el Reconciler no tiene servicio de grupos")
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

	// Los estilos van antes que las capas, que pueden referenciarlos.
	for _, st := range want.Styles {
		addr := st.Workspace + "/" + st.Name
		cur, err := r.st.Get(ctx, st.Workspace, st.Name)
		switch {
		case errors.Is(err, style.ErrNotFound):
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindStyle, Address: addr, Style: st})
		case err != nil:
			return nil, fmt.Errorf("leyendo estilo %q: %w", addr, err)
		default:
			if !style.SameSLD(cur.SLD, st.SLD) {
				plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindStyle, Address: addr, Style: st,
					Details: []string{"sld: contenido modificado"}})
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

	// Antes de los grupos: pueden incluir coverages.
	csExists, err := r.planCoverages(ctx, want, plan)
	if err != nil {
		return nil, err
	}

	for _, g := range want.LayerGroups {
		addr := g.Workspace + "/" + g.Name
		cur, err := r.lg.Get(ctx, g.Workspace, g.Name)
		switch {
		case errors.Is(err, layergroup.ErrNotFound):
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindLayerGroup, Address: addr, LayerGroup: g})
		case err != nil:
			return nil, fmt.Errorf("leyendo grupo %q: %w", addr, err)
		default:
			if diff := diffLayerGroup(*cur, g); len(diff) > 0 {
				plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindLayerGroup, Address: addr, LayerGroup: g, Details: diff})
			}
		}
	}

	var rasterLayers, rasterStores []Change
	if opts.Prune {
		if deletes, err = r.prune(ctx, want, wsExists, dsExists); err != nil {
			return nil, err
		}
		if rasterLayers, rasterStores, err = r.pruneCoverages(ctx, want, wsExists, csExists); err != nil {
			return nil, err
		}
	}
	secDeletes, err := r.planSecurity(ctx, want, opts.Prune, plan)
	if err != nil {
		return nil, err
	}
	deletes = append(deletes, secDeletes...)
	// Orden de borrado: primero lo vectorial (grupos incluidos, que pueden
	// referenciar coverages), luego las coverages y por último sus almacenes.
	plan.Changes = append(plan.Changes, deletes...)
	plan.Changes = append(plan.Changes, rasterLayers...)
	plan.Changes = append(plan.Changes, rasterStores...)
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

	var layers, stores, groups, styles []Change
	for _, w := range want.Workspaces {
		if !wsExists[w.Name] {
			continue
		}
		extraG, extraS, err := r.pruneStylesAndGroups(ctx, want, w.Name)
		if err != nil {
			return nil, err
		}
		groups, styles = append(groups, extraG...), append(styles, extraS...)
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
	// Orden: grupos, capas, estilos (ya sin capas que los usen), datastores.
	out := append(groups, layers...)
	out = append(out, styles...)
	return append(out, stores...), nil
}

// pruneStylesAndGroups lista los estilos y grupos sobrantes de un workspace.
func (r *Reconciler) pruneStylesAndGroups(ctx context.Context, want Desired, ws string) (groups, styles []Change, err error) {
	if r.lg != nil {
		wantG := map[string]bool{}
		for _, g := range want.LayerGroups {
			wantG[g.Workspace+"/"+g.Name] = true
		}
		names, err := r.lg.List(ctx, ws)
		if err != nil {
			return nil, nil, fmt.Errorf("listando grupos de %q: %w", ws, err)
		}
		sort.Strings(names)
		for _, n := range names {
			if !wantG[ws+"/"+n] {
				groups = append(groups, Change{Action: Delete, Kind: KindLayerGroup, Address: ws + "/" + n,
					LayerGroup: layergroup.Group{Workspace: ws, Name: n}})
			}
		}
	}
	if r.st != nil {
		wantS := map[string]bool{}
		for _, s := range want.Styles {
			wantS[s.Workspace+"/"+s.Name] = true
		}
		names, err := r.st.List(ctx, ws)
		if err != nil {
			return nil, nil, fmt.Errorf("listando estilos de %q: %w", ws, err)
		}
		sort.Strings(names)
		for _, n := range names {
			if !wantS[ws+"/"+n] {
				styles = append(styles, Change{Action: Delete, Kind: KindStyle, Address: ws + "/" + n,
					Style: style.Style{Workspace: ws, Name: n}})
			}
		}
	}
	return groups, styles, nil
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
	if handled, err := r.applySecurity(ctx, c); handled {
		return err
	}
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
	case KindCoverageStore, KindCoverage:
		return r.applyCoverage(ctx, c)
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
	case KindStyle:
		switch c.Action {
		case Create:
			_, err := r.st.Ensure(ctx, c.Style)
			return err
		case Update:
			return r.st.Update(ctx, c.Style)
		case Delete:
			return ignoreNotFound(r.st.Remove(ctx, c.Style.Workspace, c.Style.Name), style.ErrNotFound)
		}
	case KindLayerGroup:
		switch c.Action {
		case Create:
			_, err := r.lg.Ensure(ctx, c.LayerGroup)
			return err
		case Update:
			return r.lg.Update(ctx, c.LayerGroup)
		case Delete:
			return ignoreNotFound(r.lg.Remove(ctx, c.LayerGroup.Workspace, c.LayerGroup.Name), layergroup.ErrNotFound)
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
		Security:   d.Security,
		Styles:     append([]style.Style(nil), d.Styles...),

		CoverageStores: append([]coverage.Store(nil), d.CoverageStores...),
		Coverages:      append([]coverage.Coverage(nil), d.Coverages...),
	}
	for _, g := range d.LayerGroups {
		g.Mode = g.EffectiveMode()
		out.LayerGroups = append(out.LayerGroups, g)
	}
	declared := map[string]bool{}
	for _, st := range d.Styles {
		declared[st.Workspace+"/"+st.Name] = true
	}
	for i := range out.Datastores {
		out.Datastores[i].Type = out.Datastores[i].Kind()
		if !out.Datastores[i].IsFile() {
			out.Datastores[i].PostGIS = normalizePostGIS(out.Datastores[i].PostGIS)
		}
	}
	for i := range out.Layers {
		out.Layers[i].Table = out.Layers[i].NativeTable()
		// Un nombre sin prefijo que coincide con un estilo del workspace se
		// refiere a ese estilo; si no, a uno global.
		if l := out.Layers[i]; l.Style != "" && !strings.Contains(l.Style, ":") && declared[l.Workspace+"/"+l.Style] {
			out.Layers[i].Style = l.Workspace + ":" + l.Style
		}
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
	var out []string
	add := func(field string, a, b any) {
		if a != b {
			out = append(out, fmt.Sprintf("%s: %v -> %v", field, a, b))
		}
	}
	if cur.Kind() != want.Kind() {
		add("type", cur.Kind(), want.Kind())
		if want.IsFile() {
			add("file", cur.File, want.File)
			return out
		}
	} else if want.IsFile() {
		add("file", cur.File, want.File)
		return out
	}
	c, w := normalizePostGIS(cur.PostGIS), want.PostGIS
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
	// Estilo vacío = no gestionado.
	if want.Style != "" {
		add("style", cur.Style, want.Style)
	}
	return out
}

func diffLayerGroup(cur, want layergroup.Group) []string {
	var out []string
	if cur.EffectiveMode() != want.EffectiveMode() {
		out = append(out, fmt.Sprintf("mode: %q -> %q", cur.EffectiveMode(), want.EffectiveMode()))
	}
	// Título vacío = no gestionado.
	if want.Title != "" && cur.Title != want.Title {
		out = append(out, fmt.Sprintf("title: %q -> %q", cur.Title, want.Title))
	}
	if !reflect.DeepEqual(cur.Layers, want.Layers) {
		out = append(out, fmt.Sprintf("layers: %v -> %v", cur.Layers, want.Layers))
	}
	return out
}
