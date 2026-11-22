package coverage_test

import (
	"context"
	"testing"

	"github.com/neocaos/geoform/internal/domain/coverage"
)

type fakeRepo struct {
	stores  map[string]coverage.Store
	covs    map[string]coverage.Coverage
	creates int
}

func (f *fakeRepo) GetStore(_ context.Context, ws, n string) (*coverage.Store, error) {
	s, ok := f.stores[ws+"/"+n]
	if !ok {
		return nil, coverage.ErrStoreNotFound
	}
	return &s, nil
}
func (f *fakeRepo) ListStores(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeRepo) CreateStore(_ context.Context, s coverage.Store) error {
	f.creates++
	f.stores[s.Workspace+"/"+s.Name] = s
	return nil
}
func (f *fakeRepo) UpdateStore(context.Context, coverage.Store) error       { return nil }
func (f *fakeRepo) DeleteStore(context.Context, string, string, bool) error { return nil }
func (f *fakeRepo) Get(_ context.Context, ws, st, n string) (*coverage.Coverage, error) {
	c, ok := f.covs[ws+"/"+st+"/"+n]
	if !ok {
		return nil, coverage.ErrNotFound
	}
	return &c, nil
}
func (f *fakeRepo) List(context.Context, string, string) ([]string, error) { return nil, nil }
func (f *fakeRepo) Create(_ context.Context, c coverage.Coverage) error {
	f.creates++
	f.covs[c.Workspace+"/"+c.Store+"/"+c.Name] = c
	return nil
}
func (f *fakeRepo) Update(context.Context, coverage.Coverage) error      { return nil }
func (f *fakeRepo) Delete(context.Context, string, string, string) error { return nil }

func TestStore_Validate(t *testing.T) {
	if err := (coverage.Store{Workspace: "w", Name: "n", URL: "file:/a.tif"}).Validate(); err != nil {
		t.Fatalf("válido: %v", err)
	}
	for name, s := range map[string]coverage.Store{
		"sin url":  {Workspace: "w", Name: "n"},
		"sin ws":   {Name: "n", URL: "x"},
		"sin name": {Workspace: "w", URL: "x"},
		"tipo":     {Workspace: "w", Name: "n", URL: "x", Type: "jp2"},
	} {
		if s.Validate() == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
}

func TestCoverage_ValidateAndNative(t *testing.T) {
	c := coverage.Coverage{Workspace: "w", Store: "s", Name: "dem", SRS: "EPSG:4326"}
	if err := c.Validate(); err != nil || c.NativeName() != "dem" {
		t.Fatalf("%v %q", err, c.NativeName())
	}
	c.Native = "band1"
	if c.NativeName() != "band1" {
		t.Errorf("native: %q", c.NativeName())
	}
	if (coverage.Coverage{Workspace: "w", Store: "s", Name: "dem"}).Validate() == nil {
		t.Error("sin SRS debía fallar")
	}
}

func TestService_EnsureIdempotent(t *testing.T) {
	repo := &fakeRepo{stores: map[string]coverage.Store{}, covs: map[string]coverage.Coverage{}}
	svc := coverage.NewService(repo)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := svc.EnsureStore(ctx, coverage.Store{Workspace: "w", Name: "s", URL: "file:/a.tif"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Ensure(ctx, coverage.Coverage{Workspace: "w", Store: "s", Name: "dem", SRS: "EPSG:4326"}); err != nil {
			t.Fatal(err)
		}
	}
	if repo.creates != 2 {
		t.Errorf("creates = %d, esperado 2", repo.creates)
	}
	if _, err := svc.EnsureStore(ctx, coverage.Store{Workspace: "w", Name: "x"}); err == nil {
		t.Error("store inválido debía fallar")
	}
}
