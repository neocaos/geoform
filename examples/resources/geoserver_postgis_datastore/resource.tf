resource "geoserver_workspace" "acme" {
  name = "acme"
}

resource "geoserver_postgis_datastore" "roads" {
  workspace   = geoserver_workspace.acme.name
  name        = "roads"
  description = "Road network"

  # Connection details as seen from the GeoServer host.
  host     = "db.internal"
  port     = 5432
  database = "gis"
  schema   = "public"
  user     = "geoserver"
  password = var.roads_db_password
}

variable "roads_db_password" {
  type      = string
  sensitive = true
}
