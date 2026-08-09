package plugin

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func testDataQuery() backend.DataQuery {
	return backend.DataQuery{
		TimeRange: backend.TimeRange{
			From: time.UnixMilli(1700000000000),
			To:   time.UnixMilli(1700003600000),
		},
		Interval:      30 * time.Second,
		MaxDataPoints: 500,
	}
}

func TestInterpolateMacros(t *testing.T) {
	q := testDataQuery()

	in := `[{"$match": {"ts": {"$gte": "$__timeFrom", "$lte": "$__timeTo"}, "ms": {"$gt": "$__timeFrom_ms"}}}, {"$limit": "$__maxDataPoints"}]`
	out := interpolateMacros(in, q)

	for _, want := range []string{
		`{"$date":{"$numberLong":"1700000000000"}}`,
		`{"$date":{"$numberLong":"1700003600000"}}`,
		`"$gt": 1700000000000`,
		`{"$limit": 500}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("interpolated query missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "$__time") {
		t.Errorf("macros left unreplaced:\n%s", out)
	}

	// The interpolated pipeline must parse as extended JSON.
	if _, err := parsePipeline(out); err != nil {
		t.Errorf("interpolated pipeline does not parse: %v", err)
	}
}

func TestParsePipeline(t *testing.T) {
	pipeline, err := parsePipeline(`[{"$match": {"a": 1}}, {"$group": {"_id": "$b"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(pipeline))
	}

	// A bare document is a one-stage pipeline.
	pipeline, err = parsePipeline(`{"$match": {"a": 1}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(pipeline))
	}

	if _, err := parsePipeline(""); err == nil {
		t.Error("expected error for empty pipeline")
	}
	if _, err := parsePipeline("not json"); err == nil {
		t.Error("expected error for invalid pipeline")
	}
}

func TestDocsToFrameTable(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("65f000000000000000000001")
	docs := []bson.D{
		{
			{Key: "_id", Value: oid},
			{Key: "name", Value: "alpha"},
			{Key: "value", Value: int32(7)},
			{Key: "nested", Value: bson.D{{Key: "x", Value: 1.5}}},
			{Key: "tags", Value: bson.A{"a", "b"}},
		},
		{
			{Key: "_id", Value: oid},
			{Key: "name", Value: "beta"},
			// value missing here, extra field appears
			{Key: "extra", Value: true},
			{Key: "nested", Value: bson.D{{Key: "x", Value: 2.5}}},
		},
	}

	frame := docsToFrame(docs, "A", "table")
	if frame.Rows() != 2 {
		t.Fatalf("expected 2 rows, got %d", frame.Rows())
	}

	fields := map[string]*data.Field{}
	for _, f := range frame.Fields {
		fields[f.Name] = f
	}
	for _, name := range []string{"_id", "name", "value", "nested.x", "tags", "extra"} {
		if fields[name] == nil {
			t.Fatalf("missing field %q; have %v", name, frame.Fields)
		}
	}

	if v, ok := fields["value"].ConcreteAt(0); !ok || v.(float64) != 7 {
		t.Errorf("value[0] = %v, want 7", v)
	}
	if _, ok := fields["value"].ConcreteAt(1); ok {
		t.Error("value[1] should be null")
	}
	if v, ok := fields["tags"].ConcreteAt(0); !ok || v.(string) != `["a","b"]` {
		t.Errorf(`tags[0] = %v, want ["a","b"]`, v)
	}
	if v, ok := fields["nested.x"].ConcreteAt(1); !ok || v.(float64) != 2.5 {
		t.Errorf("nested.x[1] = %v, want 2.5", v)
	}
}

func TestDocsToFrameMixedTypesPromoteToString(t *testing.T) {
	docs := []bson.D{
		{{Key: "v", Value: int32(1)}},
		{{Key: "v", Value: "two"}},
	}
	frame := docsToFrame(docs, "A", "table")
	f := frame.Fields[0]
	if f.Type() != data.FieldTypeNullableString {
		t.Fatalf("expected string field after promotion, got %s", f.Type())
	}
	if v, _ := f.ConcreteAt(0); v.(string) != "1" {
		t.Errorf("v[0] = %v, want \"1\"", v)
	}
}

func TestDocsToFrameTimeSeries(t *testing.T) {
	base := time.UnixMilli(1700000000000).UTC()
	docs := []bson.D{}
	for i := 0; i < 3; i++ {
		for _, host := range []string{"a", "b"} {
			docs = append(docs, bson.D{
				{Key: "time", Value: bson.NewDateTimeFromTime(base.Add(time.Duration(i) * time.Minute))},
				{Key: "host", Value: host},
				{Key: "value", Value: float64(i * 10)},
			})
		}
	}

	frame := docsToFrame(docs, "A", "timeseries")
	// Wide conversion: one time field + one value field per host label.
	if len(frame.Fields) != 3 {
		t.Fatalf("expected 3 fields (time + 2 series), got %d: %v", len(frame.Fields), frame.Fields)
	}
	if frame.Rows() != 3 {
		t.Fatalf("expected 3 rows, got %d", frame.Rows())
	}
}
