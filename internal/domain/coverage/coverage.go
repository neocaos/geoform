// Package coverage modela los almacenes raster (coverage stores) de un
// workspace y las capas raster (coverages) publicadas desde ellos.
package coverage

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Errores de "no encontrado" del almacén y de la coverage.
var (
	ErrStoreNotFound = errors.New("coveragestore: no encontrado")
	ErrNotFound      = errors.New("coverage: no encontrada")
)

// StoreType es el formato raster de un coverage store.
type StoreType string

// TypeGeoTIFF es el único formato raster soportado por ahora.
const TypeGeoTIFF StoreType = "geotiff"

// Store es un origen raster publicado en un workspace.
type Store struct {
	Workspace string
	Name      string
	Type      StoreType // vacío equivale a GeoTIFF
	// URL del fichero en GeoServer, p. ej. "file:/data/dem.tif".
	URL string
}

// Kind devuelve el tipo efectivo: el valor vacío es GeoTIFF.
func (s Store) Kind() StoreType {
	if s.Type == "" {
		return TypeGeoTIFF
	}
	return s.Type
}

// Validate aplica las reglas mínimas del almacén.
func (s Store) Validate() error {
	switch {
	case s.Workspace == "":
		return errors.New("coveragestore: el workspace no puede estar vacío")
	case s.Name == "":
		return errors.New("coveragestore: el nombre no puede estar vacío")
	case s.Kind() != TypeGeoTIFF:
		return fmt.Errorf("coveragestore: tipo %q no soportado", s.Type)
	case strings.TrimSpace(s.URL) == "":
		return errors.New("coveragestore: url es obligatoria")
	}
	return nil
}

// Coverage es una capa raster publicada desde un Store.
type Coverage struct {
	Workspace string
	Store     string
	Name      string // nombre publicado
	// Native es el nombre de la cobertura dentro del fichero; si está vacío se usa Name.
	Native string
	Title  string
	SRS    string // ej. "EPSG:4326"
}

// NativeName devuelve el nombre de origen.
func (c Coverage) NativeName() string {
	if c.Native != "" {
		return c.Native
	}
	return c.Name
}

// Validate aplica las reglas mínimas de la coverage.
func (c Coverage) Validate() error {
	switch {
	case c.Workspace == "" || c.Store == "":
		return errors.New("coverage: workspace y coveragestore son obligatorios")
	case c.Name == "":
		return errors.New("coverage: el nombre no puede estar vacío")
	case c.SRS == "":
		return errors.New("coverage: el SRS es obligatorio")
	}
	return nil
}

// Repository abstrae la persistencia (implementada en infrastructure).
type Repository interface {
	GetStore(ctx context.Context, workspace, name string) (*Store, error)
	ListStores(ctx context.Context, workspace string) ([]string, error)
	CreateStore(ctx context.Context, s Store) error
	UpdateStore(ctx context.Context, s Store) error
	DeleteStore(ctx context.Context, workspace, name string, recurse bool) error

	Get(ctx context.Context, workspace, store, name string) (*Coverage, error)
	List(ctx context.Context, workspace, store string) ([]string, error)
	Create(ctx context.Context, c Coverage) error
	Update(ctx context.Context, c Coverage) error
	Delete(ctx context.Context, workspace, store, name string) error
}

// Service contiene la lógica de negocio sobre almacenes raster y coverages.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// EnsureStore crea el almacén si no existe (idempotente).
func (s *Service) EnsureStore(ctx context.Context, st Store) (*Store, error) {
	if err := st.Validate(); err != nil {
		return nil, fmt.Errorf("service: coveragestore inválido: %w", err)
	}
	existing, err := s.repo.GetStore(ctx, st.Workspace, st.Name)
	if err != nil && !errors.Is(err, ErrStoreNotFound) {
		return nil, fmt.Errorf("service: error consultando coveragestore %q: %w", st.Name, err)
	}
	if existing != nil {
		return existing, nil
	}
	if err := s.repo.CreateStore(ctx, st); err != nil {
		return nil, fmt.Errorf("service: error creando coveragestore %q: %w", st.Name, err)
	}
	return s.repo.GetStore(ctx, st.Workspace, st.Name)
}

// GetStore obtiene un almacén por workspace y nombre.
func (s *Service) GetStore(ctx context.Context, workspace, name string) (*Store, error) {
	return s.repo.GetStore(ctx, workspace, name)
}

// ListStores devuelve los nombres de los almacenes de un workspace.
func (s *Service) ListStores(ctx context.Context, workspace string) ([]string, error) {
	return s.repo.ListStores(ctx, workspace)
}

// UpdateStore aplica los cambios de st sobre un almacén existente.
func (s *Service) UpdateStore(ctx context.Context, st Store) error {
	if err := st.Validate(); err != nil {
		return fmt.Errorf("service: coveragestore inválido: %w", err)
	}
	if err := s.repo.UpdateStore(ctx, st); err != nil {
		return fmt.Errorf("service: error actualizando coveragestore %q: %w", st.Name, err)
	}
	return nil
}

// RemoveStore elimina el almacén (recurse incluye sus coverages).
func (s *Service) RemoveStore(ctx context.Context, workspace, name string, recurse bool) error {
	if err := s.repo.DeleteStore(ctx, workspace, name, recurse); err != nil {
		return fmt.Errorf("service: error eliminando coveragestore %q: %w", name, err)
	}
	return nil
}

// Ensure publica la coverage si no existe (idempotente).
func (s *Service) Ensure(ctx context.Context, c Coverage) (*Coverage, error) {
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("service: coverage inválida: %w", err)
	}
	existing, err := s.repo.Get(ctx, c.Workspace, c.Store, c.Name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("service: error consultando coverage %q: %w", c.Name, err)
	}
	if existing != nil {
		return existing, nil
	}
	if err := s.repo.Create(ctx, c); err != nil {
		return nil, fmt.Errorf("service: error publicando coverage %q: %w", c.Name, err)
	}
	return s.repo.Get(ctx, c.Workspace, c.Store, c.Name)
}

// Get obtiene una coverage.
func (s *Service) Get(ctx context.Context, workspace, store, name string) (*Coverage, error) {
	return s.repo.Get(ctx, workspace, store, name)
}

// List devuelve los nombres de las coverages publicadas desde un almacén.
func (s *Service) List(ctx context.Context, workspace, store string) ([]string, error) {
	return s.repo.List(ctx, workspace, store)
}

// Update aplica los cambios de c sobre una coverage existente.
func (s *Service) Update(ctx context.Context, c Coverage) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("service: coverage inválida: %w", err)
	}
	if err := s.repo.Update(ctx, c); err != nil {
		return fmt.Errorf("service: error actualizando coverage %q: %w", c.Name, err)
	}
	return nil
}

// Remove despublica la coverage (y su capa).
func (s *Service) Remove(ctx context.Context, workspace, store, name string) error {
	if err := s.repo.Delete(ctx, workspace, store, name); err != nil {
		return fmt.Errorf("service: error eliminando coverage %q: %w", name, err)
	}
	return nil
}
