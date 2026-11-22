package memory

import (
	"context"
	"sort"

	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/style"
)

// Style devuelve el repositorio de estilos.
func (s *Store) Style() style.Repository { return stRepo{s} }

// LayerGroup devuelve el repositorio de grupos de capas.
func (s *Store) LayerGroup() layergroup.Repository { return lgRepo{s} }

type stRepo struct{ s *Store }

func (r stRepo) Get(_ context.Context, ws, name string) (*style.Style, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	st, ok := r.s.Styles[dsKey(ws, name)]
	if !ok {
		return nil, style.ErrNotFound
	}
	return &st, nil
}

func (r stRepo) List(_ context.Context, ws string) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var names []string
	for _, st := range r.s.Styles {
		if st.Workspace == ws {
			names = append(names, st.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r stRepo) Create(_ context.Context, st style.Style) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.write("create style " + dsKey(st.Workspace, st.Name)); err != nil {
		return err
	}
	r.s.Styles[dsKey(st.Workspace, st.Name)] = st
	return nil
}

func (r stRepo) Update(_ context.Context, st style.Style) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Styles[dsKey(st.Workspace, st.Name)]; !ok {
		return style.ErrNotFound
	}
	if err := r.s.write("update style " + dsKey(st.Workspace, st.Name)); err != nil {
		return err
	}
	r.s.Styles[dsKey(st.Workspace, st.Name)] = st
	return nil
}

func (r stRepo) Delete(_ context.Context, ws, name string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Styles[dsKey(ws, name)]; !ok {
		return style.ErrNotFound
	}
	if err := r.s.write("delete style " + dsKey(ws, name)); err != nil {
		return err
	}
	delete(r.s.Styles, dsKey(ws, name))
	return nil
}

type lgRepo struct{ s *Store }

func (r lgRepo) Get(_ context.Context, ws, name string) (*layergroup.Group, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	g, ok := r.s.LayerGroups[dsKey(ws, name)]
	if !ok {
		return nil, layergroup.ErrNotFound
	}
	g.Mode = g.EffectiveMode()
	return &g, nil
}

func (r lgRepo) List(_ context.Context, ws string) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var names []string
	for _, g := range r.s.LayerGroups {
		if g.Workspace == ws {
			names = append(names, g.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r lgRepo) Create(_ context.Context, g layergroup.Group) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.write("create layergroup " + dsKey(g.Workspace, g.Name)); err != nil {
		return err
	}
	r.s.LayerGroups[dsKey(g.Workspace, g.Name)] = g
	return nil
}

func (r lgRepo) Update(_ context.Context, g layergroup.Group) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.LayerGroups[dsKey(g.Workspace, g.Name)]; !ok {
		return layergroup.ErrNotFound
	}
	if err := r.s.write("update layergroup " + dsKey(g.Workspace, g.Name)); err != nil {
		return err
	}
	r.s.LayerGroups[dsKey(g.Workspace, g.Name)] = g
	return nil
}

func (r lgRepo) Delete(_ context.Context, ws, name string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.LayerGroups[dsKey(ws, name)]; !ok {
		return layergroup.ErrNotFound
	}
	if err := r.s.write("delete layergroup " + dsKey(ws, name)); err != nil {
		return err
	}
	delete(r.s.LayerGroups, dsKey(ws, name))
	return nil
}
