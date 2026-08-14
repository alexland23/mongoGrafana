package plugin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/alandave/mongo-db/pkg/models"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// NewDatasource creates a new datasource instance. One mongo.Client is shared
// by all queries of a datasource instance; the SDK disposes and recreates the
// instance whenever its settings change.
func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	config, err := models.LoadPluginSettings(settings)
	if err != nil {
		return nil, err
	}

	opts := options.Client().ApplyURI(config.ConnectionString)
	if config.Username != "" {
		cred := options.Credential{
			Username:    config.Username,
			Password:    config.Secrets.Password,
			PasswordSet: true,
		}
		opts.SetAuth(cred)
	}
	opts.SetConnectTimeout(time.Duration(config.ConnectTimeoutSeconds) * time.Second)
	if config.MaxPoolSize > 0 {
		opts.SetMaxPoolSize(config.MaxPoolSize)
	}
	if config.ReadPreference != "" {
		rp, err := readPreferenceFromString(config.ReadPreference)
		if err != nil {
			return nil, err
		}
		opts.SetReadPreference(rp)
	}
	if config.TLSEnabled {
		tlsConfig, err := buildTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS config: %w", err)
		}
		opts.SetTLSConfig(tlsConfig)
	}

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create MongoDB client: %w", err)
	}

	return &Datasource{client: client, settings: config, derivedFields: compileDerivedFields(config.DerivedFields)}, nil
}

// compileDerivedFields precompiles each derived field's regex once at
// datasource construction instead of per query. Entries with an invalid
// pattern or no name/URL are dropped with a warning rather than failing
// datasource construction outright, since the rest of the datasource still
// works fine without them.
func compileDerivedFields(configs []models.DerivedFieldConfig) []derivedField {
	fields := make([]derivedField, 0, len(configs))
	for _, c := range configs {
		if c.Name == "" || c.MatcherRegex == "" || c.URL == "" {
			log.DefaultLogger.Warn("skipping derived field with missing name, matcher regex, or URL", "name", c.Name)
			continue
		}
		re, err := regexp.Compile(c.MatcherRegex)
		if err != nil {
			log.DefaultLogger.Warn("skipping derived field with invalid regex", "name", c.Name, "error", err)
			continue
		}
		fields = append(fields, derivedField{re: re, name: c.Name, url: c.URL, urlDisplayLabel: c.URLDisplayLabel})
	}
	return fields
}

// frameOptionsFor builds the frame options for one query, combining its
// per-query logs field mapping and flatten-depth override with the
// datasource's derived fields.
func (d *Datasource) frameOptionsFor(qm queryModel) frameOptions {
	return frameOptions{
		messageField:  qm.MessageField,
		levelField:    qm.LevelField,
		derivedFields: d.derivedFields,
		maxDepth:      int(qm.FlattenDepth),
	}
}

func readPreferenceFromString(mode string) (*readpref.ReadPref, error) {
	switch mode {
	case "primary":
		return readpref.Primary(), nil
	case "primaryPreferred":
		return readpref.PrimaryPreferred(), nil
	case "secondary":
		return readpref.Secondary(), nil
	case "secondaryPreferred":
		return readpref.SecondaryPreferred(), nil
	case "nearest":
		return readpref.Nearest(), nil
	default:
		return nil, fmt.Errorf("unknown read preference %q", mode)
	}
}

