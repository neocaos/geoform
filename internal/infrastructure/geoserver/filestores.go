package geoserver

import (
	"github.com/neocaos/geoform/internal/domain/datastore"
	gs "github.com/neocaos/geoform/internal/geoserver"
)

// kindOf deduce el tipo de datastore de Geoform a partir de lo que devuelve GeoServer.
func kindOf(ds *gs.DataStore) datastore.Type {
	p := ds.ConnectionParameters
	switch {
	case p["dbtype"] == gs.DBTypeGeoPackage:
		return datastore.TypeGeoPackage
	case p["dbtype"] == "" && p["url"] != "":
		return datastore.TypeShapefile
	default:
		return datastore.TypePostGIS
	}
}

// fileDatastore traduce un data store de GeoServer basado en fichero; ok es
// false si es de otro tipo (PostGIS).
func fileDatastore(ws string, ds *gs.DataStore) (*datastore.Datastore, bool) {
	switch kindOf(ds) {
	case datastore.TypeGeoPackage:
		return &datastore.Datastore{Workspace: ws, Name: ds.Name, Type: datastore.TypeGeoPackage, File: ds.ConnectionParameters["database"]}, true
	case datastore.TypeShapefile:
		return &datastore.Datastore{Workspace: ws, Name: ds.Name, Type: datastore.TypeShapefile, File: ds.ConnectionParameters["url"]}, true
	}
	return nil, false
}

// managedParams son los parámetros de conexión que Geoform gestiona para d.
func managedParams(d datastore.Datastore) map[string]string {
	switch d.Kind() {
	case datastore.TypeGeoPackage:
		return map[string]string{"dbtype": gs.DBTypeGeoPackage, "database": d.File}
	case datastore.TypeShapefile:
		return map[string]string{"url": d.File}
	}
	return postgisParams(d.PostGIS)
}

// newDataStore construye el data store de GeoServer completo para d.
func newDataStore(d datastore.Datastore) gs.DataStore {
	switch d.Kind() {
	case datastore.TypeGeoPackage:
		return gs.GeoPackageDataStore(d.Name, d.File)
	case datastore.TypeShapefile:
		return gs.ShapefileDataStore(d.Name, d.File)
	}
	return gs.DataStore{Name: d.Name, Type: "PostGIS", Enabled: true, ConnectionParameters: postgisParams(d.PostGIS)}
}
