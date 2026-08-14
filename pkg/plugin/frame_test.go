package plugin

import (
	"encoding/json"
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

	frame := docsToFrameWithLogOptions(docs, "A", "logs", frameOptions{messageField: "msg", levelField: "severity"})

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

	frame := docsToFrameWithLogOptions(docs, "A", "logs", frameOptions{})
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

	frame := docsToFrameWithLogOptions(docs, "A", "logs", frameOptions{messageField: "msg"})
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
	opts := frameOptions{
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

// TestDocsToFramePreservesIntegerTypes covers issue 21's integer-type
// preservation: whole-number BSON fields (int32 and int64) should come
// through as int64 columns rather than being coerced to float64.
func TestDocsToFramePreservesIntegerTypes(t *testing.T) {
	docs := []bson.D{{
		{Key: "count32", Value: int32(7)},
		{Key: "count64", Value: int64(9000000000)},
	}}

	frame := docsToFrame(docs, "A", "table")

	for name, want := range map[string]int64{"count32": 7, "count64": 9000000000} {
		f := fieldByName(frame, name)
		if f.Type() != data.FieldTypeNullableInt64 {
			t.Fatalf("%s type = %s, want %s", name, f.Type(), data.FieldTypeNullableInt64)
		}
		if v, ok := f.ConcreteAt(0); !ok || v.(int64) != want {
			t.Errorf("%s[0] = %v, want %d", name, v, want)
		}
	}
}

// TestDocsToFrameMixedIntAndFloatPromotesToFloat64 covers the numeric-merge
// path in coerce: a column that sees both integers and floats across rows
// should promote to float64 -- the only one of the two that can represent
// both without loss -- rather than falling back to stringifying everything,
// which is what any other type mismatch does.
func TestDocsToFrameMixedIntAndFloatPromotesToFloat64(t *testing.T) {
	docs := []bson.D{
		{{Key: "v", Value: int32(1)}},
		{{Key: "v", Value: 2.5}},
	}
	frame := docsToFrame(docs, "A", "table")
	f := frame.Fields[0]
	if f.Type() != data.FieldTypeNullableFloat64 {
		t.Fatalf("expected float64 field after int/float merge, got %s", f.Type())
	}
	if v, ok := f.ConcreteAt(0); !ok || v.(float64) != 1 {
		t.Errorf("v[0] = %v, want 1", v)
	}
	if v, ok := f.ConcreteAt(1); !ok || v.(float64) != 2.5 {
		t.Errorf("v[1] = %v, want 2.5", v)
	}
}

// TestFlattenDepthLimitsNesting covers issue 21's flatten-depth option: a
// nested document reached at maxDepth is kept whole as a single raw JSON
// column instead of continuing to explode into dot-notation columns,
// bounding column growth for deeply nested documents. maxDepth: 1 allows one
// level of flattening ("a" -> "a.b"/"a.sibling"), so the cutoff lands one
// level deeper still, at "b".
func TestFlattenDepthLimitsNesting(t *testing.T) {
	docs := []bson.D{{
		{Key: "id", Value: "doc1"},
		{Key: "a", Value: bson.D{
			{Key: "b", Value: bson.D{{Key: "c", Value: "deep"}}},
			{Key: "sibling", Value: 1},
		}},
	}}

	frame := docsToFrameWithLogOptions(docs, "A", "table", frameOptions{maxDepth: 1})

	if got, want := fieldNames(frame), []string{"id", "a.b", "a.sibling"}; !equalStrings(got, want) {
		t.Fatalf("fields = %v, want %v (doc nested past maxDepth kept whole)", got, want)
	}

	abField := fieldByName(frame, "a.b")
	if abField.Type() != data.FieldTypeNullableString {
		t.Fatalf(`field "a.b" type = %s, want string (raw JSON)`, abField.Type())
	}
	v, ok := abField.ConcreteAt(0)
	if !ok {
		t.Fatalf(`field "a.b" has no value`)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(v.(string)), &decoded); err != nil {
		t.Fatalf(`field "a.b" value %q is not valid JSON: %v`, v, err)
	}
	if decoded["c"] != "deep" {
		t.Errorf("decoded a.b.c = %v, want \"deep\"", decoded["c"])
	}
}

// TestFlattenDepthZeroMeansUnlimited guards the zero-value (unset maxDepth)
// case, which must reproduce today's behavior of flattening every level.
func TestFlattenDepthZeroMeansUnlimited(t *testing.T) {
	docs := []bson.D{{
		{Key: "a", Value: bson.D{{Key: "b", Value: bson.D{{Key: "c", Value: "deep"}}}}},
	}}
	frame := docsToFrameWithLogOptions(docs, "A", "table", frameOptions{})
	if got, want := fieldNames(frame), []string{"a.b.c"}; !equalStrings(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
}

// TestDocsToFrameLongFormatSkipsWideConversion covers issue 21's explicit
// "long" format: unlike "timeseries", it returns the frame in its original
// long shape (still time-sorted) instead of attempting LongToWide, so a
// caller who wants long rows doesn't have to rely on LongToWide's
// failure-and-fallback path.
func TestDocsToFrameLongFormatSkipsWideConversion(t *testing.T) {
	base := time.UnixMilli(1700000000000).UTC()
	docs := []bson.D{
		{{Key: "time", Value: bson.NewDateTimeFromTime(base.Add(2 * time.Minute))}, {Key: "host", Value: "b"}, {Key: "value", Value: 2.0}},
		{{Key: "time", Value: bson.NewDateTimeFromTime(base)}, {Key: "host", Value: "a"}, {Key: "value", Value: 1.0}},
	}

	frame := docsToFrame(docs, "A", "long")

	if got, want := fieldNames(frame), []string{"time", "host", "value"}; !equalStrings(got, want) {
		t.Fatalf("fields = %v, want %v (long format keeps original columns, no wide conversion)", got, want)
	}
	if frame.Meta == nil || frame.Meta.Type != data.FrameTypeTimeSeriesLong {
		t.Fatalf("frame.Meta = %#v, want Type %s", frame.Meta, data.FrameTypeTimeSeriesLong)
	}
	timeField := frame.Fields[0]
	first, _ := timeField.ConcreteAt(0)
	second, _ := timeField.ConcreteAt(1)
	if !first.(time.Time).Before(second.(time.Time)) {
		t.Errorf("rows not sorted by time: %v then %v", first, second)
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

func fieldByName(frame *data.Frame, name string) *data.Field {
	for _, f := range frame.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}
