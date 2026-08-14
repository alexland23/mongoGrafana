# MongoDB data source for Grafana

> **Note:** This is an unofficial, community-built plugin. It is not developed, maintained, or endorsed by MongoDB, Inc. or Grafana Labs.

Query and visualize MongoDB data in Grafana. Write aggregation pipelines, find filters, counts, distinct-value queries or raw database commands in MongoDB extended JSON, and render the results as tables, time series or logs.

## Configuration

| Option            | Description                                                                                             |
| ----------------- | ------------------------------------------------------------------------------------------------------- |
| Connection string | MongoDB URI, e.g. `mongodb://localhost:27017` or `mongodb+srv://cluster.example.net`. URI options such as `tls`, `replicaSet` and `authSource` are supported. |
| Default database  | Database used by queries unless overridden per query.                                                    |
| Query timeout     | Maximum execution time per query in seconds (default 30).                                                |
| Username/Password | Optional authentication. The password is stored encrypted and never leaves the backend.                  |

## Query safety

Two settings guard against a careless or malicious query, independent of the MongoDB user's own permissions:

- **Max documents** caps how many documents a find or aggregate query can return — enforced server-side (`SetLimit` on find, a trailing `$limit` stage on aggregate) regardless of what a query or panel requests. Defaults to 10000; a negative value disables it.
- **Operator/command safety** rejects a small denylist before it ever reaches MongoDB: the pipeline/filter operators `$out`, `$merge` (which can turn this nominally read-only datasource into a write path), `$where`, `$function`, `$accumulator` (arbitrary server-side JavaScript execution), and the admin commands `dropDatabase`, `shutdown`, `eval` for the Command query type. Add extra entries in the datasource config, or turn the check off entirely for datasources that intentionally need one of them (e.g. `$merge` for materialized views).

As always, the strongest guarantee is granting the MongoDB user in the connection string read-only roles at the database level — these settings are a backstop, not a substitute.

## Query types

- **Aggregate** (recommended) — a full aggregation pipeline as an extended JSON array. This is the most powerful option: `$match`, `$group`, `$project`, `$lookup`, `$bucket`, window functions — anything the server supports.
- **Find** — a filter document plus optional projection, sort, limit and skip.
- **Count** — count of documents matching a filter.
- **Distinct** — distinct values of a field, optionally filtered.
- **Command** — a raw database command document (`runCommand`), as an escape hatch.

Aggregate and Find queries have an **Explain** button that runs MongoDB's `explain` command and shows the query plan in a modal. Time macros are not substituted for Explain, since there's no dashboard time range at that point — use literal values, or check the post-macro query the panel inspector's JSON tab shows under `meta.executedQueryString` after running the query for real.

## Time macros

Macros are replaced server-side before the query runs, so they also work in alerting:

| Macro                          | Replaced with                                         |
| ------------------------------ | ------------------------------------------------------ |
| `$__timeFilter(field)`         | A full match clause: `{ field: { "$gte": ..., "$lte": ... } }` on a BSON date field |
| `$__unixEpochFilter(field)`    | A full match clause on an epoch-seconds numeric field |
| `$__timeGroup(field[, interval])` | A `$dateTrunc` expression bucketing a BSON date field, e.g. for `$group._id` |
| `$__unixEpochGroup(field[, interval])` | An arithmetic bucketing expression for an epoch-seconds numeric field |
| `"$__timeFrom"`                | Dashboard range start as a BSON date                   |
| `"$__timeTo"`                  | Dashboard range end as a BSON date                     |
| `"$__timeFrom_ms"`             | Range start as epoch milliseconds (number)              |
| `"$__timeTo_ms"`               | Range end as epoch milliseconds (number)                |
| `"$__from"`                    | Range start as epoch milliseconds (number)              |
| `"$__to"`                       | Range end as epoch milliseconds (number)                |
| `"$__unixEpochFrom"`           | Range start as epoch seconds (number)                   |
| `"$__unixEpochTo"`             | Range end as epoch seconds (number)                     |
| `"$__interval"`                | Suggested group-by interval, e.g. `"30s"` (string)      |
| `"$__interval_ms"`             | Suggested group-by interval in milliseconds             |
| `"$__maxDataPoints"`           | Panel max data points (handy for `$limit`)              |

