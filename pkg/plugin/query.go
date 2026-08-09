package plugin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// queryModel mirrors the JSON produced by the frontend query editor.
type queryModel struct {
	QueryType  string `json:"queryType"`  // aggregate | find | count | distinct | command
	Database   string `json:"database"`   // optional override of the datasource default
	Collection string `json:"collection"` // required except for "command"
	QueryText  string `json:"queryText"`  // extended JSON: pipeline, filter or command document
	Field      string `json:"field"`      // distinct only
	Projection string `json:"projection"` // find only, extended JSON document
	Sort       string `json:"sort"`       // find only, extended JSON document
	Limit      int64  `json:"limit"`      // find only, 0 = no limit
	Skip       int64  `json:"skip"`       // find only
	Format     string `json:"format"`     // table | timeseries | logs
}

// interpolateMacros replaces Grafana time macros inside the raw query text
// with extended-JSON values so the result parses as valid extended JSON.
// Longer tokens are listed first so e.g. $__timeFrom_ms wins over $__timeFrom.
func interpolateMacros(text string, q backend.DataQuery) string {
	fromMs := strconv.FormatInt(q.TimeRange.From.UnixMilli(), 10)
	toMs := strconv.FormatInt(q.TimeRange.To.UnixMilli(), 10)
	fromDate := fmt.Sprintf(`{"$date":{"$numberLong":"%s"}}`, fromMs)
	toDate := fmt.Sprintf(`{"$date":{"$numberLong":"%s"}}`, toMs)
	intervalMs := strconv.FormatInt(q.Interval.Milliseconds(), 10)
	maxPoints := strconv.FormatInt(q.MaxDataPoints, 10)

	r := strings.NewReplacer(
		`"$__timeFrom_ms"`, fromMs,
		`"$__timeTo_ms"`, toMs,
		`"$__timeFrom"`, fromDate,
		`"$__timeTo"`, toDate,
		`"$__interval_ms"`, intervalMs,
		`"$__maxDataPoints"`, maxPoints,
		`$__timeFrom_ms`, fromMs,
		`$__timeTo_ms`, toMs,
		`$__timeFrom`, fromDate,
		`$__timeTo`, toDate,
		`$__interval_ms`, intervalMs,
		`$__maxDataPoints`, maxPoints,
	)
	return r.Replace(text)
}

// parseDocument parses an extended JSON document (e.g. a filter) into bson.D.
// An empty string yields an empty document.
func parseDocument(text string) (bson.D, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return bson.D{}, nil
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(text), false, &doc); err != nil {
		return nil, fmt.Errorf("invalid extended JSON document: %w", err)
	}
	return doc, nil
}

// parsePipeline parses an extended JSON array of pipeline stages. A single
// document is accepted too and treated as a one-stage pipeline.
func parsePipeline(text string) ([]bson.D, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("aggregation pipeline is empty")
	}
	if strings.HasPrefix(text, "{") {
		doc, err := parseDocument(text)
		if err != nil {
			return nil, err
		}
		return []bson.D{doc}, nil
	}
	// bson.UnmarshalExtJSON only accepts documents at the top level, so wrap
	// the array in one and unwrap after parsing.
	var wrapper struct {
		P []bson.D `bson:"p"`
	}
	wrapped := fmt.Sprintf(`{"p":%s}`, text)
	if err := bson.UnmarshalExtJSON([]byte(wrapped), false, &wrapper); err != nil {
		return nil, fmt.Errorf("invalid aggregation pipeline: %w", err)
	}
	return wrapper.P, nil
}
