# Releases

Las releases se generan con [GoReleaser](https://goreleaser.com) desde GitHub
Actions (`.github/workflows/release.yml`) al empujar un tag `vX.Y.Z`.

```sh
git tag v0.1.0
git push origin v0.1.0
```

## Artefactos

Para linux, darwin y windows (amd64 y arm64):

| Artefacto | Contenido |
|-----------|-----------|
| `geoform_<versión>_<os>_<arch>.tar.gz` (`.zip` en windows) | CLI `geoform` |
| `terraform-provider-geoserver_<versión>_<os>_<arch>.zip` | provider de Terraform |
| `geoform_<versión>_SHA256SUMS` | checksums de todos los artefactos |
| `geoform_<versión>_SHA256SUMS.sig` | firma GPG (solo si hay clave configurada) |
| `terraform-registry-manifest.json` | manifiesto del registry (protocolo 6) |

## Firma GPG

El registry público de Terraform exige `SHA256SUMS` firmado. Define el secret
`GPG_PRIVATE_KEY` (clave privada ASCII-armored, sin passphrase) y la release se
firmará; sin él se publica sin firma.

## Probar en local

```sh
goreleaser release --snapshot --clean --skip=sign,publish
```
