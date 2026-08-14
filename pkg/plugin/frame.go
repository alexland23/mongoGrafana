package plugin

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// column accumulates one output field. Values are normalized to one of
// *time.Time, *bool, *float64 or *string so heterogeneous documents can
// share a column.
type column struct {
	name string
	kind data.FieldType // FieldTypeNullableTime/Bool/Float64/String
	vals []any
}

type frameBuilder struct {
	cols  map[string]*column
	order []string
	rows  int
}

func newFrameBuilder() *frameBuilder {
	return &frameBuilder{cols: map[string]*column{}}
}

// addDocument flattens one document into the builder as a new row.
func (b *frameBuilder) addDocument(doc bson.D) {
	flat := make([]bson.E, 0, len(doc))
	flattenDoc("", doc, &flat, map[string]int{})

	for _, e := range flat {
		name, raw := e.Key, e.Value
		kind, val := normalizeValue(raw)
		if val == nil && kind == data.FieldTypeUnknown {
			kind = data.FieldTypeNullableString
		}
		col, ok := b.cols[name]
		if !ok {
			col = &column{name: name, kind: kind, vals: make([]any, b.rows)}
			b.cols[name] = col
			b.order = append(b.order, name)
		}
		if val != nil && col.kind != kind {
			val = coerce(val, kind, col.kind, col)
		}
		col.vals = append(col.vals, val)
	}
	b.rows++
	for _, col := range b.cols {
		if len(col.vals) < b.rows {
			col.vals = append(col.vals, nil)
		}
	}
}

// resetRows clears a builder's accumulated rows while preserving its
// established column schema (names, order, types). Reusing a builder across
// streamed frames this way keeps the schema stable for the life of a live
// channel instead of rederiving it from scratch per frame, so a field seen
// on an earlier event but absent from the current one comes through as null
// rather than vanishing from the frame.
func (b *frameBuilder) resetRows() {
	b.rows = 0
	for _, col := range b.cols {
		col.vals = col.vals[:0]
	}
}

// coerce reconciles a value whose kind differs from the column's kind.
// Numbers arriving in a string column are stringified; strings arriving in a
// numeric column promote the whole column to string.
func coerce(val any, kind, want data.FieldType, col *column) any {
	if want == data.FieldTypeNullableString {
		return stringify(val, kind)
	}
	// Promote the column to string and convert existing values.
	old := col.kind
	col.kind = data.FieldTypeNullableString
	for i, v := range col.vals {
		if v != nil {
			col.vals[i] = stringify(v, old)
		}
	}
	return stringify(val, kind)
}

func stringify(val any, kind data.FieldType) any {
	var s string
	switch v := val.(type) {
	case *string:
		return v
	case *float64:
		s = strconv.FormatFloat(*v, 'f', -1, 64)
	case *bool:
		s = strconv.FormatBool(*v)
	case *time.Time:
		s = v.Format(time.RFC3339Nano)
	default:
		b, _ := json.Marshal(val)
		s = string(b)
	}
	return &s
}

// flattenDoc flattens nested documents using dot notation, preserving field
// order so column order stays stable across queries (map iteration order in
// Go is randomized, so an intermediate map would reshuffle columns on every
// run). Arrays and other non-scalar leaves are kept whole and later
// JSON-encoded. seen tracks each key's index in out so a repeated key
// overwrites in place rather than duplicating.
func flattenDoc(prefix string, doc bson.D, out *[]bson.E, seen map[string]int) {
	for _, e := range doc {
		key := e.Key
		if prefix != "" {
			key = prefix + "." + key
		}
		if sub, ok := e.Value.(bson.D); ok {
			flattenDoc(key, sub, out, seen)
			continue
		}
		if idx, ok := seen[key]; ok {
			(*out)[idx] = bson.E{Key: key, Value: e.Value}
			continue
		}
		seen[key] = len(*out)
		*out = append(*out, bson.E{Key: key, Value: e.Value})
	}
}

