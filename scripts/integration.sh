#!/usr/bin/env bash
# Ejecuta los integration tests contra un stack efímero y aislado.
#
# Cada ejecución usa un proyecto de compose único y puertos libres elegidos por
# Docker, así que varias ejecuciones en paralelo (workers, CI, devs) no se
# pisan. El stack se elimina siempre (también ante fallos o Ctrl-C) y se
# conserva el código de salida de go test.
#
# Variables (todas opcionales):
#   GEOFORM_IT_PROJECT            nombre del proyecto compose (por defecto único)
#   GEOFORM_IT_GEOSERVER_PORT     puerto fijo de GeoServer (por defecto: libre)
#   GEOFORM_IT_POSTGIS_PORT       puerto fijo de PostGIS   (por defecto: libre)
#   GEOFORM_IT_PKGS               paquetes a probar (por defecto ./internal/...)
#   GEOFORM_IT_RUN                filtro -run de go test
set -u
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME="${GEOFORM_IT_PROJECT:-geoform-it-$$-$(date +%s)}"
# Definida pero vacía => Docker asigna un puerto libre.
export GEOFORM_TEST_GEOSERVER_PORT="${GEOFORM_IT_GEOSERVER_PORT-}"
export GEOFORM_TEST_POSTGIS_PORT="${GEOFORM_IT_POSTGIS_PORT-}"
compose=(docker compose -f docker-compose.test.yml)

down() { "${compose[@]}" down -v --remove-orphans >/dev/null 2>&1; }
trap 'down' EXIT
trap 'exit 130' INT TERM

echo "== proyecto compose: $COMPOSE_PROJECT_NAME"
if ! "${compose[@]}" up -d --wait --wait-timeout 300; then
  "${compose[@]}" logs --tail=50
  exit 1
fi

hostport=$("${compose[@]}" port geoserver 8080 | sed 's/.*://')
export GEOSERVER_URL="http://127.0.0.1:${hostport}/geoserver"
export GEOSERVER_USER="${GEOSERVER_USER:-admin}"
export GEOSERVER_PASS="${GEOSERVER_PASS:-geoserver}"
echo "== GeoServer en $GEOSERVER_URL"

args=(-count=1 -tags=integration -timeout 20m)
[ -n "${GEOFORM_IT_RUN:-}" ] && args+=(-run "$GEOFORM_IT_RUN")
# shellcheck disable=SC2086
go test "${args[@]}" ${GEOFORM_IT_PKGS:-./internal/...} -v
