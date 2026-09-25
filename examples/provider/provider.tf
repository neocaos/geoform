terraform {
  required_providers {
    geoserver = {
      source = "neocaos/geoserver"
    }
  }
}

# Credentials can also come from GEOSERVER_URL, GEOSERVER_USERNAME and
# GEOSERVER_PASSWORD, which keeps secrets out of the configuration.
provider "geoserver" {
  url      = "http://localhost:8080/geoserver"
  username = "admin"
  password = var.geoserver_password
}

variable "geoserver_password" {
  type      = string
  sensitive = true
}
