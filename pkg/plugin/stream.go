package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var _ backend.StreamHandler = (*Datasource)(nil)

const (
	// changeStreamUnsupportedCode is the server error MongoDB returns when a
	// change stream is opened against a standalone (non replica-set) server.
	changeStreamUnsupportedCode = 40573
	// liveBacklogSize bounds how many existing documents seed a new
	// subscription so the panel isn't empty until the next tailed event.
	liveBacklogSize = 100
	// pollInterval is how often RunStream re-queries when change streams are
	// unavailable, e.g. a standalone mongod.
	pollInterval = 2 * time.Second
)

// parseStreamQuery decodes the query the frontend attached to the live
// channel address, reusing queryModel and the same extended-JSON filter
// parsing as regular "find" queries. Live tailing has no bounded time
// range, so time macros ($__timeFrom etc.) are not interpolated and will
// fail to parse if present in the filter text.
func (d *Datasource) parseStreamQuery(raw json.RawMessage) (*mongo.Collection, bson.D, queryModel, error) {
	var qm queryModel
	if err := json.Unmarshal(raw, &qm); err != nil {
		return nil, nil, qm, fmt.Errorf("invalid stream query: %w", err)
	}

	dbName := qm.Database
	if dbName == "" {
		dbName = d.settings.Database
	}
	if dbName == "" || qm.Collection == "" {
		return nil, nil, qm, fmt.Errorf("live tailing requires a database and collection")
	}

	filter, err := parseDocument(qm.QueryText)
	if err != nil {
		return nil, nil, qm, fmt.Errorf("filter: %w", err)
	}

	return d.client.Database(dbName).Collection(qm.Collection), filter, qm, nil
}

// SubscribeStream authorizes the subscription and seeds it with the most
// recent matching documents so the logs panel isn't empty until the next
// tailed event arrives.
func (d *Datasource) SubscribeStream(ctx context.Context, req *backend.SubscribeStreamRequest) (*backend.SubscribeStreamResponse, error) {
	coll, filter, qm, err := d.parseStreamQuery(req.Data)
	if err != nil {
		log.DefaultLogger.Warn("live tail: rejecting subscription", "error", err)
		return &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusNotFound}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(liveBacklogSize)
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		log.DefaultLogger.Warn("live tail: initial backlog query failed", "error", err)
		return &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusOK}, nil
	}
	var docs []bson.D
	if err := cursor.All(ctx, &docs); err != nil {
		log.DefaultLogger.Warn("live tail: initial backlog decode failed", "error", err)
		return &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusOK}, nil
	}
	reverseDocs(docs)

	// Record the newest id this backlog covers so RunStream's tail (started
	// separately, and possibly some time later) can resume exactly from here
	// instead of independently querying its own "latest" baseline — closing
	// the gap where a document inserted between the two queries would
	// otherwise be newer than the backlog but older than the tail's start
	// point, and so never delivered at all.
	var maxID any
	if len(docs) > 0 {
		maxID = idOf(docs[len(docs)-1])
	}
	d.streamBaselines.Store(req.Path, maxID)

	// Build the initial frame on a builder RunStream will reuse for every
	// subsequent frame it sends on this channel, so the schema established
	// here (field set/order/types) stays stable for the life of the stream.
	builder := newFrameBuilder()
	initialFrame := docsToStreamFrameWithLogOptions(builder, docs, "logs", "logs", d.logFieldOptionsFor(qm))
	d.streamSchemas.Store(req.Path, builder)

	initialData, err := backend.NewInitialFrame(initialFrame, data.IncludeAll)
	if err != nil {
		return nil, err
	}
	return &backend.SubscribeStreamResponse{Status: backend.SubscribeStreamStatusOK, InitialData: initialData}, nil
}

// PublishStream is unused; live tailing is read-only.
func (d *Datasource) PublishStream(context.Context, *backend.PublishStreamRequest) (*backend.PublishStreamResponse, error) {
	return &backend.PublishStreamResponse{Status: backend.PublishStreamStatusPermissionDenied}, nil
}

