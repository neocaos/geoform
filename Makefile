BINARY         := terraform-provider-geoserver
GEOSERVER_PORT ?= 8080

export GEOSERVER_PORT
export GEOSERVER_URL      ?= http://localhost:$(GEOSERVER_PORT)/geoserver
export GEOSERVER_USERNAME ?= admin
export GEOSERVER_PASSWORD ?= geoserver

.PHONY: build install fmt vet test testacc up down

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
	TF_ACC=1 go test ./internal/provider/... -v -count=1 -timeout 30m; \
		status=$$?; $(MAKE) down; exit $$status

up:
	docker compose up -d --wait

down:
	docker compose down -v
