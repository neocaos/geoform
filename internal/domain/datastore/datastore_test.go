package datastore_test

import (
	"context"
	"testing"

	"github.com/neocaos/geoform/internal/domain/datastore"
)

type fakeRepo struct {
	items   map[string]datastore.Datastore
	creates int
}

func (f *fakeRepo) Get(_ context.Context, ws, name string) (*datastore.Datastore, error) {
	d, ok := f.items[ws+"/"+name]
	if !ok {
		return nil, datastore.ErrNotFound
	}
	return &d, nil
}
func (f *fakeRepo) Create(_ context.Context, d datastore.Datastore) error {
	f.creates++
	f.items[d.Workspace+"/"+d.Name] = d
	return nil
}
func (f *fakeRepo) List(_ context.Context, ws string) ([]string, error) {
	var out []string
	for _, d := range f.items {
		if d.Workspace == ws {
			out = append(out, d.Name)
		}
	}
	return out, nil
}
func (f *fakeRepo) Update(_ context.Context, d datastore.Datastore) error {
	f.items[d.Workspace+"/"+d.Name] = d
	return nil
}
func (f *fakeRepo) Delete(_ context.Context, ws, name string, _ bool) error {
	delete(f.items, ws+"/"+name)
	return nil
}

func valid() datastore.Datastore {
	return datastore.Datastore{Workspace: "w", Name: "pg", PostGIS: datastore.PostGIS{Host: "db", Database: "g", User: "u"}}
}

func TestEnsure_Idempotent(t *testing.T) {
	repo := &fakeRepo{items: map[string]datastore.Datastore{}}
	svc := datastore.NewService(repo)
	for i := 0; i < 2; i++ {
		if _, err := svc.Ensure(context.Background(), valid()); err != nil {
			t.Fatal(err)
		}
	}
	if repo.creates != 1 {
		t.Fatalf("creates = %d, want 1", repo.creates)
	}
}

func TestEnsure_Invalid(t *testing.T) {
	svc := datastore.NewService(&fakeRepo{items: map[string]datastore.Datastore{}})
	d := valid()
	d.PostGIS.Host = ""
	if _, err := svc.Ensure(context.Background(), d); err == nil {
		t.Fatal("se esperaba error de validación")
	}
}