func buildTLSConfig(config *models.PluginSettings) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.TLSSkipVerify, //nolint:gosec // user-controlled opt-in for self-signed/dev deployments
	}

	caCert, err := loadPEMSource(config.TLSCACertPath, config.Secrets.TLSCACert)
	if err != nil {
		return nil, fmt.Errorf("CA certificate: %w", err)
	}
	if len(caCert) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("could not parse CA certificate")
		}
		tlsConfig.RootCAs = pool
	}

	clientCert, err := loadPEMSource(config.TLSClientCertPath, config.Secrets.TLSClientCert)
	if err != nil {
		return nil, fmt.Errorf("client certificate: %w", err)
	}
	clientKey, err := loadPEMSource(config.TLSClientKeyPath, config.Secrets.TLSClientKey)
	if err != nil {
		return nil, fmt.Errorf("client key: %w", err)
	}
	if len(clientCert) > 0 || len(clientKey) > 0 {
		cert, err := tls.X509KeyPair(clientCert, clientKey)
		if err != nil {
			return nil, fmt.Errorf("could not parse client certificate/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// loadPEMSource reads PEM content from path when set, otherwise falls back
// to the given inline content (e.g. pasted into the config UI).
func loadPEMSource(path, content string) ([]byte, error) {
	if path == "" {
		return []byte(content), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read %q: %w", path, err)
	}
	return data, nil
}

// Datasource is a MongoDB datasource instance.
type Datasource struct {
	client   *mongo.Client
	settings *models.PluginSettings

	// derivedFields extracts extra clickable link columns out of "logs"
	// format results (e.g. a trace ID linked to a tracing UI), precompiled
	// from settings.DerivedFields at construction. See frame.go.
	derivedFields []derivedField

	// streamBaselines holds the newest backlog _id SubscribeStream saw for a
	// given channel path, so RunStream's tail can pick up exactly where the
	// backlog left off instead of independently querying its own "latest"
	// baseline, which would leave a gap for documents inserted between the
	// two queries. See stream.go.
	streamBaselines sync.Map // map[string]any

	// streamSchemas holds the frameBuilder SubscribeStream used to build a
	// channel's initial backlog frame, so RunStream's tail reuses that same
	// builder for every frame it sends. This keeps the streamed schema
	// (field set/order/types) stable for the life of the stream instead of
	// rederiving it from scratch per event, which would otherwise let fields
	// silently vanish from a frame whenever an event happened to lack them.
	// See stream.go.
	streamSchemas sync.Map // map[string]*frameBuilder
}

// Dispose cleans up the client when the instance is replaced.
func (d *Datasource) Dispose() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.client.Disconnect(ctx)
}

// QueryData handles multiple queries and returns multiple responses.
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}

	return response, nil
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var qm queryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err))
	}

	dbName := qm.Database
	if dbName == "" {
		dbName = d.settings.Database
	}
	if dbName == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "no database configured: set one on the datasource or the query")
	}

	queryType := qm.QueryType
	if queryType == "" {
		queryType = "aggregate"
	}
	if qm.Collection == "" && queryType != "command" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "collection is required")
	}

	timeout := time.Duration(d.settings.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	db := d.client.Database(dbName)
	coll := db.Collection(qm.Collection)
	text := interpolateMacros(qm.QueryText, query)
	guard := newOperatorGuard(d.settings)

	var (
		docs []bson.D
		err  error
	)
	switch queryType {
	case "aggregate":
		docs, err = runAggregate(ctx, coll, text, guard, d.settings.MaxDocuments)
	case "find":
		docs, err = runFind(ctx, coll, text, qm, guard, d.settings.MaxDocuments)
	case "count":
		docs, err = runCount(ctx, coll, text, guard)
	case "distinct":
		docs, err = runDistinct(ctx, coll, text, qm.Field, guard)
	case "command":
		docs, err = runCommand(ctx, db, text, guard)
	default:
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("unknown query type %q", queryType))
	}
	if err != nil {
		return backend.ErrDataResponse(classifyQueryError(err), fmt.Sprintf("%s query failed: %v", queryType, err))
	}

	frame := docsToFrameWithLogOptions(docs, query.RefID, qm.Format, d.frameOptionsFor(qm))
	if frame.Meta == nil {
		frame.Meta = &data.FrameMeta{}
	}
	frame.Meta.ExecutedQueryString = text

	var response backend.DataResponse
	response.Frames = append(response.Frames, frame)
	return response
}

