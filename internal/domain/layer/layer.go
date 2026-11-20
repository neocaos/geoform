// Package layer modela una capa vectorial publicada desde una tabla de un datastore.
package layer

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotFound se devuelve cuando la capa no existe.
var ErrNotFound = errors.New("layer: no encontrada")

// Layer es una tabla de un datastore publicada como capa.
type Layer struct {
	Workspace string
	Store     string
	Name      string // nombre publicado
	Table     string // tabla nativa; si está vacío se usa Name
	Title     string
	SRS       string // ej. "EPSG:4326"
}

// NativeTable devuelve la tabla de origen.
func (l Layer) NativeTable() string {
	if l.Table != "" {
		return l.Table
	}
	return l.Name
}

// Validate aplica las reglas mínimas de la entidad.
func (l Layer) Validate() error {
	switch {
	case l.Workspace == "" || l.Store == "":
		return errors.New("layer: workspace y datastore son obligatorios")
	case l.Name == "":
		return errors.New("layer: el nombre no puede estar vacío")
	case l.SRS == "":
		return errors.New("layer: el SRS es obligatorio")
	}
	return nil
}

// Repository abstrae la persistencia de Layer (implementada en infrastructure).
type Repository interface {
	Get(ctx context.Context, workspace, store, name string) (*Layer, error)
	Create(ctx context.Context, l Layer) error
	Delete(ctx context.Context, workspace, store, name string) error
}

// Service contiene la lógica de negocio sobre Layer.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Ensure publica la capa si no existe; si existe no hace nada (idempotente).
func (s *Service) Ensure(ctx context.Context, l Layer) (*Layer, error) {
	if err := l.Validate(); err != nil {
		return nil, fmt.Errorf("service: capa inválida: %w", err)
	}
	existing, err := s.repo.Get(ctx, l.Workspace, l.Store, l.Name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("service: error consultando capa %q: %w", l.Name, err)
	}
	if existing != nil {
		return existing, nil
	}
	if err := s.repo.Create(ctx, l); err != nil {
		return nil, fmt.Errorf("service: error publicando capa %q: %w", l.Name, err)
	}
	return s.repo.Get(ctx, l.Workspace, l.Store, l.Name)
}

// Get obtiene una capa.
func (s *Service) Get(ctx context.Context, workspace, store, name string) (*Layer, error) {
	return s.repo.Get(ctx, workspace, store, name)
}

// Remove despublica la capa.
func (s *Service) Remove(ctx context.Context, workspace, store, name string) error {
	if err := s.repo.Delete(ctx, workspace, store, name); err != nil {
		return fmt.Errorf("service: error eliminando capa %q: %w", name, err)
	}
	return nil
}
