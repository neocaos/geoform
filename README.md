# geoform
MapOps solution. Terraforming Geoserver

## Stack de desarrollo

GeoServer ([`kartoza/geoserver:2.28.5`](https://hub.docker.com/r/kartoza/geoserver)) + PostGIS (`postgis/postgis:16-3.4`)
en una red compose compartida. PostGIS se siembra con `test/testdata/postgis/init.sql`
(esquema `geodata`: `cities` puntos, `roads` líneas, `regions` polígonos; SRID 4326 con índice GIST).

```sh
docker compose -f docker-compose.dev.yml up -d --wait   # el primer arranque de GeoServer tarda ~1 min
# GeoServer: http://localhost:8080/geoserver  (admin / geoserver)
# PostGIS:   localhost:5432  (geouser / geopassword, base geoform)
docker compose -f docker-compose.dev.yml down -v        # parar y borrar datos
```

Dentro de la red compose, GeoServer ve la base como host `db`, puerto `5432`.

## Tests

```sh
make test               # unitarios, sin Docker
make test-integration   # levanta docker-compose.test.yml, ejecuta go test -tags=integration y hace down -v
```

El stack de tests usa puertos `18080` (GeoServer) y `15432` (PostGIS) para no chocar con el de dev.
Los tests de integración crean un workspace, un datastore PostGIS y publican las tres tablas como
capas (todo vía `Ensure`, ejecutado dos veces para comprobar idempotencia), verifican por REST y limpian.
Se pueden apuntar a otro GeoServer con `GEOSERVER_URL` (por defecto `http://localhost:18080/geoserver/rest`),
`GEOSERVER_USER` y `GEOSERVER_PASS`. `make stack-up` / `make stack-down` levantan/borran el stack de tests a mano.
