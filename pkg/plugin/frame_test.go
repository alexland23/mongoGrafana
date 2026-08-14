package plugin

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestDocsToFrameColumnOrderIsStable guards against regressing to an
// intermediate map in addDocument/flattenDoc, which randomizes column order
// across runs since Go map iteration order is not deterministic.
func TestDocsToFrameColumnOrderIsStable(t *testing.T) {
	doc := bson.D{
		{Key: "zebra", Value: "z"},
		{Key: "time", Value: bson.DateTime(time.Now().UnixMilli())},
		{Key: "apple", Value: int32(1)},
		{Key: "mango", Value: true},
	}
	docs := []bson.D{doc}

	want := fieldNames(docsToFrame(docs, "A", "table"))
	for i := range 20 {
		got := fieldNames(docsToFrame(docs, "A", "table"))
		if len(got) != len(want) {
			t.Fatalf("run %d: field count changed: got %v, want %v", i, got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("run %d: field order changed: got %v, want %v", i, got, want)
			}
		}
	}

	if want[0] != "time" {
		t.Fatalf("expected time field first, got %v", want)
	}
	rest := want[1:]
	wantRest := []string{"zebra", "apple", "mango"}
	for i := range wantRest {
		if rest[i] != wantRest[i] {
			t.Fatalf("expected non-time fields in document order %v, got %v", wantRest, rest)
		}
	}
}

// TestDocsToStreamFrameKeepsSchemaStableAcrossCalls guards against the live
// tailing bug where each streamed frame derived its schema independently:
// a field present on one change-stream event but absent from the next
// vanished from the frame entirely instead of coming through as null,
// and consecutive frames on the same Grafana Live channel ended up with
// different field sets/order. docsToStreamFrame's reused builder must keep
// every field seen so far, in the order first seen, even when a later
// document omits it.
func TestDocsToStreamFrameKeepsSchemaStableAcrossCalls(t *testing.T) {
	b := newFrameBuilder()

	first := docsToStreamFrame(b, []bson.D{{
		{Key: "level", Value: "error"},
		{Key: "msg", Value: "boom"},
		{Key: "stack", Value: "trace..."},
	}}, "logs", "logs")
	if got, want := fieldNames(first), []string{"level", "msg", "stack"}; !equalStrings(got, want) {
		t.Fatalf("first frame fields = %v, want %v", got, want)
	}

	second := docsToStreamFrame(b, []bson.D{{
		{Key: "level", Value: "info"},
		{Key: "msg", Value: "ok"},
	}}, "logs", "logs")
	if got, want := fieldNames(second), []string{"level", "msg", "stack"}; !equalStrings(got, want) {
		t.Fatalf("second frame fields = %v, want %v (stack must not vanish)", got, want)
	}
	stackField := second.Fields[2]
	if stackField.Name != "stack" {
		t.Fatalf("expected third field to be stack, got %v", stackField.Name)
	}
	if v := stackField.At(0); v != (*string)(nil) {
		t.Errorf("stack value for a document missing that field = %v, want nil", v)
	}

	third := docsToStreamFrame(b, []bson.D{{
		{Key: "level", Value: "warn"},
		{Key: "msg", Value: "careful"},
		{Key: "service", Value: "api"},
	}}, "logs", "logs")
	if got, want := fieldNames(third), []string{"level", "msg", "stack", "service"}; !equalStrings(got, want) {
		t.Fatalf("third frame fields = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fieldNames(frame *data.Frame) []string {
	names := make([]string, len(frame.Fields))
	for i, f := range frame.Fields {
		names[i] = f.Name
	}
	return names
}
