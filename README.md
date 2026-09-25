# geoform

A Terraform provider for [GeoServer](https://geoserver.org): manage GeoServer
configuration as code, with plans, state, drift detection and imports handled by
Terraform.

> Status: early development. Available: `geoserver_workspace`, `geoserver_postgis_datastore`.

## Usage

```hcl
terraform {
  required_providers {
    geoserver = {
      source = "neocaos/geoserver"
    }
  }
}

provider "geoserver" {
  url      = "http://localhost:8080/geoserver"
  username = "admin"
  # password = ...  or GEOSERVER_PASSWORD
}

resource "geoserver_workspace" "acme" {
  name     = "acme"
  isolated = false
}
```

### Provider configuration

| Attribute  | Environment variable | Description                                                          |
|------------|----------------------|----------------------------------------------------------------------|
| `url`      | `GEOSERVER_URL`      | Base URL, e.g. `http://localhost:8080/geoserver` (`/rest` optional). |
| `username` | `GEOSERVER_USERNAME` | REST API user.                                                       |
| `password` | `GEOSERVER_PASSWORD` | REST API password (sensitive).                                       |

### Resources

**`geoserver_workspace`**

| Attribute  | Type   | Notes                                                                     |
|------------|--------|---------------------------------------------------------------------------|
| `name`     | string | Required. Also the namespace prefix. Changing it replaces the workspace.  |
| `isolated` | bool   | Optional, default `false`. Updated in place.                              |
| `id`       | string | Computed, equal to `name`.                                                |

Import an existing workspace by name: `terraform import geoserver_workspace.acme acme`.

Destroying a workspace never deletes its contents: if it still holds stores or
layers, GeoServer refuses and Terraform reports the error.

**`geoserver_postgis_datastore`**

| Attribute     | Type   | Notes                                                                 |
|---------------|--------|-----------------------------------------------------------------------|
| `workspace`   | string | Required. Changing it replaces the store.                             |
| `name`        | string | Required. Changing it replaces the store.                             |
| `description` | string | Optional.                                                             |
| `enabled`     | bool   | Optional, default `true`.                                             |
| `host`        | string | Required. As reachable from GeoServer, not from where Terraform runs. |
| `port`        | number | Optional, default `5432`.                                             |
| `database`    | string | Required.                                                             |
| `schema`      | string | Optional, default `public`.                                           |
| `user`        | string | Required.                                                             |
| `password`    | string | Required, sensitive. See note below.                                  |
| `id`          | string | Computed, `<workspace>/<name>`.                                       |

Import with `terraform import geoserver_postgis_datastore.roads acme/roads`.

GeoServer only returns the password encrypted, so password changes made outside
Terraform are not detected, and after an import the next apply writes the
configured password. Connection parameters this resource does not manage (pool
sizes, "Expose primary keys", ...) are preserved on update. Destroy never
deletes the store's layers; GeoServer refuses if any remain.

More in [`examples/`](examples).

## Development

Requirements: Go (see `go.mod`), Terraform CLI, Docker.

```sh
make build     # builds ./terraform-provider-geoserver
make test      # unit tests, no Docker
make testacc   # starts GeoServer in Docker, runs acceptance tests, tears it down
make up / down # just start / stop local GeoServer (admin / geoserver) and PostGIS
```

Set `GEOSERVER_PORT` / `POSTGIS_PORT` if 8080 / 5432 are taken, e.g.
`GEOSERVER_PORT=18080 POSTGIS_PORT=15432 make testacc`.

To use a local build from Terraform, run `make install` and point Terraform at
it in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "neocaos/geoserver" = "/home/<you>/go/bin"
  }
  direct {}
}
```

### Layout

```
main.go                 provider entry point
internal/geoserver/     GeoServer REST API client
internal/provider/      Terraform provider, resources and acceptance tests
examples/               example configurations
```

## License

See [LICENSE](LICENSE).

## Stack de desarrollo (Kartoza + PostGIS)

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

## Tests de integración (cliente REST de #2)

```sh
make test               # unitarios, sin Docker
make test-integration   # levanta docker-compose.test.yml, ejecuta go test -tags=integration y hace down -v
```

El stack de tests usa puertos `18080` (GeoServer) y `15432` (PostGIS) para no chocar con el de dev.
Los tests de integración crean un workspace, un datastore PostGIS y publican las tres tablas como
capas (todo vía `Ensure`, ejecutado dos veces para comprobar idempotencia), verifican por REST y limpian.
Se pueden apuntar a otro GeoServer con `GEOSERVER_URL` (por defecto `http://localhost:18080/geoserver/rest`),
`GEOSERVER_USER` y `GEOSERVER_PASS`. `make stack-up` / `make stack-down` levantan/borran el stack de tests a mano.
