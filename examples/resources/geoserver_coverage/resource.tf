resource "geoserver_coverage" "relieve" {
  workspace     = geoserver_workspace.acme.name
  coveragestore = geoserver_geotiff_coveragestore.dem.name
  name          = "relieve"
  native_name   = "dem" # file name without extension
  title         = "Relieve"
  srs           = "EPSG:4326"
}
