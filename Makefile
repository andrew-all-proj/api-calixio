DB_URL ?= postgres://postgres:postgres@localhost:5432/livekit?sslmode=disable
GOOSE ?= go run github.com/pressly/goose/v3/cmd/goose@latest
MIGRATIONS_DIR ?= migrations

.PHONY: migrate-up migrate-down migrate-status migrate-create

migrate-up:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DB_URL)" up

migrate-down:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DB_URL)" down

migrate-status:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DB_URL)" status

migrate-create:
	@if [ -z "$(name)" ]; then echo "Usage: make migrate-create name=add_table"; exit 1; fi
	$(GOOSE) -dir $(MIGRATIONS_DIR) create $(name) sql
