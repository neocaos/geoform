// Package layergroup modela un grupo de capas con alcance de workspace.
package layergroup

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotFound se devuelve cuando el grupo no existe.
var ErrNotFound = errors.New("layergroup: no encontrado")

// Modos de grupo admitidos por GeoServer.
const (
	ModeSingle          = "SINGLE"
	ModeOpaqueContainer = "OPAQUE_CONTAINER"
	ModeNamed           = "NAMED"
	ModeContainer       = "CONTAINER"
	ModeEO              = "EO"
)

// Group agrupa capas de un workspace bajo un único nombre.
type Group struct {
	Workspace string
	Name      string
	Title     string   // vacío = no gestionado
	Mode      string   // vacío = SINGLE
	Layers    []string // nombres de capa del mismo workspace, en orden de dibujo
}

// EffectiveMode devuelve el modo, con SINGLE por defecto.
func (g Group) EffectiveMode() string {
	if g.Mode == "" {
		return ModeSingle
	}
	return g.Mode
}

// Validate aplica las reglas mínimas de la entidad.
func (g Group) Validate() error {
	switch {
	case g.Workspace == "":
		return errors.New("layergroup: el workspace es obligatorio")
	case g.Name == "":
		return errors.New("layergroup: el nombre no puede estar vacío")
	case len(g.Layers) == 0:
		return errors.New("layergroup: debe incluir al menos una capa")
	}
	switch g.EffectiveMode() {
	case ModeSingle, ModeOpaqueContainer, ModeNamed, ModeContainer, ModeEO:
	default:
		return fmt.Errorf("layergroup: modo %q inválido (SINGLE, OPAQUE_CONTAINER, NAMED, CONTAINER, EO)", g.Mode)
	}
	seen := map[string]bool{}
	for _, l := range g.Layers {
		if l == "" {
			return errors.New("layergroup: nombre de capa vacío")
		}
		if seen[l] {
			return fmt.Errorf("layergroup: capa %q repetida", l)
		}
		seen[l] = true
	}
	return nil
}

// Repository abstrae la persistencia de Group (implementada en infrastructure).
type Repository interface {
	Get(ctx context.Context, workspace, name string) (*Group, error)
	List(ctx context.Context, workspace string) ([]string, error)
	Create(ctx context.Context, g Group) error
	Update(ctx context.Context, g Group) error
	Delete(ctx context.Context, workspace, name string) error
}

// Service contiene la lógica de negocio sobre Group.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Get(ctx context.Context, workspace, name string) (*Group, error) {
	return s.repo.Get(ctx, workspace, name)
}

func (s *Service) List(ctx context.Context, workspace string) ([]string, error) {
	return s.repo.List(ctx, workspace)
}

// Ensure crea el grupo si no existe; si existe no hace nada (idempotente).
func (s *Service) Ensure(ctx context.Context, g Group) (*Group, error) {
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("service: grupo inválido: %w", err)
	}
	existing, err := s.repo.Get(ctx, g.Workspace, g.Name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("service: error consultando grupo %q: %w", g.Name, err)
	}
	if existing != nil {
		return existing, nil
	}
	if err := s.repo.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("service: error creando grupo %q: %w", g.Name, err)
	}
	return s.repo.Get(ctx, g.Workspace, g.Name)
}

func (s *Service) Update(ctx context.Context, g Group) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("service: grupo inválido: %w", err)
	}
	if err := s.repo.Update(ctx, g); err != nil {
		return fmt.Errorf("service: error actualizando grupo %q: %w", g.Name, err)
	}
	return nil
}

func (s *Service) Remove(ctx context.Context, workspace, name string) error {
	if err := s.repo.Delete(ctx, workspace, name); err != nil {
		return fmt.Errorf("service: error eliminando grupo %q: %w", name, err)
	}
	return nil
}
