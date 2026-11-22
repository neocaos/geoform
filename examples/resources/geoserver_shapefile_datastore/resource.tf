resource "geoserver_shapefile_datastore" "places" {
  workspace = geoserver_workspace.acme.name
  name      = "places"
  file      = "file:/data/stores/places.shp"
}

# The published table is named after the file, without extension.
resource "geoserver_featuretype" "places" {
  workspace = geoserver_workspace.acme.name
  datastore = geoserver_shapefile_datastore.places.name
  name      = "places"
  srs       = "EPSG:4326"
}
