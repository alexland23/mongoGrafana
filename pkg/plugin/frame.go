package plugin

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
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
	flat := map[string]any{}
	flattenDoc("", doc, flat)

	for name, raw := range flat {
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

// flattenDoc flattens nested documents using dot notation. Arrays and other
// non-scalar leaves are kept whole and later JSON-encoded.
func flattenDoc(prefix string, doc bson.D, out map[string]any) {
	for _, e := range doc {
		key := e.Key
		if prefix != "" {
			key = prefix + "." + key
		}
		if sub, ok := e.Value.(bson.D); ok {
			flattenDoc(key, sub, out)
			continue
		}
		out[key] = e.Value
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

// docsToFrame converts query results into a frame shaped for the requested
// format: "timeseries" attempts a long-to-wide conversion, "logs" tags the
// frame for the logs visualization, anything else stays tabular.
func docsToFrame(docs []bson.D, refID, format string) *data.Frame {
	b := newFrameBuilder()
	for _, doc := range docs {
		b.addDocument(doc)
	}
	frame := b.buildFrame(refID)

	switch format {
	case "timeseries":
		return toTimeSeries(frame)
	case "logs":
		frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeLogs}
		return frame
	default:
		frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}
		return frame
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
