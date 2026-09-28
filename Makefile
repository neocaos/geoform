BINARY         := terraform-provider-geoserver
GEOSERVER_PORT ?= 8080

# GeoServer versions the provider is tested against; keep in sync with CI.
# The first one is the default for `make up` and `make testacc`.
GEOSERVER_VERSIONS := 2.28.5 2.25.7 2.24.5
GEOSERVER_VERSION  ?= $(firstword $(GEOSERVER_VERSIONS))

export GEOSERVER_PORT GEOSERVER_VERSION
export GEOSERVER_URL      ?= http://localhost:$(GEOSERVER_PORT)/geoserver
export GEOSERVER_USERNAME ?= admin
export GEOSERVER_PASSWORD ?= geoserver

.PHONY: build install fmt vet test testacc testacc-all up down

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

# Acceptance tests: starts GeoServer, runs real terraform plan/apply cycles
# against it, and always tears it down, even when tests fail.
testacc: up
	@echo "Acceptance tests against GeoServer $(GEOSERVER_VERSION)"
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
	docker compose up -d --wait

down:
	docker compose down -v