func runAggregate(ctx context.Context, coll *mongo.Collection, text string, guard *operatorGuard, maxDocuments int64) ([]bson.D, error) {
	pipeline, err := parsePipeline(text)
	if err != nil {
		return nil, err
	}
	if err := guard.checkPipeline(pipeline); err != nil {
		return nil, err
	}
	pipeline = applyMaxDocumentsLimit(pipeline, maxDocuments)
	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var docs []bson.D
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func runFind(ctx context.Context, coll *mongo.Collection, text string, qm queryModel, guard *operatorGuard, maxDocuments int64) ([]bson.D, error) {
	filter, err := parseDocument(text)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	if err := guard.checkDoc(filter); err != nil {
		return nil, err
	}
	opts := options.Find()
	if qm.Projection != "" {
		projection, err := parseDocument(qm.Projection)
		if err != nil {
			return nil, fmt.Errorf("projection: %w", err)
		}
		opts.SetProjection(projection)
	}
	if qm.Sort != "" {
		sortDoc, err := parseDocument(qm.Sort)
		if err != nil {
			return nil, fmt.Errorf("sort: %w", err)
		}
		opts.SetSort(sortDoc)
	}
	if qm.Skip > 0 {
		opts.SetSkip(qm.Skip)
	}
	if limit := resolveFindLimit(qm.Limit, maxDocuments); limit > 0 {
		opts.SetLimit(limit)
	}
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var docs []bson.D
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func runCount(ctx context.Context, coll *mongo.Collection, text string, guard *operatorGuard) ([]bson.D, error) {
	filter, err := parseDocument(text)
	if err != nil {
		return nil, err
	}
	if err := guard.checkDoc(filter); err != nil {
		return nil, err
	}
	count, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}
	return []bson.D{{{Key: "count", Value: count}}}, nil
}

func runDistinct(ctx context.Context, coll *mongo.Collection, text, field string, guard *operatorGuard) ([]bson.D, error) {
	if field == "" {
		return nil, fmt.Errorf("invalid distinct query: field is required")
	}
	filter, err := parseDocument(text)
	if err != nil {
		return nil, err
	}
	if err := guard.checkDoc(filter); err != nil {
		return nil, err
	}
	var values bson.A
	if err := coll.Distinct(ctx, field, filter).Decode(&values); err != nil {
		return nil, err
	}
	docs := make([]bson.D, 0, len(values))
	for _, v := range values {
		docs = append(docs, bson.D{{Key: field, Value: v}})
	}
	return docs, nil
}

func runCommand(ctx context.Context, db *mongo.Database, text string, guard *operatorGuard) ([]bson.D, error) {
	cmd, err := parseDocument(text)
	if err != nil {
		return nil, err
	}
	if len(cmd) == 0 {
		return nil, fmt.Errorf("invalid command: document is empty")
	}
	if err := guard.checkCommand(cmd); err != nil {
		return nil, err
	}
	var result bson.D
	if err := db.RunCommand(ctx, cmd).Decode(&result); err != nil {
		return nil, err
	}
	// Commands that return cursors (e.g. manually issued find/aggregate)
	// carry their rows in cursor.firstBatch; surface those rows directly.
	if batch, ok := extractFirstBatch(result); ok {
		return batch, nil
	}
	return []bson.D{result}, nil
}

func extractFirstBatch(result bson.D) ([]bson.D, bool) {
	for _, e := range result {
		if e.Key != "cursor" {
			continue
		}
		cursorDoc, ok := e.Value.(bson.D)
		if !ok {
			return nil, false
		}
		for _, ce := range cursorDoc {
			if ce.Key != "firstBatch" {
				continue
			}
			arr, ok := ce.Value.(bson.A)
			if !ok {
				return nil, false
			}
			docs := make([]bson.D, 0, len(arr))
			for _, item := range arr {
				if doc, ok := item.(bson.D); ok {
					docs = append(docs, doc)
				}
			}
			return docs, true
		}
	}
	return nil, false
}

// CheckHealth pings the server so the "Save & test" button verifies both
// connectivity and credentials.
func (d *Datasource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	res := &backend.CheckHealthResult{}
	_, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = err.Error()
		return res, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := d.client.Ping(ctx, readpref.Primary()); err != nil {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("could not connect to MongoDB: %v", err)
		return res, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Successfully connected to MongoDB",
	}, nil
}
