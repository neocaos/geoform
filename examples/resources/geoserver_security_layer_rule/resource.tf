# Everyone with one of these roles can read every layer of the workspace.
resource "geoserver_security_layer_rule" "demo_read" {
  workspace = geoserver_workspace.demo.name
  layer     = "*"
  access    = "read"
  roles     = ["ROLE_VIEWER", geoserver_security_role.editor.name]
}

# Only editors can write one layer.
resource "geoserver_security_layer_rule" "cities_write" {
  workspace = geoserver_workspace.demo.name
  layer     = "cities"
  access    = "write"
  roles     = [geoserver_security_role.editor.name]
}
