# Uploads an SLD as the workspace style acme:roads.
resource "geoserver_style" "roads" {
  workspace = geoserver_workspace.acme.name
  name      = "roads"
  sld       = file("${path.module}/roads.sld")
}

# Make it the default style of a layer (see geoserver_featuretype):
#   default_style = "${geoserver_style.roads.workspace}:${geoserver_style.roads.name}"
