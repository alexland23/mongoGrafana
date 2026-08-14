# Changelog

## 1.0.0 (Unreleased)

Initial release.

- Live log tailing via MongoDB change streams. Adds `"streaming": true` to `plugin.json` — **requires restarting the Grafana server** to pick up (a running dev instance will show the Live toggle as inert until restarted).
- Explore & logs polish: configurable "Message field" / "Level field" mapping on logs-format queries, datasource-level derived fields (regex-extracted link columns, e.g. a trace ID), and a query editor cheat sheet with sample queries against the seeded collections.
