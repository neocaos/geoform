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
| `--prune`                               | plan, apply    | Also delete datastores/layers/styles/groups missing from the file (off by default).    |
| `--auto-approve`                        | apply          | Skip the confirmation prompt.                                            |
| `--check-env`                           | validate       | Fail if a `password_env` variable is unset.                              |

Datastore passwords are never written in the file: use `password_env: VAR`.

### `geoform import`

Reads an existing GeoServer (workspaces, PostGIS datastores and their layers)
and writes the YAML that `plan` would see as "no changes":

```sh
./geoform import --output geoform.yaml [--workspace demo,other] [--force]
export GEOFORM_DEMO_POSTGIS_PASSWORD=...   # one placeholder per datastore
./geoform plan --file geoform.yaml          # -> no changes
```

Passwords are never read or written: each datastore gets
`password_env: GEOFORM_<WORKSPACE>_<DATASTORE>_PASSWORD`. Non-PostGIS stores and
layers without an `EPSG:<code>` SRS are skipped with a warning on stderr.
Without `--output` the YAML goes to stdout; an existing file needs `--force`.

Workspace styles (as inline `sld:`), layer groups and each layer's default
style (`style:`) are imported too, so `plan --prune` never proposes deleting
them. Styles that cannot be read as SLD, groups that include layers outside the
imported workspace, and a global default style hidden by a workspace style of
the same name are skipped with a warning (that style or group is then not
managed, so `--prune` would delete a skipped style or group).

Behaviour notes:

- `--prune` only touches workspaces declared in the file (datastores and layers
  not listed are deleted); workspaces themselves are never deleted.
- Changes to host, port, database, schema, user, table, SRS, title and
  `isolated` are detected and updated in place. A layer with no `title` leaves
  GeoServer's title unmanaged. Password-only changes are not detected
  (GeoServer never returns it).
- An optional `security:` section manages users, roles and data access rules
  (see [Security](#security-users-roles-and-access-rules)). Without it,
  security is never read or modified.
- **Styles and layer groups.** A workspace may declare `styles` (SLD files
  uploaded as workspace styles, from `file:` relative to the YAML or inline
  `sld:`) and `layergroups` (`layers` are layers of that workspace, in drawing
  order; optional `title` and `mode`, default `SINGLE`). A layer picks its
  default style with `style:`: a plain name refers to a style declared in its
  workspace, otherwise to an existing global style (e.g. `polygon`); a
  `workspace:name` form is also accepted. Order of operations: styles, layers,
  layer groups on create/update; with `--prune` groups, layers, styles and
  datastores are deleted in that order (styles still assigned to a layer are
  detached). GeoServer rewrites an SLD when storing it, so SLDs are compared
  semantically (ignoring namespace prefixes, whitespace, comments and the
  `Name` of `NamedLayer`/`UserStyle`/`FeatureTypeStyle`). A layer without
  `style:` leaves GeoServer's default unmanaged.
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
| `default_style`     | string | Optional: `<style>` (global) or `<workspace>:<style>`. Chosen by GeoServer if unset.   |
| `id`                | string | Computed, `<workspace>/<datastore>/<name>`.                                             |

Import with `terraform import geoserver_featuretype.roads acme/roads/roads`.

Bounding boxes are recalculated when `srs` or `projection_policy` changes, and
otherwise left alone. Unsetting `title`, `srs` or `projection_policy` keeps the
current value rather than resetting it. Destroying a feature type also deletes
its layer and removes it from any layer groups, since GeoServer cannot delete
one without the other.

**`geoserver_style`**

An SLD style scoped to a workspace.

| Attribute   | Type   | Notes                                                            |
|-------------|--------|------------------------------------------------------------------|
| `workspace` | string | Required. Changing it replaces the style.                        |
| `name`      | string | Required. Changing it replaces the style.                        |
| `sld`       | string | Required. Full SLD document, e.g. `file("roads.sld")`. Updated in place. |
| `id`        | string | Computed, `<workspace>/<name>`.                                  |

Import with `terraform import geoserver_style.roads acme/roads` (the `sld`
shows up as a diff on the next plan only if its content really differs; GeoServer's
re-serialization of the XML is ignored). Assign it to a layer with
`default_style = "<workspace>:<name>"` on `geoserver_featuretype` (a plain
`<name>` refers to a global style). Destroying a style detaches it from the
layers that use it.

**`geoserver_layer_group`**

| Attribute   | Type         | Notes                                                                 |
|-------------|--------------|-----------------------------------------------------------------------|
| `workspace` | string       | Required. Changing it replaces the group.                             |
| `name`      | string       | Required. Changing it replaces the group.                             |
| `title`     | string       | Optional.                                                             |
| `mode`      | string       | Optional, default `SINGLE`: `SINGLE`, `OPAQUE_CONTAINER`, `NAMED`, `CONTAINER` or `EO`. |
| `layers`    | list(string) | Required. Layer names of the same workspace, in drawing order.        |
| `id`        | string       | Computed, `<workspace>/<name>`.                                       |

Import with `terraform import geoserver_layer_group.base acme/base`. Bounds are
computed by GeoServer; each member keeps its current default style.

### Security: users, roles and access rules

Three resources manage the GeoServer default user/group service and the
data-access (layer) rules. The same model is available in the CLI through the
`security:` section of the YAML file
([`examples/cli/security.yaml`](examples/cli/security.yaml)):

```yaml
security:
  roles: [ROLE_EDITOR, ROLE_VIEWER]
  users:
    - name: alice
      password_env: ALICE_PASSWORD   # required; passwords are never written in the file
      enabled: true                  # optional, default true
      roles: [ROLE_EDITOR]
  rules:
    - workspace: demo
      layer: cities                  # optional, "*" = every layer of the workspace
      access: read                   # read | write
      roles: [ROLE_VIEWER, ROLE_EDITOR]
```

| Resource                          | Key attributes                                                                  | Import ID              |
|-----------------------------------|---------------------------------------------------------------------------------|------------------------|
| `geoserver_security_role`         | `name` (replaces on change)                                                     | role name              |
| `geoserver_security_user`         | `name`, `password` (sensitive), `enabled` (default `true`), `roles` (set)       | user name (no password)|
| `geoserver_security_layer_rule`    | `workspace`, `layer` (`*` = all), `access` (`read`/`write`), `roles` (set)      | `<ws>.<layer>.<r\|w>`  |

Behaviour notes:

- **Secrets**: passwords come from a sensitive Terraform variable or, in the
  CLI, from the environment variable named by `password_env`. They are never
  printed in plans, errors or logs. GeoServer never returns a password, so a
  password-only change made outside Geoform is not detected; the CLI re-sends
  it whenever the user changes, and Terraform whenever the configuration does.
- Roles referenced by users and rules must be declared (CLI) or exist (the
  built-in `ADMIN`, `GROUP_ADMIN`, `ROLE_AUTHENTICATED`, `ROLE_ANONYMOUS` and
  `*` are always accepted).
- Rule workspace and layer names cannot contain `.` (GeoServer joins the rule
  key with dots). Only `read` and `write` rules are managed; `admin` rules are
  left alone.
- `--prune` with a `security:` section also deletes users and roles missing from
  the file, and rules of the declared workspaces. It never deletes the `admin`
  user, the `ADMIN`/`GROUP_ADMIN` roles, global rules (`*.*.r`) or rules of
  workspaces not in the file.

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
