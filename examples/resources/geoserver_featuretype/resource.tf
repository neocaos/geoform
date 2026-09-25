# Publishes the existing "roads_2026" table of the store as the layer acme:roads.
# Terraform does not create the table itself.
resource "geoserver_featuretype" "roads" {
  workspace   = geoserver_workspace.acme.name
  datastore   = geoserver_postgis_datastore.roads.name
  name        = "roads"
  native_name = "roads_2026"

  title    = "Road network"
  abstract = "Main roads, updated nightly."

  # Serve in Web Mercator regardless of the table's native CRS.
  srs               = "EPSG:3857"
  projection_policy = "REPROJECT_TO_DECLARED"
}
