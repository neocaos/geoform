package workspace

import (
	"context"
	"fmt"
)

// Service contiene la lógica de negocio sobre Workspace, desacoplada de
// GeoServer y de Terraform. Depende de la interfaz Repository, no de una
// implementación concreta (se inyecta desde infrastructure).
type Service struct {
	repo Repository
}

// NewService crea un Service inyectando el repositorio a usar.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Ensure crea el workspace si no existe. Si ya existe, no hace nada
// (operación idempotente, útil para el "apply" de Terraform).
func (s *Service) Ensure(ctx context.Context, w Workspace) (*Workspace, error) {
	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("service: workspace inválido: %w", err)
	}

	existing, err := s.repo.Get(ctx, w.Name)
	if err != nil && err != ErrNotFound {
		return nil, fmt.Errorf("service: error consultando workspace %q: %w", w.Name, err)
	}
	if existing != nil {
		return existing, nil
	}

	if err := s.repo.Create(ctx, w); err != nil {
		return nil, fmt.Errorf("service: error creando workspace %q: %w", w.Name, err)
	}

	return s.repo.Get(ctx, w.Name)
}

// Remove elimina el workspace. recurse controla si se elimina también su
// contenido (datastores, coveragestores, etc.).
func (s *Service) Remove(ctx context.Context, name string, recurse bool) error {
	if err := s.repo.Delete(ctx, name, recurse); err != nil {
		return fmt.Errorf("service: error eliminando workspace %q: %w", name, err)
	}
	return nil
}

// Get obtiene un workspace por nombre.
func (s *Service) Get(ctx context.Context, name string) (*Workspace, error) {
	return s.repo.Get(ctx, name)
}