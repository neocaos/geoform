package workspace

import (
	"context"
	"errors"
)

// ErrNotFound se devuelve cuando el workspace no existe.
var ErrNotFound = errors.New("workspace: no encontrado")

// Repository abstrae la persistencia del Workspace. La implementación real
// vive en internal/infrastructure/geoserver, adaptando esta interfaz al
// cliente REST de GeoServer.
type Repository interface {
	Get(ctx context.Context, name string) (*Workspace, error)
	Create(ctx context.Context, w Workspace) error
	Delete(ctx context.Context, name string, recurse bool) error
}