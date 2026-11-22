// Package memory ofrece repositorios en memoria que implementan los puertos
// del dominio. Se usan en tests (sin GeoServer ni Docker).
package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/neocaos/geoform/internal/domain/coverage"
	"github.com/neocaos/geoform/internal/domain/datastore"
	"github.com/neocaos/geoform/internal/domain/layer"
	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/style"
	"github.com/neocaos/geoform/internal/domain/workspace"
)

// Store agrupa los tres repositorios sobre el mismo estado y registra las
// operaciones de escritura en Writes (p. ej. "create workspace demo").
type Store struct {
	mu          sync.Mutex
	Workspaces  map[string]workspace.Workspace
	Datastores  map[string]datastore.Datastore // clave "ws/name"
	Layers      map[string]layer.Layer         // clave "ws/store/name"
	Styles      map[string]style.Style         // clave "ws/name"
	LayerGroups map[string]layergroup.Group    // clave "ws/name"
	// Raster (ver coverage.go).
	CoverageStores map[string]coverage.Store    // clave "ws/name"
	Coverages      map[string]coverage.Coverage // clave "ws/store/name"
	Writes         []string
	// FailOn hace fallar la escritura cuyo registro coincida (para tests).
	FailOn string
}

// New crea un Store vacío.
func New() *Store {
	return &Store{
		Workspaces:     map[string]workspace.Workspace{},
		Datastores:     map[string]datastore.Datastore{},
		Layers:         map[string]layer.Layer{},
		Styles:         map[string]style.Style{},
		LayerGroups:    map[string]layergroup.Group{},
		CoverageStores: map[string]coverage.Store{},
		Coverages:      map[string]coverage.Coverage{},
	}
}

func dsKey(ws, name string) string     { return ws + "/" + name }
func lyKey(ws, st, name string) string { return ws + "/" + st + "/" + name }
func (s *Store) write(op string) error {
	s.Writes = append(s.Writes, op)
	if s.FailOn == op {
		return errFail(op)
	}
	return nil
}

type errFail string

func (e errFail) Error() string { return "fallo simulado en " + string(e) }

// Workspace devuelve el repositorio de workspaces.
func (s *Store) Workspace() workspace.Repository { return wsRepo{s} }

// Datastore devuelve el repositorio de datastores.
func (s *Store) Datastore() datastore.Repository { return dsRepo{s} }

// Layer devuelve el repositorio de capas.
func (s *Store) Layer() layer.Repository { return lyRepo{s} }

type wsRepo struct{ s *Store }

func (r wsRepo) Get(_ context.Context, name string) (*workspace.Workspace, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	w, ok := r.s.Workspaces[name]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return &w, nil
}

func (r wsRepo) Create(_ context.Context, w workspace.Workspace) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.write("create workspace " + w.Name); err != nil {
		return err
	}
	r.s.Workspaces[w.Name] = w
	return nil
}

func (r wsRepo) Update(_ context.Context, w workspace.Workspace) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Workspaces[w.Name]; !ok {
		return workspace.ErrNotFound
	}
	if err := r.s.write("update workspace " + w.Name); err != nil {
		return err
	}
	r.s.Workspaces[w.Name] = w
	return nil
}

func (r wsRepo) Delete(_ context.Context, name string, _ bool) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Workspaces[name]; !ok {
		return workspace.ErrNotFound
	}
	if err := r.s.write("delete workspace " + name); err != nil {
		return err
	}
	delete(r.s.Workspaces, name)
	return nil
}

type dsRepo struct{ s *Store }

func (r dsRepo) Get(_ context.Context, ws, name string) (*datastore.Datastore, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	d, ok := r.s.Datastores[dsKey(ws, name)]
	if !ok {
		return nil, datastore.ErrNotFound
	}
	d.PostGIS.Password = "" // como GeoServer, no devuelve el password
	return &d, nil
}

func (r dsRepo) List(_ context.Context, ws string) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var names []string
	for _, d := range r.s.Datastores {
		if d.Workspace == ws {
			names = append(names, d.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r dsRepo) Create(_ context.Context, d datastore.Datastore) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.write("create datastore " + dsKey(d.Workspace, d.Name)); err != nil {
		return err
	}
	r.s.Datastores[dsKey(d.Workspace, d.Name)] = d
	return nil
}

func (r dsRepo) Update(_ context.Context, d datastore.Datastore) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Datastores[dsKey(d.Workspace, d.Name)]; !ok {
		return datastore.ErrNotFound
	}
	if err := r.s.write("update datastore " + dsKey(d.Workspace, d.Name)); err != nil {
		return err
	}
	r.s.Datastores[dsKey(d.Workspace, d.Name)] = d
	return nil
}

func (r dsRepo) Delete(_ context.Context, ws, name string, _ bool) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Datastores[dsKey(ws, name)]; !ok {
		return datastore.ErrNotFound
	}
	if err := r.s.write("delete datastore " + dsKey(ws, name)); err != nil {
		return err
	}
	delete(r.s.Datastores, dsKey(ws, name))
	for k, l := range r.s.Layers { // recurse: se llevan sus capas
		if l.Workspace == ws && l.Store == name {
			delete(r.s.Layers, k)
		}
	}
	return nil
}

type lyRepo struct{ s *Store }

func (r lyRepo) Get(_ context.Context, ws, st, name string) (*layer.Layer, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	l, ok := r.s.Layers[lyKey(ws, st, name)]
	if !ok {
		return nil, layer.ErrNotFound
	}
	return &l, nil
}

func (r lyRepo) List(_ context.Context, ws, st string) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var names []string
	for _, l := range r.s.Layers {
		if l.Workspace == ws && l.Store == st {
			names = append(names, l.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r lyRepo) Create(_ context.Context, l layer.Layer) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.write("create layer " + lyKey(l.Workspace, l.Store, l.Name)); err != nil {
		return err
	}
	r.s.Layers[lyKey(l.Workspace, l.Store, l.Name)] = l
	return nil
}

func (r lyRepo) Update(_ context.Context, l layer.Layer) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Layers[lyKey(l.Workspace, l.Store, l.Name)]; !ok {
		return layer.ErrNotFound
	}
	if err := r.s.write("update layer " + lyKey(l.Workspace, l.Store, l.Name)); err != nil {
		return err
	}
	r.s.Layers[lyKey(l.Workspace, l.Store, l.Name)] = l
	return nil
}

func (r lyRepo) Delete(_ context.Context, ws, st, name string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.Layers[lyKey(ws, st, name)]; !ok {
		return layer.ErrNotFound
	}
	if err := r.s.write("delete layer " + lyKey(ws, st, name)); err != nil {
		return err
	}
	delete(r.s.Layers, lyKey(ws, st, name))
	return nil
}
