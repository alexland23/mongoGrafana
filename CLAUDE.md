## Project knowledge

This repository contains a **Grafana plugin**. You must Read @./.config/AGENTS/instructions.md before doing changes.

MongoDB datasource plugin for Grafana (plugin ID `alandave-mongodb-datasource`), scaffolded with `@grafana/create-plugin`. Go backend (`pkg/`) using `mongo-driver` v2, React/TypeScript frontend (`src/`).

### Layout

- `pkg/plugin/` — datasource, query handling, macro interpolation, dataframe conversion
- `pkg/models/` — connection/settings models
- `src/components/` — ConfigEditor, QueryEditor
- `dev/mongo-seed.js` — seeds the local Mongo container's `sampledb` database (collections: `metrics`, `orders`, `logs`)
- `provisioning/datasources/` — auto-provisions the datasource into the dev Grafana instance
- `tests/` — Playwright e2e specs (`configEditor.spec.ts`, `queryEditor.spec.ts`)

### Build & run

Run `nvm use` first — the shell profile pins an EOL Node 18, but this plugin requires Node >=22.

- Backend: `mage -v build:linuxARM64` (swap target arch as needed)
- Frontend: `npm run build` (production) or `npm run dev` (watch mode)
- Dev stack: `docker compose up -d` (or `npm run server`) starts Grafana 13 on `localhost:3000` plus a `mongo:7` container seeded from `dev/mongo-seed.js`; re-seeding requires deleting the mongo container first

### Test & verify

- Go unit tests live alongside the code, e.g. `pkg/plugin/datasource_test.go` (macros, parsing, framing)
- `pkg/plugin/integration_test.go` — Go integration tests against a real MongoDB via testcontainers-go (all 5 query handlers, health check, auth-failure paths); gated behind the `integration` build tag (needs Docker) and excluded from `mage test` / plain `go test ./...`. Run with `go test -tags=integration ./pkg/plugin/... -run TestIntegration -v`
- `npm run test` (watch) / `npm run test:ci` — Jest unit tests
- `npm run typecheck`, `npm run lint` — TypeScript/ESLint checks
- `npm run e2e` — Playwright e2e tests (see @./.config/AGENTS/e2e-testing.md)
- Manually verify by exercising the feature against the seeded `sampledb` in Grafana at http://localhost:3000

See `FEATURE-IDEAS.md` for the current improvement roadmap.

### Scratch notes

`notes/` is a gitignored scratch space for `.md` files (checklists, PR review notes, etc.) — save files there when the user asks you to save off notes, since they're not meant to be committed.

### Git conventions

Never include a Claude Code session URL (`https://claude.ai/code/session_...`) in commit messages or PR descriptions in this repo — not as a `Claude-Session:` trailer, not anywhere in the body. `Co-Authored-By` trailers are fine to keep.
