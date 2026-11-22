package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/neocaos/geoform/internal/domain/coverage"
)

// WithCoverage habilita la gestión de almacenes raster (GeoTIFF) y coverages.
// Sin ella, un estado deseado con raster se rechaza al planificar.
func (r *Reconciler) WithCoverage(cv *coverage.Service) *Reconciler {
	r.cv = cv
	return r
}

// validateCoverages comprueba almacenes raster y coverages: reglas de dominio,
// duplicados, referencias y colisiones de nombre de capa dentro del workspace.
func (d Desired) validateCoverages(wss, lys map[string]bool) []error {
	var errs []error
	css := map[string]bool{}
	for _, cs := range d.CoverageStores {
		k := cs.Workspace + "/" + cs.Name
		if err := cs.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("coveragestore %q: %w", k, err))
		}
		if css[k] {
			errs = append(errs, fmt.Errorf("coveragestore %q duplicado", k))
		}
		css[k] = true
		if !wss[cs.Workspace] {
			errs = append(errs, fmt.Errorf("coveragestore %q: el workspace %q no está declarado", k, cs.Workspace))
		}
	}
	// Los nombres publicados (capas vectoriales y raster) son únicos por workspace.
	published := map[string]bool{}
	for _, l := range d.Layers {
		published[l.Workspace+":"+l.Name] = true
	}
	seen := map[string]bool{}
	for _, c := range d.Coverages {
		k := c.Workspace + "/" + c.Store + "/" + c.Name
		if err := c.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("coverage %q: %w", k, err))
		}
		if seen[k] {
			errs = append(errs, fmt.Errorf("coverage %q duplicada", k))
		}
		seen[k] = true
		if !css[c.Workspace+"/"+c.Store] {
			errs = append(errs, fmt.Errorf("coverage %q: el coveragestore %q no está declarado", k, c.Workspace+"/"+c.Store))
		}
		if published[c.Workspace+":"+c.Name] {
			errs = append(errs, fmt.Errorf("coverage %q: ya existe una capa vectorial con el nombre %q en el workspace", k, c.Name))
		}
		published[c.Workspace+":"+c.Name] = true
	}
	return errs
}

// planCoverages añade al plan los cambios de almacenes raster y coverages y
// devuelve los almacenes que ya existen.
func (r *Reconciler) planCoverages(ctx context.Context, want Desired, plan *Plan) (map[string]bool, error) {
	exists := map[string]bool{}
	if len(want.CoverageStores) == 0 && len(want.Coverages) == 0 {
		return exists, nil
	}
	if r.cv == nil {
		return nil, errors.New("el reconciliador no tiene habilitado el soporte raster (WithCoverage)")
	}
	for _, s := range want.CoverageStores {
		addr := s.Workspace + "/" + s.Name
		cur, err := r.cv.GetStore(ctx, s.Workspace, s.Name)
		switch {
		case errors.Is(err, coverage.ErrStoreNotFound):
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindCoverageStore, Address: addr, CoverageStore: s})
		case err != nil:
			return nil, fmt.Errorf("leyendo coveragestore %q: %w", addr, err)
		default:
			exists[addr] = true
			if cur.URL != s.URL {
				plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindCoverageStore, Address: addr, CoverageStore: s,
					Details: []string{fmt.Sprintf("url: %v -> %v", cur.URL, s.URL)}})
			}
		}
	}
	for _, c := range want.Coverages {
		addr := c.Workspace + "/" + c.Store + "/" + c.Name
		cur, err := r.cv.Get(ctx, c.Workspace, c.Store, c.Name)
		switch {
		case errors.Is(err, coverage.ErrNotFound):
			plan.Changes = append(plan.Changes, Change{Action: Create, Kind: KindCoverage, Address: addr, Coverage: c})
		case err != nil:
			return nil, fmt.Errorf("leyendo coverage %q: %w", addr, err)
		default:
			if diff := diffCoverage(*cur, c); len(diff) > 0 {
				plan.Changes = append(plan.Changes, Change{Action: Update, Kind: KindCoverage, Address: addr, Coverage: c, Details: diff})
			}
		}
	}
	return exists, nil
}

// pruneCoverages lista los almacenes raster y coverages sobrantes de los
// workspaces declarados que ya existen. Sin soporte raster no borra nada.
func (r *Reconciler) pruneCoverages(ctx context.Context, want Desired, wsExists, csExists map[string]bool) (covs, stores []Change, err error) {
	if r.cv == nil {
		return nil, nil, nil
	}
	wantCS := map[string]bool{}
	for _, s := range want.CoverageStores {
		wantCS[s.Workspace+"/"+s.Name] = true
	}
	wantCv := map[string]bool{}
	for _, c := range want.Coverages {
		wantCv[c.Workspace+"/"+c.Store+"/"+c.Name] = true
	}
	for _, w := range want.Workspaces {
		if !wsExists[w.Name] {
			continue
		}
		names, err := r.cv.ListStores(ctx, w.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("listando coveragestores de %q: %w", w.Name, err)
		}
		sort.Strings(names)
		for _, n := range names {
			addr := w.Name + "/" + n
			if !wantCS[addr] {
				stores = append(stores, Change{Action: Delete, Kind: KindCoverageStore, Address: addr,
					CoverageStore: coverage.Store{Workspace: w.Name, Name: n}})
				continue
			}
			if !csExists[addr] {
				continue
			}
			cnames, err := r.cv.List(ctx, w.Name, n)
			if err != nil {
				return nil, nil, fmt.Errorf("listando coverages de %q: %w", addr, err)
			}
			sort.Strings(cnames)
			for _, cn := range cnames {
				if !wantCv[addr+"/"+cn] {
					covs = append(covs, Change{Action: Delete, Kind: KindCoverage, Address: addr + "/" + cn,
						Coverage: coverage.Coverage{Workspace: w.Name, Store: n, Name: cn}})
				}
			}
		}
	}
	return covs, stores, nil
}

func (r *Reconciler) applyCoverage(ctx context.Context, c Change) error {
	if r.cv == nil {
		return errors.New("soporte raster no habilitado (WithCoverage)")
	}
	switch c.Kind {
	case KindCoverageStore:
		switch c.Action {
		case Create:
			_, err := r.cv.EnsureStore(ctx, c.CoverageStore)
			return err
		case Update:
			return r.cv.UpdateStore(ctx, c.CoverageStore)
		case Delete:
			return ignoreNotFound(r.cv.RemoveStore(ctx, c.CoverageStore.Workspace, c.CoverageStore.Name, true), coverage.ErrStoreNotFound)
		}
	case KindCoverage:
		switch c.Action {
		case Create:
			_, err := r.cv.Ensure(ctx, c.Coverage)
			return err
		case Update:
			return r.cv.Update(ctx, c.Coverage)
		case Delete:
			return ignoreNotFound(r.cv.Remove(ctx, c.Coverage.Workspace, c.Coverage.Store, c.Coverage.Name), coverage.ErrNotFound)
		}
	}
	return fmt.Errorf("cambio no soportado")
}

func diffCoverage(cur, want coverage.Coverage) []string {
	var out []string
	add := func(field, a, b string) {
		if a != b {
			out = append(out, fmt.Sprintf("%s: %q -> %q", field, a, b))
		}
	}
	// Un título vacío en la configuración significa "no gestionado".
	if want.Title != "" {
		add("title", cur.Title, want.Title)
	}
	add("srs", cur.SRS, want.SRS)
	return out
}
