.PHONY: build install fmt vet test test-integration stack-up stack-down testacc up down

BINARY       := terraform-provider-geoserver
COMPOSE_TEST  = docker compose -f docker-compose.test.yml

build:
	go build -o $(BINARY) .

# Installs into $GOBIN (or $GOPATH/bin) for use with a dev_overrides block.
install:
	go install .

fmt:
	gofmt -w .

vet:
	go vet ./...

# Unit tests: fast, no Docker needed.
test:
	go test ./...

# Levanta el stack de tests (Kartoza GeoServer + PostGIS) y espera a que esté healthy
stack-up:
	$(COMPOSE_TEST) up -d --wait --wait-timeout 300

stack-down:
	$(COMPOSE_TEST) down -v

# Integration tests contra GeoServer + PostGIS reales (docker-compose.test.yml).
# El stack se elimina siempre, incluso si los tests fallan; se conserva el exit code de go test.
test-integration:
	$(COMPOSE_TEST) up -d --wait --wait-timeout 300 || { $(COMPOSE_TEST) logs --tail=50; $(COMPOSE_TEST) down -v; exit 1; }
	go test -count=1 -tags=integration ./internal/... -v; rc=$$?; $(COMPOSE_TEST) down -v; exit $$rc

# Terraform acceptance tests (docker-compose.yml): starts GeoServer, runs real
# terraform plan/apply cycles against it, and always tears it down, even when
# tests fail.
GEOSERVER_PORT ?= 8080
export GEOSERVER_PORT

testacc: up
	GEOSERVER_URL=$${GEOSERVER_URL:-http://localhost:$(GEOSERVER_PORT)/geoserver} \
	GEOSERVER_USERNAME=$${GEOSERVER_USERNAME:-admin} \
	GEOSERVER_PASSWORD=$${GEOSERVER_PASSWORD:-geoserver} \
	TF_ACC=1 go test ./internal/provider/... -v -count=1 -timeout 30m; \
		status=$$?; $(MAKE) down; exit $$status

up:
	docker compose up -d --wait

down:
	docker compose down -v
