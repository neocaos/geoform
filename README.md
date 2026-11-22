# geoform

A Terraform provider for [GeoServer](https://geoserver.org): manage GeoServer
configuration as code, with plans, state, drift detection and imports handled by
Terraform.

> Status: early development. Available: `geoserver_workspace`, `geoserver_postgis_datastore`,
> `geoserver_featuretype`.

## `geoform` CLI

Standalone declarative CLI: describe workspaces, PostGIS datastores and layers in
YAML, preview the diff and reconcile GeoServer idempotently. It shares the REST
client and domain services with the Terraform provider.

```sh
make build-cli
export PG_PASSWORD=...  GEOSERVER_PASSWORD=...
./geoform validate --file examples/cli/geoform.yaml   # offline check
./geoform plan     --file examples/cli/geoform.yaml   # no writes
./geoform apply    --file examples/cli/geoform.yaml   # asks for confirmation
```

See [`examples/cli/geoform.yaml`](examples/cli/geoform.yaml) for the file format.
Unknown keys and invalid values are reported all at once with their path
(e.g. `workspaces[0].datastores[1].port`).

| Flag / env                              | Applies to     | Description                                                              |
|-----------------------------------------|----------------|--------------------------------------------------------------------------|
| `--file`                                | all            | YAML file (default `geoform.yaml`).                                      |
| `--server` / `GEOSERVER_URL`            | plan, apply    | GeoServer URL (default `http://localhost:8080/geoserver`).               |
| `--user` / `GEOSERVER_USERNAME`         | plan, apply    | REST user (default `admin`).                                             |
| `--password` / `GEOSERVER_PASSWORD`     | plan, apply    | REST password; prefer the env var (flags show up in `ps`).               |
| `--prune`                               | plan, apply    | Also delete datastores/layers missing from the file (off by default).    |
| `--auto-approve`                        | apply          | Skip the confirmation prompt.                                            |
| `--check-env`                           | validate       | Fail if a `password_env` variable is unset.                              |

Datastore passwords are never written in the file: use `password_env: VAR`.

Behaviour notes:

- `--prune` only touches workspaces declared in the file (datastores and layers
  not listed are deleted); workspaces themselves are never deleted.
- Changes to host, port, database, schema, user, table, SRS, title and
  `isolated` are detected and updated in place. A layer with no `title` leaves
  GeoServer's title unmanaged. Password-only changes are not detected
  (GeoServer never returns it).
- Exit codes: `0` ok / plan without changes, `1` runtime error or apply
  declined, `2` plan with pending changes, `3` invalid config or usage.

## Terraform provider usage

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

**`geoserver_featuretype`**

Publishes an existing table of a data store as a layer. GeoServer creates the
matching layer automatically and computes the bounding boxes from the data.

| Attribute           | Type   | Notes                                                                                   |
|---------------------|--------|-----------------------------------------------------------------------------------------|
| `workspace`         | string | Required. Changing it replaces the feature type.                                        |
| `datastore`         | string | Required. Changing it replaces the feature type.                                        |
| `name`              | string | Required. Published layer name. Changing it replaces the feature type.                  |
| `native_name`       | string | Optional, default `name`. Table or view to publish. Changing it replaces the feature type. |
| `title`             | string | Optional. GeoServer defaults it to the table name.                                      |
| `abstract`          | string | Optional.                                                                               |
| `srs`               | string | Optional, e.g. `EPSG:4326`. Defaults to the table's native CRS.                         |
| `projection_policy` | string | Optional: `FORCE_DECLARED`, `REPROJECT_TO_DECLARED` or `NONE`. Chosen by GeoServer if unset. |
| `enabled`           | bool   | Optional, default `true`.                                                               |
| `id`                | string | Computed, `<workspace>/<datastore>/<name>`.                                             |

Import with `terraform import geoserver_featuretype.roads acme/roads/roads`.

Bounding boxes are recalculated when `srs` or `projection_policy` changes, and
otherwise left alone. Unsetting `title`, `srs` or `projection_policy` keeps the
current value rather than resetting it. Destroying a feature type also deletes
its layer and removes it from any layer groups, since GeoServer cannot delete
one without the other.

More in [`examples/`](examples).

## Development

Requirements: Go (see `go.mod`), Terraform CLI, Docker.

```sh
make build     # builds ./terraform-provider-geoserver
make test      # unit tests, no Docker
make testacc   # starts GeoServer in Docker, runs acceptance tests, tears it down
make testacc-all # the same, against every supported GeoServer version
make up / down # just start / stop the local stack: GeoServer (admin / geoserver) and seeded PostGIS
```

Tested against GeoServer **2.28.5, 2.25.4 and 2.24.4** (the
[`kartoza/geoserver`](https://hub.docker.com/r/kartoza/geoserver) image; the
newest Kartoza tags of the 2.25 and 2.24 lines), the most-used versions in public Docker
setups. CI runs the acceptance tests on each; `make testacc` uses 2.28.5 unless
`GEOSERVER_VERSION` is set, e.g. `GEOSERVER_VERSION=2.24.4 make testacc`.

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
internal/geoserver/     GeoServer REST API client (the only one)
internal/infrastructure/geoserver/  adapters from that client to the domain repositories
internal/provider/      Terraform provider, resources and acceptance tests
examples/               example configurations
```


Ver [docs/release.md](docs/release.md) para el proceso de releases.

## Stack de desarrollo (Kartoza + PostGIS)

Un único `docker-compose.yml`: GeoServer ([`kartoza/geoserver`](https://hub.docker.com/r/kartoza/geoserver),
2.28.5 por defecto) + PostGIS (`postgis/postgis:16-3.4`) en una red compose compartida. PostGIS se siembra con
`test/testdata/postgis/init.sql` (esquema `geodata`: `cities` puntos, `roads` líneas, `regions` polígonos;
SRID 4326 con índice GIST). Lo usan `make up`, `make testacc` y el desarrollo manual.

```sh
make up      # = docker compose up -d --wait; el primer arranque de GeoServer tarda ~1 min
# GeoServer: http://localhost:8080/geoserver  (admin / geoserver)
# PostGIS:   localhost:5432  (geouser / geopassword, base geoform)
make down    # parar y borrar datos
```

`GEOSERVER_VERSION`, `GEOSERVER_PORT` y `POSTGIS_PORT` cambian versión y puertos. Dentro de la red
compose, GeoServer ve la base como host `db`, puerto `5432`.

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

## License

See [LICENSE](LICENSE).
