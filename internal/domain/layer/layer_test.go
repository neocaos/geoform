package layer_test

import (
	"context"
	"testing"

	"github.com/neocaos/geoform/internal/domain/layer"
)

type fakeRepo struct {
	items   map[string]layer.Layer
	creates int
}

func (f *fakeRepo) Get(_ context.Context, ws, st, n string) (*layer.Layer, error) {
	l, ok := f.items[ws+"/"+st+"/"+n]
	if !ok {
		return nil, layer.ErrNotFound
	}
	return &l, nil
}
func (f *fakeRepo) Create(_ context.Context, l layer.Layer) error {
	f.creates++
	f.items[l.Workspace+"/"+l.Store+"/"+l.Name] = l
	return nil
}
func (f *fakeRepo) List(_ context.Context, ws, st string) ([]string, error) {
	var out []string
	for _, l := range f.items {
		if l.Workspace == ws && l.Store == st {
			out = append(out, l.Name)
		}
	}
	return out, nil
}
func (f *fakeRepo) Update(_ context.Context, l layer.Layer) error {
	f.items[l.Workspace+"/"+l.Store+"/"+l.Name] = l
	return nil
}
func (f *fakeRepo) Delete(_ context.Context, ws, st, n string) error {
	delete(f.items, ws+"/"+st+"/"+n)
	return nil
}

func TestEnsure_Idempotent(t *testing.T) {
	repo := &fakeRepo{items: map[string]layer.Layer{}}
	svc := layer.NewService(repo)
	l := layer.Layer{Workspace: "w", Store: "s", Name: "cities", SRS: "EPSG:4326"}
	for i := 0; i < 2; i++ {
		if _, err := svc.Ensure(context.Background(), l); err != nil {
			t.Fatal(err)
		}
	}
	if repo.creates != 1 {
		t.Fatalf("creates = %d, want 1", repo.creates)
	}
}

func TestNativeTable_DefaultsToName(t *testing.T) {
	if got := (layer.Layer{Name: "x"}).NativeTable(); got != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestEnsure_RequiresSRS(t *testing.T) {
	svc := layer.NewService(&fakeRepo{items: map[string]layer.Layer{}})
	if _, err := svc.Ensure(context.Background(), layer.Layer{Workspace: "w", Store: "s", Name: "n"}); err == nil {
		t.Fatal("se esperaba error")
	}
}
