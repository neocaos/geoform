.PHONY: test test-integration test-integration-tc

# Unit tests (rápidos, sin Docker)
test:
	go test ./...

# Integration tests contra GeoServer real vía docker-compose manual
test-integration:
	docker compose -f docker-compose.test.yml up -d --wait
	go test -tags=integration ./internal/infrastructure/geoserver/... -v
	docker compose -f docker-compose.test.yml down -v

# Alternativa: integration tests vía testcontainers-go (no requiere el paso up/down de arriba)
test-integration-tc:
	go test -tags=integration ./internal/infrastructure/geoserver/... -v