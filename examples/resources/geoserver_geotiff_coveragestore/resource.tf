resource "geoserver_geotiff_coveragestore" "dem" {
  workspace = geoserver_workspace.acme.name
  name      = "dem"
  url       = "file:/data/stores/dem.tif"
}
