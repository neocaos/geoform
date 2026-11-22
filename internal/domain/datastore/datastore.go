// Package datastore modela un origen de datos PostGIS dentro de un workspace.
package datastore

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotFound se devuelve cuando el datastore no existe.
var ErrNotFound = errors.New("datastore: no encontrado")

// PostGIS son los parámetros de conexión a la base de datos.
type PostGIS struct {
	Host     string
	Port     int
	Database string
	Schema   string
	User     string
	Password string
}

// Datastore es un origen de datos PostGIS publicado en un workspace.
type Datastore struct {
	Workspace string
	Name      string
	PostGIS   PostGIS
}

// Validate aplica las reglas mínimas de la entidad.
func (d Datastore) Validate() error {
	switch {
	case d.Workspace == "":
		return errors.New("datastore: el workspace no puede estar vacío")
	case d.Name == "":
		return errors.New("datastore: el nombre no puede estar vacío")
	case d.PostGIS.Host == "" || d.PostGIS.Database == "" || d.PostGIS.User == "":
		return errors.New("datastore: host, database y user son obligatorios")
	case d.PostGIS.Port < 0 || d.PostGIS.Port > 65535:
		return fmt.Errorf("datastore: puerto inválido %d", d.PostGIS.Port)
	}
	return nil
}

// Repository abstrae la persistencia de Datastore (implementada en infrastructure).
type Repository interface {
	Get(ctx context.Context, workspace, name string) (*Datastore, error)
	List(ctx context.Context, workspace string) ([]string, error)
	Create(ctx context.Context, d Datastore) error
	Update(ctx context.Context, d Datastore) error
	Delete(ctx context.Context, workspace, name string, recurse bool) error
}

// Service contiene la lógica de negocio sobre Datastore.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Ensure crea el datastore si no existe; si existe no hace nada (idempotente).
func (s *Service) Ensure(ctx context.Context, d Datastore) (*Datastore, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("service: datastore inválido: %w", err)
	}
	existing, err := s.repo.Get(ctx, d.Workspace, d.Name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("service: error consultando datastore %q: %w", d.Name, err)
	}
	if existing != nil {
		return existing, nil
	}
	if err := s.repo.Create(ctx, d); err != nil {
		return nil, fmt.Errorf("service: error creando datastore %q: %w", d.Name, err)
	}
	return s.repo.Get(ctx, d.Workspace, d.Name)
}

// Get obtiene un datastore por workspace y nombre.
func (s *Service) Get(ctx context.Context, workspace, name string) (*Datastore, error) {
	return s.repo.Get(ctx, workspace, name)
}

// Remove elimina el datastore (recurse incluye sus capas).
func (s *Service) Remove(ctx context.Context, workspace, name string, recurse bool) error {
	if err := s.repo.Delete(ctx, workspace, name, recurse); err != nil {
		return fmt.Errorf("service: error eliminando datastore %q: %w", name, err)
	}
	return nil
}

// List devuelve los nombres de los datastores de un workspace.
func (s *Service) List(ctx context.Context, workspace string) ([]string, error) {
	return s.repo.List(ctx, workspace)
}

// Update aplica los cambios de d sobre un datastore existente.
func (s *Service) Update(ctx context.Context, d Datastore) error {
	if err := d.Validate(); err != nil {
		return fmt.Errorf("service: datastore inválido: %w", err)
	}
	if err := s.repo.Update(ctx, d); err != nil {
		return fmt.Errorf("service: error actualizando datastore %q: %w", d.Name, err)
	}
	return nil
}
