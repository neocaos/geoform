# Tests y seguridad

## Niveles de test

| Comando | Qué ejecuta | Necesita |
|---|---|---|
| `make test` | unit tests | nada |
| `make test-race` | unit tests con `-race` | nada |
| `make test-integration` | tests `-tags=integration` contra Kartoza GeoServer + PostGIS reales | Docker |
| `make testacc` | acceptance (Terraform) | Docker + terraform |
| `make security` | `govulncheck` + `gosec` (igual que CI) | red |

## Aislamiento de los integration tests

`make test-integration` ejecuta `scripts/integration.sh`, que por cada ejecución:

1. usa un **proyecto de compose único** (`geoform-it-<pid>-<epoch>`),
2. deja que Docker elija **puertos libres** del host (solo loopback),
3. descubre la URL (`docker compose port`) y la exporta como `GEOSERVER_URL`,
4. elimina siempre el stack (`down -v`), aunque fallen los tests o se pulse Ctrl-C,
   conservando el código de salida de `go test`.

Así pueden correr varias ejecuciones a la vez (workers, CI, desarrolladores) sin pisarse.

Variables (todas opcionales):

| Variable | Efecto | Por defecto |
|---|---|---|
| `GEOFORM_IT_PROJECT` | nombre del proyecto compose | único por ejecución |
| `GEOFORM_IT_GEOSERVER_PORT` / `GEOFORM_IT_POSTGIS_PORT` | puertos fijos del host | libres |
| `GEOFORM_IT_PKGS`, `GEOFORM_IT_RUN` | paquetes y filtro `-run` de `go test` | `./internal/...`, todos |
| `GEOSERVER_URL`, `GEOSERVER_USER`, `GEOSERVER_PASS` | conexión que leen los tests (`internal/testenv`) | la que exporta el script |

Para usar el stack a mano (`make stack-up` / `make stack-down`), `docker-compose.test.yml`
acepta `COMPOSE_PROJECT_NAME`, `GEOFORM_TEST_GEOSERVER_PORT` (18080) y
`GEOFORM_TEST_POSTGIS_PORT` (15432); si se definen vacías, Docker elige puerto. Sin
`GEOSERVER_URL`, los tests apuntan a `http://127.0.0.1:$GEOFORM_TEST_GEOSERVER_PORT/geoserver`.

## Tests de seguridad

- `internal/infrastructure/cli/security_e2e_integration_test.go`: aplica roles, usuarios y reglas y
  comprueba que GeoServer las **hace cumplir** en WMS/WFS (anónimo 401, credenciales incorrectas 401,
  usuario sin rol 401/403, usuario autorizado 200, regla por capa, modos de catálogo HIDE y CHALLENGE),
  además de `plan` vacío tras `apply`, `--prune` y re-apply idempotente, y rotación de contraseña.
- `internal/infrastructure/cli/secrets_test.go` y `internal/geoserver/client_test.go`: ninguna contraseña
  (REST, PostGIS, usuarios) ni cabecera `Authorization` aparece en stdout/stderr/errores, ni siquiera cuando
  GeoServer repite el cuerpo de la petición en su error (`APIError` lo redacta como `[REDACTED]`).

## Postura de seguridad de la CLI

- Las contraseñas solo entran por variables de entorno (`password_env`, `GEOSERVER_PASSWORD`); el flag
  `--password` existe pero queda visible en la lista de procesos.
- La URL de GeoServer **no admite credenciales** (`user:pass@`); se rechaza sin imprimirla.
- `http://` fuera de loopback emite un aviso (la contraseña REST viajaría en claro).
- TLS se verifica siempre. `--ca-file` (o `GEOSERVER_CA_FILE`) añade CAs propias;
  `--insecure-skip-tls-verify` desactiva la verificación, es explícito y avisa.
- `apply`/`plan` detectan la **rotación de contraseña** de un usuario probando el login real
  (`password: rotar` en el plan); la contraseña viaja solo en la cabecera Authorization.

## Limitaciones conocidas (no resolubles desde Geoform)

1. **Caché de autenticación de GeoServer.** GeoServer cachea los logins correctos; tras rotar la contraseña,
   deshabilitar o **borrar** un usuario, las credenciales antiguas siguen sirviendo hasta que caduque la
   caché (minutos). No hay endpoint REST para vaciarla; reiniciar GeoServer o reducir el TTL lo acorta.
   La detección de rotación tampoco ve la contraseña antigua si se vuelve a ella dentro de ese plazo.
2. **Contraseñas de datastores.** GeoServer las enmascara al leerlas, así que un cambio solo de
   `password_env` en un datastore no se detecta en `plan` (se aplica si cambia otra cosa).
3. **Fuerza bruta.** GeoServer retrasa 1–5 s las respuestas 401 (brute-force prevention); la verificación de
   rotación con una contraseña errónea sufre ese retraso.
