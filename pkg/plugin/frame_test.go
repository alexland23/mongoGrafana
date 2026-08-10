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

func fieldNames(frame *data.Frame) []string {
	names := make([]string, len(frame.Fields))
	for i, f := range frame.Fields {
		names[i] = f.Name
	}
	return names
}
