# MongoDB data source plugin for Grafana

A Grafana data source plugin (plugin ID `alandave-mongodb-datasource`) for querying and visualizing MongoDB data. It has a Go backend built on the official MongoDB driver (v2) and a React frontend with a Monaco JSON query editor.

User-facing documentation (configuration, query types, macros, formats, variables) lives in [src/README.md](./src/README.md), which is what ships with the plugin.

## Features

- **Five query types**: aggregation pipelines (the power tool — `$group`, `$lookup`, `$bucket`, window functions, anything the server supports), find (with projection/sort/limit/skip), count, distinct, and raw database commands as an escape hatch.
- **MongoDB extended JSON** everywhere, so BSON types like `{"$date": ...}` and `{"$oid": ...}` are expressible.
- **Time macros** replaced in the backend (`"$__timeFrom"`, `"$__timeTo"`, `"$__timeFrom_ms"`, `"$__timeTo_ms"`, `"$__interval_ms"`, `"$__maxDataPoints"`), so time-range-aware queries also work in alerting.
- **Three output formats**: table (nested documents flattened to dot notation), time series (long-to-wide pivot with string columns as series labels), and logs.
- **Template variables** interpolated in the collection, database, field and query text; multi-value variables render as JSON arrays so `{"host": {"$in": $host}}` works. Variables can themselves be populated by any query.
- **Secure credentials**: password stored in encrypted `secureJsonData`; connection string supports TLS, replica sets and Atlas SRV URIs.

## Requirements

- Node.js 22 (see `.nvmrc` — run `nvm use` in this directory)
- Go (version per `go.mod`) and [Mage](https://magefile.org/)
- Docker

## Development

```bash
# 1. Install frontend dependencies
nvm use
npm install

# 2. Build the backend binary for the Docker container
#    (linuxARM64 on Apple Silicon, linux for x86)
mage -v build:linuxARM64

# 3. Build the frontend (or `npm run dev` for watch mode)
npm run build

# 4. Start Grafana + a seeded MongoDB
docker compose up -d
```

Then open http://localhost:3000. The data source is auto-provisioned (see `provisioning/datasources/datasources.yml`) and points at the bundled MongoDB container.

### Rebuild loop

- Backend changes: `mage -v build:linuxARM64 && docker compose restart grafana`
- Frontend changes: `npm run dev` (or `npm run build`) — `dist/` is volume-mounted, just reload the browser
- Changes to `src/plugin.json` require a Grafana restart

## Sample data

`docker compose up` starts a `mongo:7` container seeded by [`dev/mongo-seed.js`](./dev/mongo-seed.js) into the `sampledb` database:

| Collection | Contents                                                                                  |
| ---------- | ----------------------------------------------------------------------------------------- |
| `metrics`  | 6 hours of per-minute CPU/memory metrics for hosts `web-01`, `web-02`, `db-01` — good for time series panels |
| `orders`   | 500 orders with `orderId`, `createdAt`, `status`, `region`, `amount`, nested `customer` — good for tables and aggregations |
| `logs`     | 300 log-style events with `time`, `level`, `service`, `message` — good for the logs format |

Timestamps are generated relative to container start, so "Last 6 hours" shows data. The seed script only runs on first start — delete the container (`docker compose down` then `up`) to re-seed.

Example query against the sample data (aggregate, format "Time series"):

```json
[
  { "$match": { "time": { "$gte": "$__timeFrom", "$lte": "$__timeTo" } } },
  { "$group": {
      "_id": { "host": "$host", "t": { "$dateTrunc": { "date": "$time", "unit": "minute", "binSize": 10 } } },
      "avg_cpu": { "$avg": "$cpu" }
  } },
  { "$project": { "_id": 0, "time": "$_id.t", "host": "$_id.host", "avg_cpu": 1 } },
  { "$sort": { "time": 1 } }
]
```

## Testing

```bash
go test ./pkg/...        # backend unit tests (macros, extended JSON parsing, framing)
npm run typecheck        # TypeScript
npm run lint             # ESLint
npm run e2e              # Playwright e2e tests (requires the docker stack to be up)
```

## Project layout

```
pkg/
  main.go                 # plugin entry point
  models/settings.go      # datasource settings (connection string, database, auth, timeout)
  plugin/
    datasource.go         # connection lifecycle, query dispatch, health check
    query.go              # query model, time macros, extended JSON parsing
    frame.go              # BSON documents -> Grafana data frames
src/
  datasource.ts           # frontend datasource: variable interpolation, variable support
  components/
    ConfigEditor.tsx      # connection + auth settings UI
    QueryEditor.tsx       # query type/collection/format controls + Monaco editor
dev/mongo-seed.js         # sample data for the dev MongoDB container
```

# Distributing the plugin

When distributing a Grafana plugin either within the community or privately the plugin must be signed so the Grafana application can verify its authenticity. This can be done with the `@grafana/sign-plugin` package.

_Note: It's not necessary to sign a plugin during development. The docker development environment that is scaffolded with `@grafana/create-plugin` caters for running the plugin without a signature._

## Initial steps

Before signing a plugin please read the Grafana [plugin publishing and signing criteria](https://grafana.com/legal/plugins/#plugin-publishing-and-signing-criteria) documentation carefully.

`@grafana/create-plugin` has added the necessary commands and workflows to make signing and distributing a plugin via the grafana plugins catalog as straightforward as possible.

Before signing a plugin for the first time please consult the Grafana [plugin signature levels](https://grafana.com/legal/plugins/#what-are-the-different-classifications-of-plugins) documentation to understand the differences between the types of signature level.

1. Create a [Grafana Cloud account](https://grafana.com/signup).
2. Make sure that the first part of the plugin ID matches the slug of your Grafana Cloud account.
   - _You can find the plugin ID in the `plugin.json` file inside your plugin directory. For example, if your account slug is `acmecorp`, you need to prefix the plugin ID with `acmecorp-`._
3. Create a Grafana Cloud API key with the `PluginPublisher` role.
4. Keep a record of this API key as it will be required for signing a plugin

## Signing a plugin

### Using Github actions release workflow

If the plugin is using the github actions supplied with `@grafana/create-plugin` signing a plugin is included out of the box. The [release workflow](./.github/workflows/release.yml) can prepare everything to make submitting your plugin to Grafana as easy as possible. Before being able to sign the plugin however a secret needs adding to the Github repository.

1. Please navigate to "settings > secrets > actions" within your repo to create secrets.
2. Click "New repository secret"
3. Name the secret "GRAFANA_API_KEY"
4. Paste your Grafana Cloud API key in the Secret field
5. Click "Add secret"

#### Push a version tag

To trigger the workflow we need to push a version tag to github. This can be achieved with the following steps:

1. Run `npm version <major|minor|patch>`
2. Run `git push origin main --follow-tags`

## Learn more

- [Grafana plugin development documentation](https://grafana.com/developers/plugin-tools/)
- [`plugin.json` documentation](https://grafana.com/developers/plugin-tools/reference/plugin-json)
- [MongoDB extended JSON](https://www.mongodb.com/docs/manual/reference/mongodb-extended-json/)
- [How to sign a plugin?](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin)
