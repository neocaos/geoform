package geoserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Projection policies accepted by GeoServer for a published resource.
const (
	ProjectionForceDeclared       = "FORCE_DECLARED"
	ProjectionReprojectToDeclared = "REPROJECT_TO_DECLARED"
	ProjectionKeepNative          = "NONE"
)

// FeatureType is a vector layer published from a data store table.
type FeatureType struct {
	Name       string `json:"name"`
	NativeName string `json:"nativeName,omitempty"`
	Title      string `json:"title,omitempty"`
	// Abstract is always sent so it can be cleared.
	Abstract         string       `json:"abstract"`
	SRS              string       `json:"srs,omitempty"`
	ProjectionPolicy string       `json:"projectionPolicy,omitempty"`
	Enabled          bool         `json:"enabled"`
	NativeBBox       *BoundingBox `json:"nativeBoundingBox,omitempty"`
	LatLonBBox       *BoundingBox `json:"latLonBoundingBox,omitempty"`
}

// BoundingBox is an extent in the CRS it is attached to. CRS is kept raw
// because GeoServer sends either an "EPSG:x" string or a WKT object.
type BoundingBox struct {
	MinX float64         `json:"minx"`
	MaxX float64         `json:"maxx"`
	MinY float64         `json:"miny"`
	MaxY float64         `json:"maxy"`
	CRS  json.RawMessage `json:"crs,omitempty"`
}

type featureTypeEnvelope struct {
	FeatureType FeatureType `json:"featureType"`
}

func featureTypesPath(workspace, store string) string {
	return dataStorePath(workspace, store) + "/featuretypes"
}

func featureTypePath(workspace, store, name string) string {
	return featureTypesPath(workspace, store) + "/" + url.PathEscape(name)
}

// GetFeatureType fetches a feature type. It returns an error satisfying
// IsNotFound if it, its store or its workspace does not exist.
func (c *Client) GetFeatureType(ctx context.Context, workspace, store, name string) (*FeatureType, error) {
	var env featureTypeEnvelope
	if err := c.do(ctx, http.MethodGet, featureTypePath(workspace, store, name)+".json?quietOnNotFound=true", nil, &env, http.StatusOK); err != nil {
		return nil, err
	}
	return &env.FeatureType, nil
}

// CreateFeatureType publishes a table of store as a feature type, which also
// creates its layer. Bounding boxes left nil are computed by GeoServer.
func (c *Client) CreateFeatureType(ctx context.Context, workspace, store string, ft FeatureType) error {
	return c.do(ctx, http.MethodPost, featureTypesPath(workspace, store), featureTypeEnvelope{FeatureType: ft}, nil, http.StatusCreated)
}

// UpdateFeatureType updates the feature type called name. GeoServer only
// changes the fields present in ft. With recalculateBBoxes, the native and
// lat/lon bounding boxes are recomputed from the data.
func (c *Client) UpdateFeatureType(ctx context.Context, workspace, store, name string, ft FeatureType, recalculateBBoxes bool) error {
	path := featureTypePath(workspace, store, name)
	if recalculateBBoxes {
		path += "?recalculate=nativebbox,latlonbbox"
	}
	return c.do(ctx, http.MethodPut, path, featureTypeEnvelope{FeatureType: ft}, nil, http.StatusOK)
}

// DeleteFeatureType unpublishes a feature type. GeoServer refuses to delete
// one that still has its layer unless recurse is set, in which case the layer
// is deleted and removed from any layer groups too.
func (c *Client) DeleteFeatureType(ctx context.Context, workspace, store, name string, recurse bool) error {
	path := featureTypePath(workspace, store, name) + "?recurse=false"
	if recurse {
		path = featureTypePath(workspace, store, name) + "?recurse=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil, http.StatusOK)
}
