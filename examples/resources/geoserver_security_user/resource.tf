variable "alice_password" {
  type      = string
  sensitive = true # e.g. TF_VAR_alice_password; never hardcode it
}

resource "geoserver_security_user" "alice" {
  name     = "alice"
  password = var.alice_password
  enabled  = true
  roles    = [geoserver_security_role.editor.name]
}
