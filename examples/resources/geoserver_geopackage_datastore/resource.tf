# The path is a URL as GeoServer sees it, not a path on the Terraform host.
resource "geoserver_geopackage_datastore" "places" {
  workspace = geoserver_workspace.acme.name
  name      = "places"
  file      = "file:/data/stores/places.gpkg"
}

# Publish a table of the GeoPackage as a layer.
resource "geoserver_featuretype" "places" {
  workspace   = geoserver_workspace.acme.name
  datastore   = geoserver_geopackage_datastore.places.name
  name        = "places"
  native_name = "places"
  srs         = "EPSG:4326"
}
