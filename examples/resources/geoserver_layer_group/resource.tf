# Groups layers of the workspace; the first one is drawn at the bottom.
resource "geoserver_layer_group" "base" {
  workspace = geoserver_workspace.acme.name
  name      = "base"
  title     = "Base map"
  mode      = "SINGLE"
  layers    = [geoserver_featuretype.regions.name, geoserver_featuretype.roads.name]
}
