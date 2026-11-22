package memory

import (
	"context"
	"sort"

	"github.com/neocaos/geoform/internal/domain/coverage"
)

// Coverage devuelve el repositorio de almacenes raster y coverages.
func (s *Store) Coverage() coverage.Repository { return cvRepo{s} }

type cvRepo struct{ s *Store }

func (r cvRepo) GetStore(_ context.Context, ws, name string) (*coverage.Store, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	st, ok := r.s.CoverageStores[dsKey(ws, name)]
	if !ok {
		return nil, coverage.ErrStoreNotFound
	}
	return &st, nil
}

func (r cvRepo) ListStores(_ context.Context, ws string) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out []string
	for _, st := range r.s.CoverageStores {
		if st.Workspace == ws {
			out = append(out, st.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r cvRepo) CreateStore(_ context.Context, st coverage.Store) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.write("create coveragestore " + dsKey(st.Workspace, st.Name)); err != nil {
		return err
	}
	r.s.CoverageStores[dsKey(st.Workspace, st.Name)] = st
	return nil
}

func (r cvRepo) UpdateStore(_ context.Context, st coverage.Store) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	k := dsKey(st.Workspace, st.Name)
	if _, ok := r.s.CoverageStores[k]; !ok {
		return coverage.ErrStoreNotFound
	}
	if err := r.s.write("update coveragestore " + k); err != nil {
		return err
	}
	r.s.CoverageStores[k] = st
	return nil
}

func (r cvRepo) DeleteStore(_ context.Context, ws, name string, _ bool) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	k := dsKey(ws, name)
	if _, ok := r.s.CoverageStores[k]; !ok {
		return coverage.ErrStoreNotFound
	}
	if err := r.s.write("delete coveragestore " + k); err != nil {
		return err
	}
	delete(r.s.CoverageStores, k)
	for ck, c := range r.s.Coverages {
		if c.Workspace == ws && c.Store == name {
			delete(r.s.Coverages, ck)
		}
	}
	return nil
}

func (r cvRepo) Get(_ context.Context, ws, st, name string) (*coverage.Coverage, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	c, ok := r.s.Coverages[lyKey(ws, st, name)]
	if !ok {
		return nil, coverage.ErrNotFound
	}
	return &c, nil
}

func (r cvRepo) List(_ context.Context, ws, st string) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out []string
	for _, c := range r.s.Coverages {
		if c.Workspace == ws && c.Store == st {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r cvRepo) Create(_ context.Context, c coverage.Coverage) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.write("create coverage " + lyKey(c.Workspace, c.Store, c.Name)); err != nil {
		return err
	}
	r.s.Coverages[lyKey(c.Workspace, c.Store, c.Name)] = c
	return nil
}

func (r cvRepo) Update(_ context.Context, c coverage.Coverage) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	k := lyKey(c.Workspace, c.Store, c.Name)
	if _, ok := r.s.Coverages[k]; !ok {
		return coverage.ErrNotFound
	}
	if err := r.s.write("update coverage " + k); err != nil {
		return err
	}
	r.s.Coverages[k] = c
	return nil
}

func (r cvRepo) Delete(_ context.Context, ws, st, name string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	k := lyKey(ws, st, name)
	if _, ok := r.s.Coverages[k]; !ok {
		return coverage.ErrNotFound
	}
	if err := r.s.write("delete coverage " + k); err != nil {
		return err
	}
	delete(r.s.Coverages, k)
	return nil
}