// normalizeValue converts a BSON value into one of the four supported
// nullable field types.
func normalizeValue(v any) (data.FieldType, any) {
	switch val := v.(type) {
	case nil:
		return data.FieldTypeUnknown, nil
	case string:
		return data.FieldTypeNullableString, &val
	case bool:
		return data.FieldTypeNullableBool, &val
	case int32:
		f := float64(val)
		return data.FieldTypeNullableFloat64, &f
	case int64:
		f := float64(val)
		return data.FieldTypeNullableFloat64, &f
	case float64:
		f := val
		return data.FieldTypeNullableFloat64, &f
	case bson.DateTime:
		t := val.Time().UTC()
		return data.FieldTypeNullableTime, &t
	case time.Time:
		t := val.UTC()
		return data.FieldTypeNullableTime, &t
	case bson.ObjectID:
		s := val.Hex()
		return data.FieldTypeNullableString, &s
	case bson.Decimal128:
		if f, err := strconv.ParseFloat(val.String(), 64); err == nil {
			return data.FieldTypeNullableFloat64, &f
		}
		s := val.String()
		return data.FieldTypeNullableString, &s
	case bson.Timestamp:
		t := time.Unix(int64(val.T), 0).UTC()
		return data.FieldTypeNullableTime, &t
	case bson.Binary:
		s := base64.StdEncoding.EncodeToString(val.Data)
		return data.FieldTypeNullableString, &s
	case bson.Null, bson.Undefined:
		return data.FieldTypeUnknown, nil
	default:
		// Arrays, embedded structures and exotic types become JSON strings.
		s := toJSONString(v)
		return data.FieldTypeNullableString, &s
	}
}

// toJSONString renders any BSON value as relaxed JSON text.
func toJSONString(v any) string {
	plain := toPlain(v)
	b, err := json.Marshal(plain)
	if err != nil {
		return ""
	}
	return string(b)
}

// toPlain recursively converts bson.D/bson.A into json.Marshal-friendly
// maps and slices.
func toPlain(v any) any {
	switch val := v.(type) {
	case bson.D:
		m := map[string]any{}
		for _, e := range val {
			m[e.Key] = toPlain(e.Value)
		}
		return m
	case bson.M:
		m := map[string]any{}
		for k, e := range val {
			m[k] = toPlain(e)
		}
		return m
	case bson.A:
		s := make([]any, len(val))
		for i, e := range val {
			s[i] = toPlain(e)
		}
		return s
	case bson.DateTime:
		return val.Time().UTC().Format(time.RFC3339Nano)
	case bson.ObjectID:
		return val.Hex()
	case bson.Decimal128:
		return val.String()
	default:
		return val
	}
}

// buildFrame assembles the accumulated columns into a data frame. Column
// order follows first appearance in the result set, with time columns kept
// first so table and time series rendering behave predictably.
func (b *frameBuilder) buildFrame(name string) *data.Frame {
	names := make([]string, len(b.order))
	copy(names, b.order)
	sort.SliceStable(names, func(i, j int) bool {
		return b.cols[names[i]].kind == data.FieldTypeNullableTime && b.cols[names[j]].kind != data.FieldTypeNullableTime
	})

	frame := data.NewFrame(name)
	for _, colName := range names {
		col := b.cols[colName]
		frame.Fields = append(frame.Fields, col.toField())
	}
	return frame
}

func (c *column) toField() *data.Field {
	switch c.kind {
	case data.FieldTypeNullableTime:
		vals := make([]*time.Time, len(c.vals))
		for i, v := range c.vals {
			if v != nil {
				vals[i] = v.(*time.Time)
			}
		}
		return data.NewField(c.name, nil, vals)
	case data.FieldTypeNullableBool:
		vals := make([]*bool, len(c.vals))
		for i, v := range c.vals {
			if v != nil {
				vals[i] = v.(*bool)
			}
		}
		return data.NewField(c.name, nil, vals)
	case data.FieldTypeNullableFloat64:
		vals := make([]*float64, len(c.vals))
		for i, v := range c.vals {
			if v != nil {
				vals[i] = v.(*float64)
			}
		}
		return data.NewField(c.name, nil, vals)
	default:
		vals := make([]*string, len(c.vals))
		for i, v := range c.vals {
			if v != nil {
				vals[i] = v.(*string)
			}
		}
		return data.NewField(c.name, nil, vals)
	}
}

