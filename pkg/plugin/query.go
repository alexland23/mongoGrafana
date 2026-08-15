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

// unixEpochFilterRe matches $__unixEpochFilter(field) calls, the epoch-seconds
// equivalent of $__timeFilter for collections that store timestamps as numbers
// rather than BSON dates.
var unixEpochFilterRe = regexp.MustCompile(`\$__unixEpochFilter\(\s*"?([A-Za-z0-9_.]+)"?\s*\)`)

// intervalArgPattern matches an optional interval argument shared by
// $__timeGroup and $__unixEpochGroup, e.g. "1m", "30s", "1h", "1d".
const intervalArgPattern = `[0-9]+(?:ms|s|m|h|d|w)`

// timeGroupRe matches $__timeGroup(field[, interval]) calls.
var timeGroupRe = regexp.MustCompile(`\$__timeGroup\(\s*"?([A-Za-z0-9_.]+)"?\s*(?:,\s*"?(` + intervalArgPattern + `)"?\s*)?\)`)

// unixEpochGroupRe matches $__unixEpochGroup(field[, interval]) calls.
var unixEpochGroupRe = regexp.MustCompile(`\$__unixEpochGroup\(\s*"?([A-Za-z0-9_.]+)"?\s*(?:,\s*"?(` + intervalArgPattern + `)"?\s*)?\)`)

// intervalArgRe extracts the numeric amount and unit from an interval argument.
var intervalArgRe = regexp.MustCompile(`^([0-9]+)(ms|s|m|h|d|w)$`)

// parseIntervalArg parses a Grafana-style interval string ("30s", "5m", "2h",
// "1d", "1w") into a time.Duration. time.ParseDuration doesn't support "d"
// or "w", so those units are handled by hand.
func parseIntervalArg(s string) (time.Duration, error) {
	m := intervalArgRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid interval %q", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, err
	}
	switch m[2] {
	case "ms":
		return time.Duration(n) * time.Millisecond, nil
	case "s":
		return time.Duration(n) * time.Second, nil
	case "m":
		return time.Duration(n) * time.Minute, nil
	case "h":
		return time.Duration(n) * time.Hour, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid interval unit in %q", s)
	}
}

// resolveGroupInterval determines the grouping duration shared by
// $__timeGroup and $__unixEpochGroup: an explicit interval argument
// overrides the dashboard's interval, falling back to it silently if the
// argument fails to parse (parseIntervalArg's regexp match already
// guarantees this can't happen for text the macro regexps accept).
func resolveGroupInterval(dashboardInterval time.Duration, arg string) time.Duration {
	if arg == "" {
		return dashboardInterval
	}
	if parsed, err := parseIntervalArg(arg); err == nil {
		return parsed
	}
	return dashboardInterval
}

// dateTruncUnitAndBinSize converts a duration into the largest whole
// $dateTrunc unit/binSize pair that represents it, e.g. 2*time.Hour ->
// ("hour", 2), 90*time.Second -> ("second", 90). $dateTrunc has no
// millisecond unit, so sub-second or otherwise non-whole-second durations
// are rounded to the nearest second (minimum 1s).
func dateTruncUnitAndBinSize(d time.Duration) (string, int64) {
	switch {
	case d <= 0:
		return "second", 1
	case d%(24*time.Hour) == 0:
		return "day", int64(d / (24 * time.Hour))
	case d%time.Hour == 0:
		return "hour", int64(d / time.Hour)
	case d%time.Minute == 0:
		return "minute", int64(d / time.Minute)
	case d%time.Second == 0:
		return "second", int64(d / time.Second)
	default:
		return "second", max(int64(d.Round(time.Second)/time.Second), 1)
	}
}

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
	Limit      int64  `json:"limit"`      // find/aggregate, 0 = no limit
	Skip       int64  `json:"skip"`       // find/aggregate
	Format     string `json:"format"`     // table | timeseries | long | logs

	// MessageField/LevelField rename the given document field to the
	// canonical "message"/"level" column the logs visualization looks for
	// by convention. Only applies when Format is "logs"; empty leaves
	// columns as-is.
	MessageField string `json:"messageField"`
	LevelField   string `json:"levelField"`

	// FlattenDepth caps how many levels of nested documents get flattened
	// into dot-notation columns before a nested document is instead kept
	// whole as a single JSON-encoded column. <= 0 (the zero value) means
	// unlimited, i.e. today's behavior of flattening every level.
	FlattenDepth int64 `json:"flattenDepth"`
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

	text = unixEpochFilterRe.ReplaceAllStringFunc(text, func(match string) string {
		field := unixEpochFilterRe.FindStringSubmatch(match)[1]
		return fmt.Sprintf(`{%q: {"$gte": %s, "$lte": %s}}`, field, fromEpochSec, toEpochSec)
	})

	text = timeGroupRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := timeGroupRe.FindStringSubmatch(match)
		field, arg := sub[1], sub[2]
		d := resolveGroupInterval(q.Interval, arg)
		unit, binSize := dateTruncUnitAndBinSize(d)
		return fmt.Sprintf(`{"$dateTrunc": {"date": "$%s", "unit": %q, "binSize": %d}}`, field, unit, binSize)
	})

	text = unixEpochGroupRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := unixEpochGroupRe.FindStringSubmatch(match)
		field, arg := sub[1], sub[2]
		d := resolveGroupInterval(q.Interval, arg)
		intervalSec := max(int64(d.Round(time.Second)/time.Second), 1)
		return fmt.Sprintf(`{"$subtract": ["$%s", {"$mod": ["$%s", %d]}]}`, field, field, intervalSec)
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