// RunStream tails a collection for as long as at least one client is
// subscribed. It prefers MongoDB change streams (replica sets/Atlas); where
// those are unavailable it falls back to polling by _id.
func (d *Datasource) RunStream(ctx context.Context, req *backend.RunStreamRequest, sender *backend.StreamSender) error {
	coll, filter, qm, err := d.parseStreamQuery(req.Data)
	if err != nil {
		return err
	}
	logOpts := d.logFieldOptionsFor(qm)

	// baseline, if present, is the newest _id the triggering SubscribeStream
	// call's backlog already covered; nil-but-present means the backlog was
	// empty. Its absence (no entry recorded) means we have no reference
	// point, e.g. the backlog query itself failed.
	var baseline *any
	if v, ok := d.streamBaselines.LoadAndDelete(req.Path); ok {
		baseline = &v
	}

	// Reuse the builder SubscribeStream seeded from the backlog, if any, so
	// the schema it established carries through every frame this stream
	// sends. Absence (e.g. the backlog query itself failed) just means
	// starting from an empty schema.
	builder := newFrameBuilder()
	if v, ok := d.streamSchemas.LoadAndDelete(req.Path); ok {
		if b, ok := v.(*frameBuilder); ok {
			builder = b
		}
	}

	err = d.watchChangeStream(ctx, coll, filter, baseline, builder, sender, logOpts)
	if err == nil || ctx.Err() != nil {
		return err
	}
	if !isChangeStreamUnsupported(err) {
		return err
	}

	log.DefaultLogger.Info("live tail: change streams unavailable, falling back to polling", "collection", coll.Name(), "error", err)
	return d.pollCollection(ctx, coll, filter, baseline, builder, sender, logOpts)
}

// watchChangeStream tails inserts via a MongoDB change stream, matching the
// caller's filter against each new document's fields.
func (d *Datasource) watchChangeStream(ctx context.Context, coll *mongo.Collection, filter bson.D, baseline *any, builder *frameBuilder, sender *backend.StreamSender, logOpts logFieldOptions) error {
	match := append(bson.D{{Key: "operationType", Value: "insert"}}, prefixFilterKeys(filter, "fullDocument.")...)
	stream, err := coll.Watch(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}})
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = stream.Close(closeCtx)
	}()

	// The change stream only observes inserts from this point forward, but
	// it was opened strictly after the backlog snapshot (a separate query,
	// possibly seconds earlier). Catch up on anything inserted in between,
	// and remember those ids so the change stream doesn't redeliver them.
	seen := map[string]struct{}{}
	if baseline != nil {
		caughtUp, err := findAfterID(ctx, coll, filter, *baseline)
		if err != nil {
			log.DefaultLogger.Warn("live tail: catch-up poll failed", "error", err)
		} else if len(caughtUp) > 0 {
			for _, doc := range caughtUp {
				if id := idOf(doc); id != nil {
					seen[fmt.Sprint(id)] = struct{}{}
				}
			}
			if err := sender.SendFrame(docsToStreamFrameWithLogOptions(builder, caughtUp, "logs", "logs", logOpts), data.IncludeAll); err != nil {
				return err
			}
		}
	}

	for stream.Next(ctx) {
		var event struct {
			FullDocument bson.D `bson:"fullDocument"`
		}
		if err := stream.Decode(&event); err != nil {
			log.DefaultLogger.Error("live tail: failed to decode change event", "error", err)
			continue
		}
		if id := idOf(event.FullDocument); id != nil {
			key := fmt.Sprint(id)
			if _, dup := seen[key]; dup {
				delete(seen, key)
				continue
			}
		}
		if err := sender.SendFrame(docsToStreamFrameWithLogOptions(builder, []bson.D{event.FullDocument}, "logs", "logs", logOpts), data.IncludeAll); err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

// pollCollection re-queries documents with _id greater than the last one
// seen, sorted ascending, on a fixed interval.
func (d *Datasource) pollCollection(ctx context.Context, coll *mongo.Collection, filter bson.D, baseline *any, builder *frameBuilder, sender *backend.StreamSender, logOpts logFieldOptions) error {
	var lastID any
	if baseline != nil {
		// Resume exactly where the triggering SubscribeStream's backlog left
		// off, rather than an independently-queried "latest" id that could
		// already be newer than that backlog and drop documents in between.
		lastID = *baseline
	} else {
		var err error
		lastID, err = latestID(ctx, coll, filter)
		if err != nil {
			return err
		}
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			docs, err := findAfterID(ctx, coll, filter, lastID)
			if err != nil {
				log.DefaultLogger.Error("live tail: poll failed", "error", err)
				continue
			}
			if len(docs) == 0 {
				continue
			}
			lastID = idOf(docs[len(docs)-1])
			if err := sender.SendFrame(docsToStreamFrameWithLogOptions(builder, docs, "logs", "logs", logOpts), data.IncludeAll); err != nil {
				return err
			}
		}
	}
}

