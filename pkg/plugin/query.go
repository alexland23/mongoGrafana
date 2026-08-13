package plugin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// timeFilterRe matches $__timeFilter(field) calls, with the field name
// optionally wrapped in quotes, e.g. $__timeFilter(time) or $__timeFilter("time").
var timeFilterRe = regexp.MustCompile(`\$__timeFilter\(\s*"?([A-Za-z0-9_.]+)"?\s*\)`)

// formatInterval renders a duration the way Grafana's built-in $__interval
// variable does elsewhere: the largest whole unit that fits, e.g. "30s", "5m", "2h".
func formatInterval(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int64(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int64(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int64(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int64(d.Hours()/24))
	}
}

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
// $__timeFilter(field) is handled separately via regexp since it takes an
// argument. The remaining tokens are replaced via strings.Replacer, which
// matches earlier entries first at a given position, so e.g. $__interval_ms
// must be listed before $__interval to keep the "_ms" suffix from being
// left behind.
func interpolateMacros(text string, q backend.DataQuery) string {
	fromMs := strconv.FormatInt(q.TimeRange.From.UnixMilli(), 10)
	toMs := strconv.FormatInt(q.TimeRange.To.UnixMilli(), 10)
	fromDate := fmt.Sprintf(`{"$date":{"$numberLong":"%s"}}`, fromMs)
	toDate := fmt.Sprintf(`{"$date":{"$numberLong":"%s"}}`, toMs)
	fromEpochSec := strconv.FormatInt(q.TimeRange.From.Unix(), 10)
	toEpochSec := strconv.FormatInt(q.TimeRange.To.Unix(), 10)
	intervalMs := strconv.FormatInt(q.Interval.Milliseconds(), 10)
	interval := fmt.Sprintf(`%q`, formatInterval(q.Interval))
	maxPoints := strconv.FormatInt(q.MaxDataPoints, 10)

	text = timeFilterRe.ReplaceAllStringFunc(text, func(match string) string {
		field := timeFilterRe.FindStringSubmatch(match)[1]
		return fmt.Sprintf(`{%q: {"$gte": %s, "$lte": %s}}`, field, fromDate, toDate)
	})

	r := strings.NewReplacer(
		`"$__timeFrom_ms"`, fromMs,
		`"$__timeTo_ms"`, toMs,
		`"$__timeFrom"`, fromDate,
		`"$__timeTo"`, toDate,
		`"$__unixEpochFrom"`, fromEpochSec,
		`"$__unixEpochTo"`, toEpochSec,
		`"$__interval_ms"`, intervalMs,
		`"$__interval"`, interval,
		`"$__maxDataPoints"`, maxPoints,
		`"$__from"`, fromMs,
		`"$__to"`, toMs,
		`$__timeFrom_ms`, fromMs,
		`$__timeTo_ms`, toMs,
		`$__timeFrom`, fromDate,
		`$__timeTo`, toDate,
		`$__unixEpochFrom`, fromEpochSec,
		`$__unixEpochTo`, toEpochSec,
		`$__interval_ms`, intervalMs,
		`$__interval`, interval,
		`$__maxDataPoints`, maxPoints,
		`$__from`, fromMs,
		`$__to`, toMs,
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
