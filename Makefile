SHELL := /bin/bash

GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)
export GOTOOLCHAIN ?= local

COMPOSE ?= docker compose
STAGING_COMPOSE = COMPOSE_DISABLE_ENV_FILE=1 $(COMPOSE) --env-file /dev/null --project-name fayrly-staging -f compose.staging.yml

.PHONY: help setup build test vet fmt tidy db-up up down logs reset smoke \
        run-auth run-user run-event run-report run-gateway \
        staging-check staging-build staging-up staging-status

help:
	@echo "Fayrly backend"
	@echo ""
	@echo "  make setup        copy .env.example to .env (if absent)"
	@echo "  make up           build images and start the full stack (docker compose)"
	@echo "  make down         stop the stack"
	@echo "  make logs         follow logs from every container"
	@echo "  make reset        stop the stack and delete the database volume"
	@echo "  make smoke        run end-to-end API checks against http://localhost:8080"
	@echo ""
	@echo "  make db-up        start only PostgreSQL (for local 'go run' development)"
	@echo "  make run-auth     run one service locally in the foreground"
	@echo "                    (run-auth, run-user, run-event, run-report, run-gateway)"
	@echo ""
	@echo "  make build        go build ./..."
	@echo "  make test         go test ./..."
	@echo "  make vet          go vet ./..."
	@echo "  make fmt          go fmt ./..."
	@echo "  make tidy         go mod tidy"
	@echo ""
	@echo "  make staging-check   validate staging environment without reading .env"
	@echo "  make staging-build   build staging images"
	@echo "  make staging-up      start staging and wait for all services to be ready"
	@echo "  make staging-status  show staging container status"
	@echo "  Required: STAGING_POSTGRES_PASSWORD, STAGING_JWT_SECRET, STAGING_INTERNAL_API_KEY"
	@echo "            STAGING_CORS_ALLOWED_ORIGINS (comma-separated HTTPS origins)"
	@echo "  Secrets: generate each independently with openssl rand -hex 32; keep them in a secret manager"
	@echo "  Optional: STAGING_FIREBASE_PROJECT_ID, STAGING_RESEND_API_KEY + STAGING_MAIL_FROM"
	@echo "  Gateway: 127.0.0.1:18080 by default; put an HTTPS reverse proxy in front"
	@echo "  Bootstrap admin is disabled; staging starts with an empty database"

staging-check:
	@bash scripts/staging-check.sh
	@$(STAGING_COMPOSE) config --quiet

staging-build: staging-check
	@$(STAGING_COMPOSE) build

staging-up: staging-check
	@$(STAGING_COMPOSE) up --build --detach --wait --wait-timeout 180

staging-status: staging-check
	@$(STAGING_COMPOSE) ps

setup:
	@if [ -f .env ]; then echo ".env already exists; leaving it untouched"; else cp .env.example .env; echo "created .env"; fi

build:
	$(GO) build ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

db-up:
	$(COMPOSE) up -d postgres

up:
	$(COMPOSE) up --build -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

reset:
	$(COMPOSE) down -v

smoke:
	./scripts/smoke.sh

run-%:
	@test -f .env || { echo "missing .env; run: make setup"; exit 1; }
	@set -a; . ./.env; set +a; $(GO) run ./cmd/$*