// derivedField extracts one extra clickable link column out of a logs
// frame's message field, e.g. pulling a trace ID out of a log line and
// linking it to a tracing UI. Compiled once from models.DerivedFieldConfig
// at datasource construction (see Datasource.derivedFields) rather than per
// query.
type derivedField struct {
	re              *regexp.Regexp
	name            string
	url             string
	urlDisplayLabel string
}

// logFieldOptions configures how a "logs" format frame is shaped. Ignored
// for every other format.
type logFieldOptions struct {
	// messageField/levelField rename the given document field (if present)
	// to the canonical "message"/"level" column the logs visualization
	// looks for. Empty leaves columns as-is, i.e. today's behavior of
	// relying on fields already being named "message"/"level".
	messageField string
	levelField   string
	// derivedFields extracts extra link columns out of the message column.
	derivedFields []derivedField
}

// docsToFrame converts query results into a frame shaped for the requested
// format: "timeseries" attempts a long-to-wide conversion, "logs" tags the
// frame for the logs visualization, anything else stays tabular.
func docsToFrame(docs []bson.D, refID, format string) *data.Frame {
	return buildDocsFrame(newFrameBuilder(), docs, refID, format, logFieldOptions{})
}

// docsToFrameWithLogOptions is docsToFrame with logs-specific field mapping
// and derived fields applied (see logFieldOptions).
func docsToFrameWithLogOptions(docs []bson.D, refID, format string, opts logFieldOptions) *data.Frame {
	return buildDocsFrame(newFrameBuilder(), docs, refID, format, opts)
}

// docsToStreamFrame is docsToFrame but built onto a builder the caller keeps
// across calls, so the schema (field set/order/types) it establishes stays
// stable across the many frames sent over a live channel's lifetime instead
// of being rederived from scratch each call -- important for change-stream
// events, which are typically converted one document at a time.
func docsToStreamFrame(b *frameBuilder, docs []bson.D, refID, format string) *data.Frame {
	b.resetRows()
	return buildDocsFrame(b, docs, refID, format, logFieldOptions{})
}

// docsToStreamFrameWithLogOptions is docsToStreamFrame with logs-specific
// field mapping and derived fields applied (see logFieldOptions).
func docsToStreamFrameWithLogOptions(b *frameBuilder, docs []bson.D, refID, format string, opts logFieldOptions) *data.Frame {
	b.resetRows()
	return buildDocsFrame(b, docs, refID, format, opts)
}

func buildDocsFrame(b *frameBuilder, docs []bson.D, refID, format string, opts logFieldOptions) *data.Frame {
	for _, doc := range docs {
		b.addDocument(doc)
	}
	frame := b.buildFrame(refID)

	switch format {
	case "timeseries":
		return toTimeSeries(frame)
	case "logs":
		renameField(frame, opts.messageField, "message")
		renameField(frame, opts.levelField, "level")
		applyDerivedFields(frame, opts.derivedFields)
		frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeLogs}
		return frame
	default:
		frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}
		return frame
	}
}

// renameField points the logs visualization at a differently-named document
// field by renaming it to the canonical name ("message" or "level") it
// looks for by convention. A blank "from", a from equal to the canonical
// name, a missing source column, or a pre-existing column already using the
// canonical name are all no-ops -- the last of those avoids silently
// clobbering a field that's already correctly named.
func renameField(frame *data.Frame, from, to string) {
	from = strings.TrimSpace(from)
	if from == "" || from == to {
		return
	}
	for _, f := range frame.Fields {
		if f.Name == to {
			return
		}
	}
	for _, f := range frame.Fields {
		if f.Name == from {
			f.Name = to
			return
		}
	}
}

