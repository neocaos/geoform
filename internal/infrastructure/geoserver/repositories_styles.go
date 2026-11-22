package geoserver

import (
	"context"
	"strings"

	"github.com/neocaos/geoform/internal/domain/layergroup"
	"github.com/neocaos/geoform/internal/domain/style"
	gs "github.com/neocaos/geoform/internal/geoserver"
)

// StyleRepository adapta Client a style.Repository.
type StyleRepository struct{ c *gs.Client }

func NewStyleRepository(c *gs.Client) *StyleRepository { return &StyleRepository{c} }

func (r *StyleRepository) Get(ctx context.Context, ws, name string) (*style.Style, error) {
	sld, err := r.c.GetStyleSLD(ctx, ws, name)
	if gs.IsNotFound(err) {
		return nil, style.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &style.Style{Workspace: ws, Name: name, SLD: sld}, nil
}

func (r *StyleRepository) List(ctx context.Context, ws string) ([]string, error) {
	names, err := r.c.ListStyles(ctx, ws)
	if gs.IsNotFound(err) {
		return nil, nil
	}
	return names, err
}

func (r *StyleRepository) Create(ctx context.Context, s style.Style) error {
	return r.c.CreateStyle(ctx, s.Workspace, s.Name, s.SLD)
}

func (r *StyleRepository) Update(ctx context.Context, s style.Style) error {
	err := r.c.UpdateStyle(ctx, s.Workspace, s.Name, s.SLD)
	if gs.IsNotFound(err) {
		return style.ErrNotFound
	}
	return err
}

// Delete borra el estilo; recurse retira antes su referencia de las capas.
func (r *StyleRepository) Delete(ctx context.Context, ws, name string) error {
	err := r.c.DeleteStyle(ctx, ws, name, true)
	if gs.IsNotFound(err) {
		return style.ErrNotFound
	}
	return err
}

// LayerGroupRepository adapta Client a layergroup.Repository. Los nombres de
// capa del dominio son locales al workspace; GeoServer usa "workspace:capa".
type LayerGroupRepository struct{ c *gs.Client }

func NewLayerGroupRepository(c *gs.Client) *LayerGroupRepository { return &LayerGroupRepository{c} }

func (r *LayerGroupRepository) Get(ctx context.Context, ws, name string) (*layergroup.Group, error) {
	g, err := r.c.GetLayerGroup(ctx, ws, name)
	if gs.IsNotFound(err) {
		return nil, layergroup.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	layers := make([]string, 0, len(g.Layers))
	for _, l := range g.Layers {
		layers = append(layers, strings.TrimPrefix(l, ws+":"))
	}
	return &layergroup.Group{Workspace: ws, Name: g.Name, Title: g.Title, Mode: g.Mode, Layers: layers}, nil
}

func (r *LayerGroupRepository) List(ctx context.Context, ws string) ([]string, error) {
	names, err := r.c.ListLayerGroups(ctx, ws)
	if gs.IsNotFound(err) {
		return nil, nil
	}
	return names, err
}

func toWire(g layergroup.Group) gs.LayerGroup {
	layers := make([]string, 0, len(g.Layers))
	for _, l := range g.Layers {
		layers = append(layers, g.Workspace+":"+l)
	}
	return gs.LayerGroup{Name: g.Name, Title: g.Title, Mode: g.EffectiveMode(), Layers: layers}
}

func (r *LayerGroupRepository) Create(ctx context.Context, g layergroup.Group) error {
	return r.c.CreateLayerGroup(ctx, g.Workspace, toWire(g))
}

func (r *LayerGroupRepository) Update(ctx context.Context, g layergroup.Group) error {
	err := r.c.UpdateLayerGroup(ctx, g.Workspace, g.Name, toWire(g))
	if gs.IsNotFound(err) {
		return layergroup.ErrNotFound
	}
	return err
}

func (r *LayerGroupRepository) Delete(ctx context.Context, ws, name string) error {
	err := r.c.DeleteLayerGroup(ctx, ws, name)
	if gs.IsNotFound(err) {
		return layergroup.ErrNotFound
	}
	return err
}
