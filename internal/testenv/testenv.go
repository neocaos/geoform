// Package testenv centraliza la conexión a GeoServer de los integration tests,
// de modo que todos lean las mismas variables de entorno que scripts/integration.sh.
package testenv

import (
	"os"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// URL es la URL base de GeoServer (GEOSERVER_URL). Si no se define se usa el
// puerto GEOFORM_TEST_GEOSERVER_PORT (18080 por defecto) en loopback.
func URL() string {
	if v := os.Getenv("GEOSERVER_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:" + env("GEOFORM_TEST_GEOSERVER_PORT", "18080") + "/geoserver"
}

// User es el usuario administrador (GEOSERVER_USER).
func User() string { return env("GEOSERVER_USER", "admin") }

// Pass es la contraseña del administrador (GEOSERVER_PASS).
func Pass() string { return env("GEOSERVER_PASS", "geoserver") }