// applyDerivedFields appends one nullable-string column per configured
// derived field, extracting a value from each row's "message" column and
// attaching it as a clickable data link (e.g. a trace ID linked to a
// tracing UI). Rows the pattern doesn't match get a nil value for that
// column. A no-op when there's no "message" column (e.g. renameField above
// found nothing to rename) or no derived fields are configured. A derived
// field whose name collides with an existing column (document column or an
// earlier derived field) is skipped rather than clobbering it, matching
// renameField's precedent.
func applyDerivedFields(frame *data.Frame, derived []derivedField) {
	if len(derived) == 0 {
		return
	}
	var msg *data.Field
	for _, f := range frame.Fields {
		if f.Name == "message" {
			msg = f
			break
		}
	}
	if msg == nil {
		return
	}

	names := make(map[string]struct{}, len(frame.Fields))
	for _, f := range frame.Fields {
		names[f.Name] = struct{}{}
	}

	for _, d := range derived {
		if _, exists := names[d.name]; exists {
			continue
		}
		vals := make([]*string, msg.Len())
		for i := 0; i < msg.Len(); i++ {
			v, ok := msg.ConcreteAt(i)
			if !ok {
				continue
			}
			s, ok := v.(string)
			if !ok {
				continue
			}
			m := d.re.FindStringSubmatch(s)
			if m == nil {
				continue
			}
			extracted := m[0]
			if len(m) > 1 {
				extracted = m[1]
			}
			vals[i] = &extracted
		}
		label := d.urlDisplayLabel
		if label == "" {
			label = d.name
		}
		field := data.NewField(d.name, nil, vals)
		field.Config = &data.FieldConfig{
			Links: []data.DataLink{{Title: label, URL: d.url, TargetBlank: true}},
		}
		frame.Fields = append(frame.Fields, field)
		names[d.name] = struct{}{}
	}
}

// toTimeSeries converts a long frame (time + value + optional string labels)
// into wide time series. If conversion is impossible the long frame is
// returned with an explanatory notice, since many panels handle long frames.
func toTimeSeries(frame *data.Frame) *data.Frame {
	sortFrameByTime(frame)

	tsSchema := frame.TimeSeriesSchema()
	if tsSchema.Type == data.TimeSeriesTypeLong {
		wide, err := data.LongToWide(frame, &data.FillMissing{Mode: data.FillModeNull})
		if err == nil {
			wide.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesWide}
			return wide
		}
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityWarning,
			Text:     "could not convert long frame to wide time series: " + err.Error(),
		})
	}
	if frame.Meta == nil {
		frame.Meta = &data.FrameMeta{}
	}
	return frame
}

// sortFrameByTime orders rows ascending by the first time field, which
// LongToWide requires.
func sortFrameByTime(frame *data.Frame) {
	timeIdx := -1
	for i, f := range frame.Fields {
		if f.Type() == data.FieldTypeNullableTime || f.Type() == data.FieldTypeTime {
			timeIdx = i
			break
		}
	}
	if timeIdx < 0 || frame.Rows() == 0 {
		return
	}

	idx := make([]int, frame.Rows())
	for i := range idx {
		idx[i] = i
	}
	timeField := frame.Fields[timeIdx]
	at := func(i int) time.Time {
		v, ok := timeField.ConcreteAt(i)
		if !ok {
			return time.Time{}
		}
		return v.(time.Time)
	}
	sort.SliceStable(idx, func(i, j int) bool { return at(idx[i]).Before(at(idx[j])) })

	for _, f := range frame.Fields {
		vals := make([]any, frame.Rows())
		for newPos, oldPos := range idx {
			vals[newPos] = f.At(oldPos)
		}
		for i, v := range vals {
			f.Set(i, v)
		}
	}
}
