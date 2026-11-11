SHELL := /bin/bash

GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)
export GOTOOLCHAIN ?= local

COMPOSE ?= docker compose

.PHONY: help setup build test vet fmt tidy db-up up down logs reset smoke \
        run-auth run-user run-event run-report run-gateway

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
