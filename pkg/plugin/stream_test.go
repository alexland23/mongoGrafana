package plugin

import (
	"errors"
	"reflect"
	"testing"

	"github.com/alandave/mongo-db/pkg/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestPrefixFilterKeys(t *testing.T) {
	filter := bson.D{
		{Key: "level", Value: "error"},
		{Key: "$or", Value: bson.A{bson.D{{Key: "service", Value: "api"}}}},
	}
	got := prefixFilterKeys(filter, "fullDocument.")
	want := bson.D{
		{Key: "fullDocument.level", Value: "error"},
		{Key: "$or", Value: bson.A{bson.D{{Key: "fullDocument.service", Value: "api"}}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prefixFilterKeys() = %#v, want %#v", got, want)
	}
}

func TestPrefixFilterKeysNestedLogicalOperators(t *testing.T) {
	filter := bson.D{
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "level", Value: "error"}},
			bson.D{{Key: "level", Value: "warn"}},
		}},
	}
	got := prefixFilterKeys(filter, "fullDocument.")
	want := bson.D{
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "fullDocument.level", Value: "error"}},
			bson.D{{Key: "fullDocument.level", Value: "warn"}},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prefixFilterKeys() = %#v, want %#v", got, want)
	}

	nested := bson.D{
		{Key: "$and", Value: bson.A{
			bson.D{{Key: "$or", Value: bson.A{
				bson.D{{Key: "level", Value: "error"}},
			}}},
			bson.D{{Key: "service", Value: "api"}},
		}},
	}
	got = prefixFilterKeys(nested, "fullDocument.")
	want = bson.D{
		{Key: "$and", Value: bson.A{
			bson.D{{Key: "$or", Value: bson.A{
				bson.D{{Key: "fullDocument.level", Value: "error"}},
			}}},
			bson.D{{Key: "fullDocument.service", Value: "api"}},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prefixFilterKeys() nested = %#v, want %#v", got, want)
	}
}

func TestIdOf(t *testing.T) {
	doc := bson.D{{Key: "_id", Value: 42}, {Key: "level", Value: "info"}}
	if got := idOf(doc); got != 42 {
		t.Errorf("idOf() = %v, want 42", got)
	}
	if got := idOf(bson.D{{Key: "level", Value: "info"}}); got != nil {
		t.Errorf("idOf() with no _id = %v, want nil", got)
	}
}

func TestReverseDocs(t *testing.T) {
	docs := []bson.D{
		{{Key: "_id", Value: 1}},
		{{Key: "_id", Value: 2}},
		{{Key: "_id", Value: 3}},
	}
	reverseDocs(docs)
	got := []int{docs[0][0].Value.(int), docs[1][0].Value.(int), docs[2][0].Value.(int)}
	want := []int{3, 2, 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reverseDocs() order = %v, want %v", got, want)
	}
}

func TestIsChangeStreamUnsupported(t *testing.T) {
	standaloneErr := mongo.CommandError{Code: changeStreamUnsupportedCode, Message: "The $changeStream stage is only supported on replica sets"}
	if !isChangeStreamUnsupported(standaloneErr) {
		t.Errorf("expected standalone CommandError to be recognized as unsupported")
	}

	wrapped := errors.New("wrapped: " + standaloneErr.Error())
	if !isChangeStreamUnsupported(wrapped) {
		t.Errorf("expected error mentioning $changeStream to be recognized as unsupported")
	}

	other := mongo.CommandError{Code: 13, Message: "not authorized"}
	if isChangeStreamUnsupported(other) {
		t.Errorf("did not expect an unrelated CommandError to be recognized as unsupported")
	}
}

func TestParseStreamQueryRequiresDatabaseAndCollection(t *testing.T) {
	d := &Datasource{settings: &models.PluginSettings{}}
	if _, _, err := d.parseStreamQuery([]byte(`{"collection":"logs"}`)); err == nil {
		t.Errorf("expected error when no database is configured")
	}
}

func TestFilterAfterID(t *testing.T) {
	filter := bson.D{{Key: "level", Value: "error"}}

	got := filterAfterID(filter, 42)
	want := bson.D{
		{Key: "_id", Value: bson.D{{Key: "$gt", Value: 42}}},
		{Key: "level", Value: "error"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterAfterID() = %#v, want %#v", got, want)
	}

	if got := filterAfterID(filter, nil); !reflect.DeepEqual(got, filter) {
		t.Errorf("filterAfterID() with nil id = %#v, want filter unchanged %#v", got, filter)
	}
}

func TestFilterAfterIDWithExistingIDKey(t *testing.T) {
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$gt", Value: "bookmark123"}}}}

	got := filterAfterID(filter, 42)
	want := bson.D{
		{Key: "$and", Value: bson.A{
			bson.D{{Key: "_id", Value: bson.D{{Key: "$gt", Value: 42}}}},
			filter,
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterAfterID() = %#v, want %#v", got, want)
	}

	idCount := 0
	for _, e := range got {
		if e.Key == "_id" {
			idCount++
		}
	}
	if idCount > 1 {
		t.Errorf("filterAfterID() produced %d top-level _id keys, want at most 1", idCount)
	}
}

// TestStreamBaselineHandoff verifies the SubscribeStream->RunStream
// coordination this package relies on to close the race between the
// backlog snapshot and the tail's start point: a baseline recorded for a
// channel path is delivered to the next reader exactly once.
func TestStreamBaselineHandoff(t *testing.T) {
	d := &Datasource{}
	d.streamBaselines.Store("a/b/c", 42)

	got, ok := d.streamBaselines.LoadAndDelete("a/b/c")
	if !ok || got != 42 {
		t.Errorf("LoadAndDelete() = (%v, %v), want (42, true)", got, ok)
	}

	if _, ok := d.streamBaselines.LoadAndDelete("a/b/c"); ok {
		t.Errorf("expected baseline to be consumed after the first read")
	}

	if _, ok := d.streamBaselines.LoadAndDelete("never-stored"); ok {
		t.Errorf("expected no baseline for a path that was never recorded")
	}
}
