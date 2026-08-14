# MongoDB Grafana Datasource — Feature Ideas & Improvement Plan

## Where the plugin stands today

The plugin (`alandave-mongodb-datasource`) is already a solid backend datasource:

- **5 query types** — aggregate, find, count, distinct, command — written in extended JSON via a Monaco editor
- **6 server-side time macros** (`$__timeFrom`, `$__timeTo`, `_ms` variants, `$__interval_ms`, `$__maxDataPoints`), so alerting works
- **3 output formats** — table (flattened dot-notation), timeseries (long→wide pivot), logs
- Dashboard variable support, pooled connections per datasource instance, health checks
- User docs, Go unit tests for macros/parsing/framing, Playwright e2e smoke tests

The ideas below would move it from "works" to "pleasant to use and competitive with the commercial MongoDB datasources" (e.g. Grafana Labs' enterprise plugin). Grouped by impact vs. effort, with the key files each would touch.

---

## Tier 1 — High impact, moderate effort (recommended first)

### 1. Schema discovery: database / collection / field autocomplete — done
Today database, collection, and field are free-text inputs — the biggest UX gap. Add a backend **`CallResource` handler** exposing endpoints like `/databases`, `/collections?db=x`, `/fields?db=x&collection=y` (fields via a `$sample` + key-scan aggregation). The frontend swaps the text inputs for async `Combobox`es fed by `getResource()`.

Make discovery **opt-in, not on-by-default**: on very large clusters, listing databases/collections or sampling for field discovery can be slow or expensive, and admins may not want every collection exposed to dashboard authors. Two settings knobs, both in `jsonData` (non-sensitive, so no `secureJsonData`):

- **Enable schema discovery** — a toggle in the config editor (default off). When off, `/databases`, `/collections`, `/fields` all return a "disabled" response (or are unregistered) and the frontend falls back to today's free-text inputs.
- **Collection filtering** — allow-list and/or deny-list glob patterns (e.g. `sampledb.*`, `!*.system.*`, `!*_internal`) evaluated by the `/databases` and `/collections` resource handlers before returning results, so restricted collections never reach the frontend regardless of discovery being enabled. Field discovery inherits the same filter (no `/fields` for a hidden collection).

- Backend: new `pkg/plugin/resource.go`, register `CallResourceHandler` in `pkg/plugin/datasource.go`; filter/glob matching plus the enable flag in `pkg/models/settings.go`
- Frontend: `src/components/ConfigEditor.tsx` (discovery toggle + filter pattern list), `src/components/QueryEditor.tsx` (fall back to text inputs when discovery is disabled or `getResource()` errors), `src/datasource.ts`
- Bonus: the same field list can drive Monaco autocomplete inside the query editor

### 2. Dedicated variable query editor
Variables currently work via generic `DataSourceVariableSupport` ("first column = values"). Upgrade to `CustomVariableSupport` with a small variable editor (collection + distinct-field or pipeline) and explicit `__text` / `__value` column mapping, so variables can show friendly labels while filtering on `_id`s.

- Frontend: `src/datasource.ts`, new `src/components/VariableQueryEditor.tsx`, `src/module.ts`

### 3. Annotation query editor — done
`plugin.json` declares `annotations: true` but there was no dedicated editor — users got the raw query editor with no guidance on required columns. Registered `annotations: AnnotationSupport<MongoQuery>` on the datasource with a default example query; Grafana's standard frame > event mapping and its built-in per-annotation "Mapping" section (time/timeEnd/title/text/tags) handle the rest, no custom editor needed. Documented the expected columns in `src/README.md`.

- Frontend: `src/datasource.ts`, `src/types.ts`, `src/README.md`

### 4. More macros, especially `$__timeFilter`
Add the macros people reach for from the SQL datasources:

- `$__timeFilter(field)` → expands to a full `{ field: { $gte: ..., $lte: ... } }` match clause — a huge boilerplate saver in every dashboard query
- `$__interval` / `$from` / `$to` (non-`_ms` forms) so queries behave identically in panels and alerting
- `$__unixEpochFrom` / `$__unixEpochTo` (seconds) for collections storing epoch-seconds

- Backend: `pkg/plugin/query.go` (`interpolateMacros`), tests in `pkg/plugin/datasource_test.go`, docs in `src/README.md`

---

## Tier 2 — Strong differentiators, more effort

### 5. Query builder mode (visual editor)
A toggle between "Code" (current Monaco) and "Builder": pick collection → add match filters (field/operator/value rows) → optional group-by (time bucket via `$dateTrunc` + sum/avg/count) → sort/limit. Builder state compiles to a pipeline shown in the code editor. This is what makes the plugin approachable for non-Mongo experts, and it's the headline feature of the paid alternatives.

- Frontend: new `src/components/QueryBuilder/` components, extend `MongoQuery` in `src/types.ts` (keep raw + builder state, `editorMode` flag)
- Backend: unchanged (still receives a pipeline)

### 6. Live streaming for log tailing — done
Implemented `StreamHandler` (`SubscribeStream`/`RunStream`) using MongoDB **change streams** (replica-set/Atlas only, with graceful fallback to polling by `_id` for standalone deployments). A "Live" toggle on logs-format queries tails the selected collection in Explore, seeded with a recent backlog on subscribe. Added the `streaming: true` capability flag; `datasource.Manage` wires up the stream handlers automatically, no `pkg/main.go` change needed. **The `plugin.json` change requires restarting the Grafana server** — a running dev instance won't pick up the Live toggle until restarted.

- Backend: new `pkg/plugin/stream.go` (reuses `queryModel`/`parseDocument`/`docsToFrame` from the regular query path)
- Frontend: `src/datasource.ts` (`query()` override routes live "logs" targets through `getGrafanaLiveSrv().getDataStream`), `src/components/QueryEditor.tsx` (Live toggle), `src/types.ts`, `src/plugin.json`
- Frontend: `src/datasource.ts` (channel support), `src/plugin.json`

### 7. TLS / advanced connection options in the config UI — done
Added a "TLS / Security" fieldset (enable TLS, skip-verify toggle, CA cert / client cert / key as secure JSON textareas, each with an alternate "...or path" field so certs can be loaded from a file on the plugin backend host instead of pasted) and an "Advanced connection options" fieldset (read preference, connect timeout, max pool size) to the config editor. The backend builds a `tls.Config` by reading each cert from its path when set (falling back to the pasted content), and applies read preference / connect timeout / max pool size to the Mongo client options.

- Frontend: `src/components/ConfigEditor.tsx`, `src/types.ts`
- Backend: `pkg/models/settings.go`, client options in `pkg/plugin/datasource.go`; tests in `pkg/models/settings_test.go`, `pkg/plugin/datasource_tls_test.go`

### 8. Explore & logs polish
- Configurable **log field mapping** (which field is the message, which is the level) instead of relying on a `message` field by convention
- A `QueryEditorHelp` cheat-sheet component with sample queries for the seeded collections
- Derived fields / data links for logs (e.g. extract a trace ID)

- Frontend: `src/module.ts`, new help component, `src/components/QueryEditor.tsx`
- Backend: `pkg/plugin/frame.go` (log meta)

---

## Tier 3 — Nice-to-haves / hardening

### 9. Integration tests against real Mongo
Go tests currently cover only macros/parsing/framing. Add integration tests (testcontainers-go, or the existing docker-compose mongo) exercising all 5 query handlers, health check, and auth-failure paths. Add Jest tests for `filterQuery` / `applyTemplateVariables`.

- `pkg/plugin/*_test.go`, `src/**/*.test.tsx`

### 10. Query safety & observability
- A `maxDocuments` guard (server-side limit injection) so a careless `find {}` can't OOM the plugin
- Set `executedQueryString` in frame meta (the post-macro query) so it shows in the panel inspector — great for debugging
- Optional "Explain" button in the editor showing the query plan
- Better error classification than the current "message contains 'invalid'" heuristic — use Mongo server error codes

- Backend: `pkg/plugin/datasource.go`, `pkg/plugin/query.go`, `pkg/plugin/frame.go`

### 11. Frame conversion improvements
- Preserve integer types instead of coercing all numbers to float64
- Optional flatten-depth / raw-JSON-column mode for deeply nested documents
- An explicit "long" format option (today `LongToWide` failure silently falls back with a notice)

- Backend: `pkg/plugin/frame.go`

### 12. Distribution readiness
- `git init` + push — the repo isn't version-controlled yet!
- Plugin signing, screenshots/logo in `src/img`, a CHANGELOG, provisioning docs — all prerequisites for submitting to the Grafana plugin catalog

---

## Suggested first batch

Best value-for-effort sequence:

1. **#4 macros** — small, pure backend, immediately useful in every query
2. **#1 schema discovery** — the biggest day-to-day UX win
3. **#3 annotations + #2 variable editor** — closes out the "declared but not really supported" capabilities

## How to verify whatever gets built

`mage -v build:linuxARM64` + `npm run build` (run `nvm use` first — the shell profile pins EOL Node 18), then `docker compose up -d` and exercise the feature against the seeded `sampledb` (metrics / orders / logs) in Grafana at http://localhost:3000. Add Go unit tests alongside `pkg/plugin/datasource_test.go` and extend the Playwright specs in `tests/` for UI-facing changes.
