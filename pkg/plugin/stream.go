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
func (d *Datasource) parseStreamQuery(raw json.RawMessage) (*mongo.Collection, bson.D, error) {
	var qm queryModel
	if err := json.Unmarshal(raw, &qm); err != nil {
		return nil, nil, fmt.Errorf("invalid stream query: %w", err)
	}

	dbName := qm.Database
	if dbName == "" {
		dbName = d.settings.Database
	}
	if dbName == "" || qm.Collection == "" {
		return nil, nil, fmt.Errorf("live tailing requires a database and collection")
	}

	filter, err := parseDocument(qm.QueryText)
	if err != nil {
		return nil, nil, fmt.Errorf("filter: %w", err)
	}

	return d.client.Database(dbName).Collection(qm.Collection), filter, nil
}

// SubscribeStream authorizes the subscription and seeds it with the most
// recent matching documents so the logs panel isn't empty until the next
// tailed event arrives.
func (d *Datasource) SubscribeStream(ctx context.Context, req *backend.SubscribeStreamRequest) (*backend.SubscribeStreamResponse, error) {
	coll, filter, err := d.parseStreamQuery(req.Data)
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

	initialData, err := backend.NewInitialFrame(docsToFrame(docs, "logs", "logs"), data.IncludeAll)
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
	coll, filter, err := d.parseStreamQuery(req.Data)
	if err != nil {
		return err
	}

	err = d.watchChangeStream(ctx, coll, filter, sender)
	if err == nil || ctx.Err() != nil {
		return err
	}
	if !isChangeStreamUnsupported(err) {
		return err
	}

	log.DefaultLogger.Info("live tail: change streams unavailable, falling back to polling", "collection", coll.Name(), "error", err)
	return d.pollCollection(ctx, coll, filter, sender)
}

// watchChangeStream tails inserts via a MongoDB change stream, matching the
// caller's filter against each new document's fields.
func (d *Datasource) watchChangeStream(ctx context.Context, coll *mongo.Collection, filter bson.D, sender *backend.StreamSender) error {
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

	for stream.Next(ctx) {
		var event struct {
			FullDocument bson.D `bson:"fullDocument"`
		}
		if err := stream.Decode(&event); err != nil {
			log.DefaultLogger.Error("live tail: failed to decode change event", "error", err)
			continue
		}
		if err := sender.SendFrame(docsToFrame([]bson.D{event.FullDocument}, "logs", "logs"), data.IncludeAll); err != nil {
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
func (d *Datasource) pollCollection(ctx context.Context, coll *mongo.Collection, filter bson.D, sender *backend.StreamSender) error {
	lastID, err := latestID(ctx, coll, filter)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pollFilter := filter
			if lastID != nil {
				pollFilter = append(bson.D{{Key: "_id", Value: bson.D{{Key: "$gt", Value: lastID}}}}, filter...)
			}
			cursor, err := coll.Find(ctx, pollFilter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
			if err != nil {
				log.DefaultLogger.Error("live tail: poll failed", "error", err)
				continue
			}
			var docs []bson.D
			if err := cursor.All(ctx, &docs); err != nil {
				log.DefaultLogger.Error("live tail: poll decode failed", "error", err)
				continue
			}
			if len(docs) == 0 {
				continue
			}
			lastID = idOf(docs[len(docs)-1])
			if err := sender.SendFrame(docsToFrame(docs, "logs", "logs"), data.IncludeAll); err != nil {
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
