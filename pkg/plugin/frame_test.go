package plugin

import (
	"regexp"
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

// TestLogFieldMappingRenamesConfiguredColumns covers issue 18's "configurable
// log field mapping": a collection that doesn't name its fields "message"/
// "level" should still render in the logs visualization once the user maps
// the actual field names.
func TestLogFieldMappingRenamesConfiguredColumns(t *testing.T) {
	docs := []bson.D{{
		{Key: "time", Value: bson.DateTime(time.Now().UnixMilli())},
		{Key: "msg", Value: "boom"},
		{Key: "severity", Value: "error"},
	}}

	frame := docsToFrameWithLogOptions(docs, "A", "logs", logFieldOptions{messageField: "msg", levelField: "severity"})

	got := fieldNames(frame)
	for _, want := range []string{"message", "level"} {
		found := false
		for _, name := range got {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("fields = %v, want a %q column", got, want)
		}
	}
	for _, unwanted := range []string{"msg", "severity"} {
		for _, name := range got {
			if name == unwanted {
				t.Fatalf("fields = %v, original column %q should have been renamed away", got, unwanted)
			}
		}
	}
}

// TestLogFieldMappingLeavesUnmappedColumnsAlone guards the zero-value
// (blank messageField/levelField) case, which must reproduce today's
// behavior of relying on fields already being named "message"/"level".
func TestLogFieldMappingLeavesUnmappedColumnsAlone(t *testing.T) {
	docs := []bson.D{{
		{Key: "time", Value: bson.DateTime(time.Now().UnixMilli())},
		{Key: "message", Value: "hello"},
		{Key: "level", Value: "info"},
	}}

	frame := docsToFrameWithLogOptions(docs, "A", "logs", logFieldOptions{})
	if got, want := fieldNames(frame), []string{"time", "message", "level"}; !equalStrings(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
}

// TestLogFieldMappingDoesNotClobberExistingCanonicalColumn covers the edge
// case where the configured source field and an already-canonically-named
// field both exist: the pre-existing "message" column must win rather than
// being silently overwritten.
func TestLogFieldMappingDoesNotClobberExistingCanonicalColumn(t *testing.T) {
	docs := []bson.D{{
		{Key: "message", Value: "keep me"},
		{Key: "msg", Value: "should not overwrite"},
	}}

	frame := docsToFrameWithLogOptions(docs, "A", "logs", logFieldOptions{messageField: "msg"})
	if got, want := fieldNames(frame), []string{"message", "msg"}; !equalStrings(got, want) {
		t.Fatalf("fields = %v, want %v (both columns kept, unrenamed)", got, want)
	}
}

// TestApplyDerivedFieldsExtractsAndLinksTraceID covers issue 18's derived
// fields: a trace ID pulled out of the message text via regex should become
// its own column with a clickable data link, and rows that don't match get
// a nil value rather than an empty string.
func TestApplyDerivedFieldsExtractsAndLinksTraceID(t *testing.T) {
	docs := []bson.D{
		{{Key: "message", Value: "request failed trace_id=abc123 status=500"}},
		{{Key: "message", Value: "no trace id in this line"}},
	}
	traceRe := mustCompile(`trace_id=(\w+)`)
	opts := logFieldOptions{
		derivedFields: []derivedField{
			{re: traceRe, name: "traceID", url: "https://tracing.example/trace/${__value.raw}", urlDisplayLabel: "Trace view"},
		},
	}

	frame := docsToFrameWithLogOptions(docs, "A", "logs", opts)

	var traceField *data.Field
	for _, f := range frame.Fields {
		if f.Name == "traceID" {
			traceField = f
		}
	}
	if traceField == nil {
		t.Fatalf("fields = %v, want a traceID column", fieldNames(frame))
	}
	if v := traceField.At(0); v == nil || *(v.(*string)) != "abc123" {
		t.Errorf("traceID row 0 = %v, want \"abc123\"", v)
	}
	if v := traceField.At(1); v != (*string)(nil) {
		t.Errorf("traceID row 1 (no match) = %v, want nil", v)
	}
	if traceField.Config == nil || len(traceField.Config.Links) != 1 {
		t.Fatalf("traceID field config = %#v, want one data link", traceField.Config)
	}
	link := traceField.Config.Links[0]
	if link.Title != "Trace view" || link.URL != "https://tracing.example/trace/${__value.raw}" {
		t.Errorf("traceID link = %#v, unexpected title/url", link)
	}
}

func mustCompile(pattern string) *regexp.Regexp {
	re, err := regexp.Compile(pattern)
	if err != nil {
		panic(err)
	}
	return re
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
