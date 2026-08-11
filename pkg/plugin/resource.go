package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ backend.CallResourceHandler = (*Datasource)(nil)

// fieldSampleSize bounds how many documents /fields samples to discover
// keys, keeping discovery cheap on large collections.
const fieldSampleSize = 50

// CallResource implements the schema discovery endpoints (/databases,
// /collections, /fields) that back autocomplete in the query and variable
// editors. Discovery is opt-in: settings.SchemaDiscoveryEnabled must be set,
// since listing databases/collections or sampling for fields can be slow on
// large clusters and admins may not want every collection exposed.
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !d.settings.SchemaDiscoveryEnabled {
		return sendJSON(sender, http.StatusNotImplemented, map[string]string{"message": "schema discovery is disabled for this datasource"})
	}

	reqURL, err := url.Parse(req.URL)
	if err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"message": err.Error()})
	}
	q := reqURL.Query()

	switch strings.TrimPrefix(req.Path, "/") {
	case "databases":
		return d.handleDatabases(ctx, sender)
	case "collections":
		return d.handleCollections(ctx, sender, q.Get("db"))
	case "fields":
		return d.handleFields(ctx, sender, q.Get("db"), q.Get("collection"))
	default:
		return sendJSON(sender, http.StatusNotFound, map[string]string{"message": "unknown resource"})
	}
}

func (d *Datasource) handleDatabases(ctx context.Context, sender backend.CallResourceResponseSender) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.settings.TimeoutSeconds)*time.Second)
	defer cancel()

	names, err := d.client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"message": err.Error()})
	}

	filter := newCollectionFilter(d.settings.CollectionFilters)
	result := make([]string, 0, len(names))
	for _, name := range names {
		if filter.databaseAllowed(name) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return sendJSON(sender, http.StatusOK, result)
}

func (d *Datasource) handleCollections(ctx context.Context, sender backend.CallResourceResponseSender, db string) error {
	if db == "" {
		db = d.settings.Database
	}
	if db == "" {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"message": "db is required"})
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.settings.TimeoutSeconds)*time.Second)
	defer cancel()

	names, err := d.client.Database(db).ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"message": err.Error()})
	}

	filter := newCollectionFilter(d.settings.CollectionFilters)
	result := make([]string, 0, len(names))
	for _, name := range names {
		if filter.collectionAllowed(db, name) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return sendJSON(sender, http.StatusOK, result)
}

func (d *Datasource) handleFields(ctx context.Context, sender backend.CallResourceResponseSender, db, collection string) error {
	if db == "" {
		db = d.settings.Database
	}
	if db == "" || collection == "" {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"message": "db and collection are required"})
	}

	filter := newCollectionFilter(d.settings.CollectionFilters)
	if !filter.collectionAllowed(db, collection) {
		return sendJSON(sender, http.StatusForbidden, map[string]string{"message": "collection is not accessible"})
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.settings.TimeoutSeconds)*time.Second)
	defer cancel()

	pipeline := []bson.D{{{Key: "$sample", Value: bson.D{{Key: "size", Value: fieldSampleSize}}}}}
	cursor, err := d.client.Database(db).Collection(collection).Aggregate(ctx, pipeline)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"message": err.Error()})
	}
	var docs []bson.D
	if err := cursor.All(ctx, &docs); err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"message": err.Error()})
	}

	seen := map[string]struct{}{}
	for _, doc := range docs {
		var flat []bson.E
		flattenDoc("", doc, &flat, map[string]int{})
		for _, e := range flat {
			seen[e.Key] = struct{}{}
		}
	}
	fields := make([]string, 0, len(seen))
	for field := range seen {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return sendJSON(sender, http.StatusOK, fields)
}

func sendJSON(sender backend.CallResourceResponseSender, status int, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    b,
	})
}

// collectionFilter evaluates glob allow/deny patterns against
// "database.collection" names, as configured by settings.CollectionFilters.
type collectionFilter struct {
	allow []string
	deny  []string
}

func newCollectionFilter(patterns []string) collectionFilter {
	var f collectionFilter
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(p, "!"); ok {
			f.deny = append(f.deny, rest)
		} else {
			f.allow = append(f.allow, p)
		}
	}
	return f
}

func (f collectionFilter) allowed(name string) bool {
	for _, p := range f.deny {
		if globMatch(p, name) {
			return false
		}
	}
	if len(f.allow) == 0 {
		return true
	}
	for _, p := range f.allow {
		if globMatch(p, name) {
			return true
		}
	}
	return false
}

// databaseAllowed checks a bare database name against the database segment
// of each pattern (the part before the first "."), so a collection-level
// pattern like "sampledb.*" also permits "sampledb" to appear in the
// database list.
func (f collectionFilter) databaseAllowed(db string) bool {
	dbFilter := collectionFilter{
		allow: databaseSegments(f.allow),
		deny:  databaseSegments(f.deny),
	}
	return dbFilter.allowed(db)
}

func (f collectionFilter) collectionAllowed(db, collection string) bool {
	return f.allowed(db + "." + collection)
}

func databaseSegments(patterns []string) []string {
	segments := make([]string, len(patterns))
	for i, p := range patterns {
		if idx := strings.Index(p, "."); idx >= 0 {
			p = p[:idx]
		}
		segments[i] = p
	}
	return segments
}

func globMatch(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	return err == nil && ok
}
