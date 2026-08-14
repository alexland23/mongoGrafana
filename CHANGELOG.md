# Changelog

## 1.0.0 (Unreleased)

Initial release.

- TLS / advanced connection options in the config UI: a "TLS / Security" fieldset (enable, skip-verify, CA/client cert and key, each pastable or loadable from a file path on the plugin backend host) and an "Advanced connection options" fieldset (read preference, connect timeout, max pool size).
- Query builder mode: a Code/Builder toggle on aggregate queries. Builder mode gives a visual UI for match filters, a time-bucketed group-by with aggregations, sort and limit, compiling to the same extended-JSON pipeline used by Code mode.
- Live log tailing via MongoDB change streams. Adds `"streaming": true` to `plugin.json` — **requires restarting the Grafana server** to pick up (a running dev instance will show the Live toggle as inert until restarted).
- Explore & logs polish: configurable "Message field" / "Level field" mapping on logs-format queries, datasource-level derived fields (regex-extracted link columns, e.g. a trace ID), and a query editor cheat sheet with sample queries against the seeded collections.
- Integration test coverage against a real MongoDB via testcontainers-go, exercising all 5 query handlers, the health check, and auth-failure paths, plus Jest coverage for `filterQuery` / `applyTemplateVariables`.
- Query safety & observability: a configurable `maxDocuments` guard injected server-side (`SetLimit` on find, a trailing `$limit` stage on aggregate); an operator/command safety denylist (blocking `$out`, `$merge`, `$where`, `$function`, `$accumulator` and admin commands like `dropDatabase`/`shutdown`/`eval` by default, extensible or disable-able per datasource); `executedQueryString` in frame meta so the panel inspector shows the post-macro query; an "Explain" button in the query editor showing the MongoDB query plan; and error classification based on MongoDB server error codes instead of a "message contains 'invalid'" heuristic.
