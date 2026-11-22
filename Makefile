.PHONY: test-race security build build-cli install fmt vet test test-integration stack-up stack-down testacc testacc-all up down

BINARY       := terraform-provider-geoserver
COMPOSE_TEST  = docker compose -f docker-compose.test.yml

# GeoServer versions the provider is tested against; keep in sync with CI.
# The first one is the default for `make up` and `make testacc`.
GEOSERVER_VERSIONS := 2.28.5 2.25.4 2.24.4
GEOSERVER_VERSION  ?= $(firstword $(GEOSERVER_VERSIONS))
GEOSERVER_PORT     ?= 8080
POSTGIS_PORT       ?= 5432
export GEOSERVER_PORT POSTGIS_PORT GEOSERVER_VERSION

build:
	go build -o $(BINARY) .

# CLI independiente (geoform validate|plan|apply).
build-cli:
	go build -o geoform ./cmd/geoform

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

# Stack de tests (Kartoza GeoServer + PostGIS) para uso manual. Puertos y
# proyecto configurables: COMPOSE_PROJECT_NAME, GEOFORM_TEST_GEOSERVER_PORT,
# GEOFORM_TEST_POSTGIS_PORT (ver docs/testing.md).
stack-up:
	$(COMPOSE_TEST) up -d --wait --wait-timeout 300

stack-down:
	$(COMPOSE_TEST) down -v --remove-orphans

# Integration tests contra GeoServer + PostGIS reales. Cada ejecución usa un
# proyecto compose único y puertos libres (aislada de otras ejecuciones) y
# elimina siempre el stack, incluso si fallan los tests o se interrumpe.
test-integration:
	./scripts/integration.sh

# Unit tests con detector de carreras.
test-race:
	go test -race ./...

# Análisis de seguridad (vulnerabilidades y gosec); mismas herramientas que CI.
security:
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
	go run github.com/securego/gosec/v2/cmd/gosec@v2.22.10 -quiet ./...

# Terraform acceptance tests (docker-compose.yml, Kartoza GeoServer + seeded
# PostGIS): starts the stack, runs real
# terraform plan/apply cycles against it, and always tears it down, even when
# tests fail. The GEOSERVER_* connection variables are set only for this
# recipe so they do not leak into test-integration.
testacc: up
	@echo "Acceptance tests against GeoServer $(GEOSERVER_VERSION)"
	GEOSERVER_URL=$${GEOSERVER_URL:-http://localhost:$(GEOSERVER_PORT)/geoserver} \
	GEOSERVER_USERNAME=$${GEOSERVER_USERNAME:-admin} \
	GEOSERVER_PASSWORD=$${GEOSERVER_PASSWORD:-geoserver} \
	TF_ACC=1 go test ./internal/provider/... -v -count=1 -timeout 30m; \
		status=$$?; $(MAKE) down; exit $$status

# Acceptance tests against every supported version, one after another. Each
# run gets its own compose project so a failure never leaves stale state.
testacc-all:
	@failed=; for v in $(GEOSERVER_VERSIONS); do \
		echo "=== GeoServer $$v"; \
		COMPOSE_PROJECT_NAME=geoform-$$(echo $$v | tr . -) \
			$(MAKE) --no-print-directory testacc GEOSERVER_VERSION=$$v || failed="$$failed $$v"; \
	done; \
	if [ -n "$$failed" ]; then echo "FAILED on GeoServer:$$failed"; exit 1; fi; \
	echo "PASSED on GeoServer: $(GEOSERVER_VERSIONS)"

up:
	docker compose up -d --wait --wait-timeout 300

down:
	docker compose down -v
