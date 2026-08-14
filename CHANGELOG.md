# Changelog

## 1.0.0 (Unreleased)

Initial release.

- Live log tailing via MongoDB change streams. Adds `"streaming": true` to `plugin.json` — **requires restarting the Grafana server** to pick up (a running dev instance will show the Live toggle as inert until restarted).