// isChangeStreamUnsupported reports whether err is MongoDB's "not a replica
// set" response, in which case RunStream should fall back to polling.
func isChangeStreamUnsupported(err error) bool {
	if cmdErr, ok := errors.AsType[mongo.CommandError](err); ok {
		return cmdErr.HasErrorCode(changeStreamUnsupportedCode)
	}
	return strings.Contains(err.Error(), "$changeStream")
}

// prefixFilterKeys rewrites a filter's field keys (recursing into logical
// operators like $or/$and/$nor) so it can match a change stream event's
// fullDocument instead of the document itself.
func prefixFilterKeys(filter bson.D, prefix string) bson.D {
	prefixed := make(bson.D, 0, len(filter))
	for _, e := range filter {
		key := e.Key
		switch key {
		case "$or", "$and", "$nor":
			if arr, ok := e.Value.(bson.A); ok {
				prefixed = append(prefixed, bson.E{Key: key, Value: prefixFilterArray(arr, prefix)})
				continue
			}
		}
		if !strings.HasPrefix(key, "$") {
			key = prefix + key
		}
		prefixed = append(prefixed, bson.E{Key: key, Value: e.Value})
	}
	return prefixed
}

// prefixFilterArray applies prefixFilterKeys to each sub-document of a
// $or/$and/$nor operator's array value.
func prefixFilterArray(arr bson.A, prefix string) bson.A {
	prefixedArr := make(bson.A, 0, len(arr))
	for _, item := range arr {
		if sub, ok := item.(bson.D); ok {
			prefixedArr = append(prefixedArr, prefixFilterKeys(sub, prefix))
		} else {
			prefixedArr = append(prefixedArr, item)
		}
	}
	return prefixedArr
}

// filterAfterID returns filter augmented with an _id lower bound so only
// documents newer than id match. If id is nil, filter is returned unchanged
// since nothing has been delivered yet, so nothing should be excluded. If
// filter already has a top-level _id key (e.g. a user filter that itself
// bookmarks on _id), the two constraints are combined with $and instead of
// prepending a second top-level _id key, which would produce a bson.D with
// duplicate keys and undefined driver/server behavior.
func filterAfterID(filter bson.D, id any) bson.D {
	if id == nil {
		return filter
	}
	idConstraint := bson.D{{Key: "_id", Value: bson.D{{Key: "$gt", Value: id}}}}
	for _, e := range filter {
		if e.Key == "_id" {
			return bson.D{{Key: "$and", Value: bson.A{idConstraint, filter}}}
		}
	}
	return append(idConstraint, filter...)
}

// findAfterID returns documents matching filter newer than id (all matching
// documents if id is nil), sorted ascending by _id.
func findAfterID(ctx context.Context, coll *mongo.Collection, filter bson.D, id any) ([]bson.D, error) {
	cursor, err := coll.Find(ctx, filterAfterID(filter, id), options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []bson.D
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// idOf returns a document's _id value, or nil if absent.
func idOf(doc bson.D) any {
	for _, e := range doc {
		if e.Key == "_id" {
			return e.Value
		}
	}
	return nil
}

// latestID returns the _id of the most recent document matching filter, or
// nil if the collection has no match yet.
func latestID(ctx context.Context, coll *mongo.Collection, filter bson.D) (any, error) {
	var doc bson.D
	err := coll.FindOne(ctx, filter, options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return idOf(doc), nil
}

// reverseDocs reverses docs in place, e.g. turning a newest-first backlog
// query into the chronological order a logs panel expects.
func reverseDocs(docs []bson.D) {
	for i, j := 0, len(docs)-1; i < j; i, j = i+1, j-1 {
		docs[i], docs[j] = docs[j], docs[i]
	}
}
