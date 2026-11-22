package geoserver

import (
	"context"

	"github.com/neocaos/geoform/internal/domain/coverage"
	gs "github.com/neocaos/geoform/internal/geoserver"
)

// CoverageRepository adapta Client a coverage.Repository (raster).
type CoverageRepository struct{ c *gs.Client }

func NewCoverageRepository(c *gs.Client) *CoverageRepository { return &CoverageRepository{c} }

func (r *CoverageRepository) GetStore(ctx context.Context, ws, name string) (*coverage.Store, error) {
	cs, err := r.c.GetCoverageStore(ctx, ws, name)
	if gs.IsNotFound(err) {
		return nil, coverage.ErrStoreNotFound
	}
	if err != nil {
		return nil, err
	}
	return &coverage.Store{Workspace: ws, Name: cs.Name, Type: coverage.TypeGeoTIFF, URL: cs.URL}, nil
}

func (r *CoverageRepository) ListStores(ctx context.Context, ws string) ([]string, error) {
	names, err := r.c.ListCoverageStores(ctx, ws)
	if gs.IsNotFound(err) {
		return nil, nil
	}
	return names, err
}

func (r *CoverageRepository) CreateStore(ctx context.Context, s coverage.Store) error {
	return r.c.CreateCoverageStore(ctx, s.Workspace, gs.CoverageStore{
		Name: s.Name, Type: gs.CoverageStoreTypeGeoTIFF, Enabled: true, URL: s.URL,
	})
}

// UpdateStore conserva los campos que Geoform no gestiona y sobrescribe la URL.
func (r *CoverageRepository) UpdateStore(ctx context.Context, s coverage.Store) error {
	cur, err := r.c.GetCoverageStore(ctx, s.Workspace, s.Name)
	if gs.IsNotFound(err) {
		return coverage.ErrStoreNotFound
	}
	if err != nil {
		return err
	}
	cur.URL = s.URL
	return r.c.UpdateCoverageStore(ctx, s.Workspace, s.Name, *cur)
}

func (r *CoverageRepository) DeleteStore(ctx context.Context, ws, name string, recurse bool) error {
	err := r.c.DeleteCoverageStore(ctx, ws, name, recurse)
	if gs.IsNotFound(err) {
		return coverage.ErrStoreNotFound
	}
	return err
}

func (r *CoverageRepository) Get(ctx context.Context, ws, store, name string) (*coverage.Coverage, error) {
	cv, err := r.c.GetCoverage(ctx, ws, store, name)
	if gs.IsNotFound(err) {
		return nil, coverage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &coverage.Coverage{Workspace: ws, Store: store, Name: cv.Name, Native: cv.NativeName, Title: cv.Title, SRS: cv.SRS}, nil
}

func (r *CoverageRepository) List(ctx context.Context, ws, store string) ([]string, error) {
	names, err := r.c.ListCoverages(ctx, ws, store)
	if gs.IsNotFound(err) {
		return nil, nil
	}
	return names, err
}

func (r *CoverageRepository) Create(ctx context.Context, c coverage.Coverage) error {
	return r.c.CreateCoverage(ctx, c.Workspace, c.Store, gs.Coverage{
		Name: c.Name, NativeName: c.NativeName(), NativeCoverageName: c.NativeName(),
		Title: c.Title, SRS: c.SRS, Enabled: true,
	})
}

// Update modifica solo los campos gestionados, conservando el resto.
func (r *CoverageRepository) Update(ctx context.Context, c coverage.Coverage) error {
	cur, err := r.c.GetCoverage(ctx, c.Workspace, c.Store, c.Name)
	if gs.IsNotFound(err) {
		return coverage.ErrNotFound
	}
	if err != nil {
		return err
	}
	cur.SRS = c.SRS
	if c.Title != "" {
		cur.Title = c.Title
	}
	return r.c.UpdateCoverage(ctx, c.Workspace, c.Store, c.Name, *cur)
}

func (r *CoverageRepository) Delete(ctx context.Context, ws, store, name string) error {
	err := r.c.DeleteCoverage(ctx, ws, store, name, true)
	if gs.IsNotFound(err) {
		return coverage.ErrNotFound
	}
	return err
}
