SHELL := /bin/bash

.PHONY: help setup install build build-frontend build-backend up down restart \
	dev logs clean-stale reseed test typecheck lint e2e

help:
	@echo "Targets:"
	@echo "  make setup           - install deps, build frontend+backend, start dev stack"
	@echo "  make install         - npm install (Node $$(cat .nvmrc) via nvm)"
	@echo "  make build           - build frontend and backend"
	@echo "  make build-frontend  - webpack production build"
	@echo "  make build-backend   - mage build + chmod dist/gpx_* binaries"
	@echo "  make up              - docker compose up -d (Grafana + Mongo)"
	@echo "  make down            - docker compose down"
	@echo "  make restart         - down, then up"
	@echo "  make dev             - webpack watch mode (frontend)"
	@echo "  make logs            - tail Grafana container logs"
	@echo "  make reseed          - drop mongo container/volume and restart to re-run seed script"
	@echo "  make clean-stale     - remove stopped containers left from previous runs"
	@echo "  make test            - jest unit tests (CI mode)"
	@echo "  make typecheck       - tsc --noEmit"
	@echo "  make lint            - eslint"
	@echo "  make e2e             - playwright e2e tests"

# Run an npm script under the Node version pinned in .nvmrc.
define NVM_RUN
	source ~/.nvm/nvm.sh && nvm use >/dev/null && $(1)
endef

setup: install build up

install:
	$(call NVM_RUN,npm install)

build: build-frontend build-backend

build-frontend:
	$(call NVM_RUN,npm run build)

build-backend:
	mage -v
	chmod 0755 dist/gpx_*

up:
	docker compose up -d

down:
	docker compose down

restart: down up

dev:
	$(call NVM_RUN,npm run dev)

logs:
	docker compose logs -f grafana

reseed:
	docker compose stop mongo
	docker compose rm -f mongo
	docker compose up -d mongo

clean-stale:
	docker compose rm -f

test:
	$(call NVM_RUN,npm run test:ci)

typecheck:
	$(call NVM_RUN,npm run typecheck)

lint:
	$(call NVM_RUN,npm run lint)

e2e:
	$(call NVM_RUN,npm run e2e)