`$__timeFilter(field)`, `$__unixEpochFilter(field)`, `$__timeGroup(...)` and `$__unixEpochGroup(...)` all expand to a whole clause or expression, not a value, so use them unquoted:

```json
[{ "$match": $__timeFilter(time) }]
```

`$__timeGroup` and `$__unixEpochGroup` take an optional interval argument (`"30s"`, `"5m"`, `"1h"`, `"1d"`, `"1w"`); if omitted, they use the dashboard's suggested `$__interval`.

Example — average CPU per host bucketed by 10 minutes, limited to the dashboard time range:

```json
[
  { "$match": $__timeFilter(time) },
  { "$group": {
      "_id": { "host": "$host", "t": $__timeGroup(time, "10m") },
      "avg_cpu": { "$avg": "$cpu" }
  } },
  { "$project": { "_id": 0, "time": "$_id.t", "host": "$_id.host", "avg_cpu": 1 } },
  { "$sort": { "time": 1 } }
]
```

With format **Time series**, rows containing a date field, numeric fields and optional string label fields are pivoted into one series per label value (`avg_cpu {host=web-01}`, …).

## Formats

- **Table** — rows as returned; nested documents are flattened with dot notation, arrays are rendered as JSON.
- **Time series** — long-to-wide conversion using the first date column as time and string columns as labels.
- **Logs** — renders in the logs visualization; include a date field and a `message`-like field. If your collection names those fields something else, set "Message field" / "Level field" in the query editor to remap them (blank keeps the `message`/`level` convention). A query editor cheat sheet with sample queries against the seeded `metrics`, `orders` and `logs` collections is available from Explore's "Kick start your query" panel.

## Template variables

Dashboard variables are interpolated in the collection, database, field and query text (multi-value variables become JSON arrays, so `{"host": {"$in": $host}}` works). To populate a variable from MongoDB, choose this data source in the variable editor and write any query — the first column supplies the values (the **Distinct** type is a natural fit).

## Annotations

Any query can drive dashboard annotations — write it with the regular query editor, in the "Annotations" section of dashboard settings. The result is mapped into annotation events by column name:

| Column     | Required | Meaning                                    |
| ---------- | -------- | ------------------------------------------ |
| `time`     | yes      | Event timestamp                            |
| `timeEnd`  | no       | End of a region annotation                 |
| `title`    | no       | Annotation title                           |
| `text`     | no       | Annotation body                            |
| `tags`     | no       | Comma-separated tag string (BSON arrays are flattened to a JSON string, not split into tags) |

If your columns are named differently, remap them from the "Mapping" section of the annotation editor instead of renaming fields in the query. A new annotation query starts pre-filled with an example that projects the seeded `logs` collection's error events into this shape:

```json
[
  { "$match": { "level": "error" } },
  { "$project": { "time": 1, "title": "$level", "text": "$message", "tags": "$service" } },
  { "$limit": 100 }
]
```

## Derived fields (logs)

Configure "Derived fields" on the datasource to pull extra clickable link columns out of logs results, e.g. a trace ID linked to a tracing UI. Each rule has:

- **Regex** — matched against the message field's text; the first capture group becomes the link value (or the whole match if the pattern has no group)
- **Field name** — name of the new column
- **URL** — link target; supports the `${__value.raw}` variable
- **Link label** — optional, defaults to the field name

Example: regex `trace_id=(\w+)`, field name `traceID`, URL `https://tracing.example/trace/${__value.raw}` turns a log line like `request failed trace_id=abc123` into a clickable `traceID` column.

## Extended JSON

Queries are parsed as [MongoDB extended JSON](https://www.mongodb.com/docs/manual/reference/mongodb-extended-json/), so BSON types are expressible: `{"$date": "2026-01-01T00:00:00Z"}`, `{"$oid": "65f0..."}`, `{"$numberDecimal": "9.99"}`, etc.
