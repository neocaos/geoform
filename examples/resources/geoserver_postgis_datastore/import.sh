# Data stores are imported as <workspace>/<name>. The password cannot be read
# back from GeoServer, so the next apply will set it from configuration.
terraform import geoserver_postgis_datastore.roads acme/roads
