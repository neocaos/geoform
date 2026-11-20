-- Datos de ejemplo para probar Geoform contra GeoServer.
-- Se ejecuta automáticamente al primer arranque del contenedor postgis
-- (montado en /docker-entrypoint-initdb.d).
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE SCHEMA IF NOT EXISTS geodata;

-- Puntos: ciudades
CREATE TABLE geodata.cities (
    id         serial PRIMARY KEY,
    name       text NOT NULL,
    population integer,
    geom       geometry(Point, 4326) NOT NULL
);
CREATE INDEX cities_geom_idx ON geodata.cities USING GIST (geom);
INSERT INTO geodata.cities (name, population, geom) VALUES
    ('Madrid',    3300000, ST_SetSRID(ST_MakePoint(-3.7038, 40.4168), 4326)),
    ('Barcelona', 1620000, ST_SetSRID(ST_MakePoint(2.1734, 41.3851), 4326)),
    ('Valencia',   800000, ST_SetSRID(ST_MakePoint(-0.3763, 39.4699), 4326)),
    ('Sevilla',    690000, ST_SetSRID(ST_MakePoint(-5.9845, 37.3891), 4326));

-- Líneas: carreteras
CREATE TABLE geodata.roads (
    id   serial PRIMARY KEY,
    name text NOT NULL,
    geom geometry(LineString, 4326) NOT NULL
);
CREATE INDEX roads_geom_idx ON geodata.roads USING GIST (geom);
INSERT INTO geodata.roads (name, geom) VALUES
    ('A-2 Madrid-Barcelona', ST_GeomFromText('LINESTRING(-3.7038 40.4168, -1.1307 41.6488, 2.1734 41.3851)', 4326)),
    ('A-4 Madrid-Sevilla',   ST_GeomFromText('LINESTRING(-3.7038 40.4168, -4.7727 37.8882, -5.9845 37.3891)', 4326)),
    ('A-3 Madrid-Valencia',  ST_GeomFromText('LINESTRING(-3.7038 40.4168, -0.3763 39.4699)', 4326));

-- Polígonos: regiones
CREATE TABLE geodata.regions (
    id   serial PRIMARY KEY,
    name text NOT NULL,
    geom geometry(Polygon, 4326) NOT NULL
);
CREATE INDEX regions_geom_idx ON geodata.regions USING GIST (geom);
INSERT INTO geodata.regions (name, geom) VALUES
    ('Centro',  ST_GeomFromText('POLYGON((-4.5 39.5, -3 39.5, -3 41, -4.5 41, -4.5 39.5))', 4326)),
    ('Levante', ST_GeomFromText('POLYGON((-1.5 38.5, 0.5 38.5, 0.5 40, -1.5 40, -1.5 38.5))', 4326)),
    ('Cataluña',ST_GeomFromText('POLYGON((0.5 40.6, 3.2 40.6, 3.2 42.5, 0.5 42.5, 0.5 40.6))', 4326));

ANALYZE;
