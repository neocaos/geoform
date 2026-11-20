.PHONY: test test-integration stack-up stack-down

COMPOSE_TEST = docker compose -f docker-compose.test.yml

# Unit tests (rápidos, sin Docker)
test:
	go test ./...

# Levanta el stack de tests y espera a que esté healthy
stack-up:
	$(COMPOSE_TEST) up -d --wait --wait-timeout 300

stack-down:
	$(COMPOSE_TEST) down -v

# Integration tests contra GeoServer + PostGIS reales (docker compose).
# El stack se elimina siempre, incluso si los tests fallan; se conserva el exit code de go test.
test-integration:
	$(COMPOSE_TEST) up -d --wait --wait-timeout 300 || { $(COMPOSE_TEST) logs --tail=50; $(COMPOSE_TEST) down -v; exit 1; }
	go test -count=1 -tags=integration ./internal/... -v; rc=$$?; $(COMPOSE_TEST) down -v; exit $$rc
