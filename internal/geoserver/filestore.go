package geoserver

// Tipos de data store basados en fichero. GeoServer los identifica por el
// campo type y por sus parámetros de conexión.
const (
	DataStoreTypeGeoPackage = "GeoPackage"
	DataStoreTypeShapefile  = "Shapefile"
	// DBTypeGeoPackage es el valor de "dbtype" de un GeoPackage.
	DBTypeGeoPackage = "geopkg"
)

// GeoPackageDataStore devuelve el data store de un GeoPackage vectorial.
// file es una URL de GeoServer (p. ej. "file:/data/places.gpkg" o "file:data/places.gpkg",
// relativa al data dir).
func GeoPackageDataStore(name, file string) DataStore {
	return DataStore{
		Name: name, Type: DataStoreTypeGeoPackage, Enabled: true,
		ConnectionParameters: ConnectionParameters{"dbtype": DBTypeGeoPackage, "database": file},
	}
}

// ShapefileDataStore devuelve el data store de un shapefile (o directorio de shapefiles).
func ShapefileDataStore(name, file string) DataStore {
	return DataStore{
		Name: name, Type: DataStoreTypeShapefile, Enabled: true,
		ConnectionParameters: ConnectionParameters{"url": file},
	}
}
