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

## Query types

- **Aggregate** (recommended) — a full aggregation pipeline as an extended JSON array. This is the most powerful option: `$match`, `$group`, `$project`, `$lookup`, `$bucket`, window functions — anything the server supports.
- **Find** — a filter document plus optional projection, sort, limit and skip.
- **Count** — count of documents matching a filter.
- **Distinct** — distinct values of a field, optionally filtered.
- **Command** — a raw database command document (`runCommand`), as an escape hatch.

## Time macros

Macros are replaced server-side before the query runs, so they also work in alerting:

| Macro               | Replaced with                                  |
| ------------------- | ---------------------------------------------- |
| `"$__timeFrom"`     | Dashboard range start as a BSON date           |
| `"$__timeTo"`       | Dashboard range end as a BSON date             |
| `"$__timeFrom_ms"`  | Range start as epoch milliseconds (number)     |
| `"$__timeTo_ms"`    | Range end as epoch milliseconds (number)       |
| `"$__interval_ms"`  | Suggested group-by interval in milliseconds    |
| `"$__maxDataPoints"`| Panel max data points (handy for `$limit`)     |

Example — average CPU per host bucketed by 10 minutes, limited to the dashboard time range:

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

With format **Time series**, rows containing a date field, numeric fields and optional string label fields are pivoted into one series per label value (`avg_cpu {host=web-01}`, …).

## Formats

- **Table** — rows as returned; nested documents are flattened with dot notation, arrays are rendered as JSON.
- **Time series** — long-to-wide conversion using the first date column as time and string columns as labels.
- **Logs** — renders in the logs visualization; include a date field and a `message`-like field.

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

## Extended JSON

Queries are parsed as [MongoDB extended JSON](https://www.mongodb.com/docs/manual/reference/mongodb-extended-json/), so BSON types are expressible: `{"$date": "2026-01-01T00:00:00Z"}`, `{"$oid": "65f0..."}`, `{"$numberDecimal": "9.99"}`, etc.
