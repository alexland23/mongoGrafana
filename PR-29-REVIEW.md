# PR #29 Review — Live Log Tailing

Findings from `/code-review medium` on `16-live-streaming-log-tailing` (change-stream based live tailing). Ranked most severe first. Check items off as we fix them.

## Correctness bugs

- [x] **1. `prefixFilterKeys` doesn't rewrite nested keys** — `pkg/plugin/stream.go:204`
  Only rewrites top-level filter keys, so logical-operator filters (`$or`/`$and`/`$nor`) never match change-stream events.
  *Failure:* Live tail with filter `{"$or":[{"level":"error"},{"level":"warn"}]}` against a replica set produces `$match {operationType:"insert", $or:[{level:"error"},...]}`. Change-stream events only have top-level `operationType`/`ns`/`fullDocument` fields, not `level`, so the `$or` clause can never match — the tail silently returns zero events forever even though matching documents are being inserted.

- [x] **2. Live toggle allowed for unparseable aggregate queries** — `src/components/QueryEditor.tsx:177`
  The Live toggle is shown for every query type except `command`, but the backend only knows how to parse a filter document, not an aggregation pipeline.
  *Failure:* Default query type is `aggregate` with `queryText` being a pipeline array (e.g. `[{"$match":{}},{"$limit":100}]`). If a user sets Format=Logs and flips Live on without changing query type, `parseStreamQuery` calls `parseDocument` on the array text, which fails to unmarshal into `bson.D`, so `SubscribeStream` always returns `SubscribeStreamStatusNotFound` with no explanation — Live is silently broken for the most common/default query type.

- [x] **3. Race between backlog snapshot and tail baseline drops docs** — `pkg/plugin/stream.go:63`
  The initial backlog snapshot (`SubscribeStream`) and the tail baseline (`RunStream`'s `Watch()`/`latestID`) are independent, uncoordinated queries.
  *Failure:* A document inserted between the backlog `Find` (sorted `_id` desc, limit 100) and `RunStream`'s subsequent `coll.Watch()` open (or `pollCollection`'s `latestID()` baseline) is newer than the backlog snapshot, so it's absent from `InitialData` — but it's also already covered by the tail's starting point, so it's excluded as "already seen." That document never reaches the panel.

- [x] **4. `pollFilter` can get duplicate `_id` keys** — `pkg/plugin/stream.go:167`
  Built by unconditionally prepending an `_id` constraint onto the user's filter without checking whether the filter already contains an `_id` key.
  *Failure:* A live-tail find query whose filter itself references `_id` (e.g. a resume bookmark `{"_id":{"$gt":...}}`), combined with the polling fallback (standalone mongod), produces a `bson.D` with two top-level `_id` entries. Driver handling of duplicate keys in a filter document is undefined/order-dependent, silently corrupting the poll filter instead of erroring.

- [x] **5. Live subscription failures surface no error to the user** — `src/datasource.ts:86`
  Live targets bypass `DataSourceWithBackend`'s `query()` entirely, losing its error surfacing/cancellation handling.
  *Failure:* When `SubscribeStream` rejects (e.g. findings #1/#2) or `RunStream` errors (auth failure, disconnected client), `getGrafanaLiveSrv().getDataStream()` has no equivalent to the base class's populated `DataQueryResponse.error` path — the logs panel just shows stale/no data with no visible error banner, unlike a normal failed query.

- [x] **6. Streamed frame schema can vary between consecutive events** — `pkg/plugin/frame.go:274`
  `docsToFrame` derives each streamed frame's schema independently per call, so consecutive live frames on the same Grafana Live channel can have different field sets/order.
  *Failure:* Heterogeneous log documents (some with an `error`/`stack` field, some without) each produce a single-document frame via `watchChangeStream`'s `docsToFrame([]bson.D{event.FullDocument}, ...)` call. Grafana Live's streaming consumer generally expects a stable schema per channel; a field appearing/disappearing between frames can cause the logs panel to drop data or reset instead of appending rows.

- [ ] **7. Live channel hash collisions can cross-wire subscriptions** — `src/datasource.ts:26`
  `hashChannelSegment` is a 32-bit non-cryptographic hash with no collision handling, used as the sole differentiator for arbitrary-length filter text in the live channel path.
  *Failure:* Two different panels/queries on the same collection/database/refId with distinct `queryText` values that hash-collide (feasible at ~64k-input birthday bound) resolve to the same Grafana Live channel path. Per the `RunStream` contract, only the first subscriber's `req.Data` establishes the running stream for a channel, so the second query's Live subscription silently receives the first query's filtered results instead of its own.

## Process

- [ ] **8. `plugin.json` change needs a Grafana restart reminder** — `src/plugin.json:11`
  Adding `"streaming": true` requires a Grafana server restart per `.config/AGENTS/instructions.md` ("Any modifications to plugin.json require a restart of the Grafana server. Remind the user of this."). Not called out anywhere in this change, so the feature will silently appear inert in a running dev Grafana instance until it's restarted.
