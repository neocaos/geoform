// Package datastore modela un origen de datos PostGIS dentro de un workspace.
package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// Type es el tipo de origen de datos vectorial de un datastore.
type Type string

const (
	// TypePostGIS es el tipo por defecto (el valor vacío equivale a este).
	TypePostGIS Type = "postgis"
	// TypeGeoPackage es un fichero GeoPackage (.gpkg) con capas vectoriales.
	TypeGeoPackage Type = "geopackage"
	// TypeShapefile es un shapefile (.shp) o un directorio de shapefiles.
	TypeShapefile Type = "shapefile"
)

// Datastore es un origen de datos vectorial publicado en un workspace. Según
// Type se usa PostGIS (conexión a base de datos) o File (URL de GeoServer del
// fichero, p. ej. "file:/data/places.gpkg").
type Datastore struct {
	Workspace string
	Name      string
	Type      Type
	PostGIS   PostGIS
	File      string
}

// Kind devuelve el tipo efectivo: el valor vacío es PostGIS.
func (d Datastore) Kind() Type {
	if d.Type == "" {
		return TypePostGIS
	}
	return d.Type
}

// IsFile indica si el datastore se define por un fichero en vez de por una conexión.
func (d Datastore) IsFile() bool {
	return d.Kind() == TypeGeoPackage || d.Kind() == TypeShapefile
}

// Validate aplica las reglas mínimas de la entidad.
func (d Datastore) Validate() error {
	switch {
	case d.Workspace == "":
		return errors.New("datastore: el workspace no puede estar vacío")
	case d.Name == "":
		return errors.New("datastore: el nombre no puede estar vacío")
	case d.IsFile():
		return d.validateFile()
	case d.Kind() != TypePostGIS:
		return fmt.Errorf("datastore: tipo %q no soportado", d.Type)
	case d.PostGIS.Host == "" || d.PostGIS.Database == "" || d.PostGIS.User == "":
		return errors.New("datastore: host, database y user son obligatorios")
	case d.PostGIS.Port < 0 || d.PostGIS.Port > 65535:
		return fmt.Errorf("datastore: puerto inválido %d", d.PostGIS.Port)
	}
	return nil
}

func (d Datastore) validateFile() error {
	if strings.TrimSpace(d.File) == "" {
		return fmt.Errorf("datastore: file es obligatorio para el tipo %s", d.Kind())
	}
	if d.PostGIS != (PostGIS{}) {
		return fmt.Errorf("datastore: los parámetros de PostGIS no aplican al tipo %s", d.Kind())
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
