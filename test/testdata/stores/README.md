# Fixtures de ficheros

Ficheros mínimos para los tests de GeoPackage, shapefile y GeoTIFF. Los stacks de
compose los montan en `/data/stores` dentro de GeoServer.

- `places.gpkg` / `places.shp` (+ `.shx`, `.dbf`, `.prj`): tabla `places`, 2 puntos (EPSG:4326).
- `dem.tif`: raster Byte de 8×8 píxeles, EPSG:4326, extensión (-10, 36, 4, 44).

Se generaron con GDAL, p. ej.:

```sh
ogr2ogr -f "ESRI Shapefile" places.shp places.geojson -nln places -a_srs EPSG:4326
ogr2ogr -f GPKG places.gpkg places.geojson -nln places -a_srs EPSG:4326
gdal_create -of GTiff -outsize 8 8 -bands 1 -ot Byte -a_srs EPSG:4326 -a_ullr -10 44 4 36 -burn 7 dem.tif
```
