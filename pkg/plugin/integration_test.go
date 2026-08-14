//go:build integration

// Integration tests exercise the datasource against a real MongoDB server
// started with testcontainers-go, covering the paths the unit tests can't:
// actual driver round-trips for each of the 5 query handlers, CheckHealth,
// and authentication failure. They require a working Docker daemon and are
// excluded from the default `mage test` / `go test ./...` run (see
// pkg/plugin/query.go's queryModel and datasource.go's query handlers).
//
// Run with: go test -tags=integration ./pkg/plugin/... -run Integration -v
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	tcmongodb "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const testDatabase = "sampledb"

// startMongoContainer starts a standalone, unauthenticated mongo:7 container
// (matching the image used by dev/mongo-seed.js) and returns its connection
// string. The container is terminated via t.Cleanup.
func startMongoContainer(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	container, err := tcmongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate mongodb container: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	return uri
}

// seedMetrics inserts documents into testDatabase's "metrics" collection
// using a plain driver client, independent of the Datasource under test.
func seedMetrics(t *testing.T, uri string, docs []bson.D) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect seed client: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	if _, err := client.Database(testDatabase).Collection("metrics").InsertMany(ctx, docs); err != nil {
		t.Fatalf("failed to seed metrics: %v", err)
	}
}

// newDatasource builds a *Datasource the same way Grafana does, via
// NewDatasource, from the given jsonData/secrets.
func newDatasource(t *testing.T, jsonData map[string]any, secrets map[string]string) *Datasource {
	t.Helper()

	raw, err := json.Marshal(jsonData)
	if err != nil {
		t.Fatalf("failed to marshal jsonData: %v", err)
	}

	inst, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
		JSONData:                raw,
		DecryptedSecureJSONData: secrets,
	})
	if err != nil {
		t.Fatalf("NewDatasource() error: %v", err)
	}
	ds, ok := inst.(*Datasource)
	if !ok {
		t.Fatalf("NewDatasource() returned %T, want *Datasource", inst)
	}
	t.Cleanup(ds.Dispose)
	return ds
}

func testTimeRangeQuery(t *testing.T, refID string, qm queryModel) backend.DataQuery {
	t.Helper()
	body, err := json.Marshal(qm)
	if err != nil {
		t.Fatalf("failed to marshal queryModel: %v", err)
	}
	return backend.DataQuery{
		RefID: refID,
		JSON:  body,
		TimeRange: backend.TimeRange{
			From: time.Now().Add(-time.Hour),
			To:   time.Now(),
		},
		Interval:      30 * time.Second,
		MaxDataPoints: 100,
	}
}

func runSingleQuery(t *testing.T, ds *Datasource, qm queryModel) backend.DataResponse {
	t.Helper()
	req := &backend.QueryDataRequest{Queries: []backend.DataQuery{testTimeRangeQuery(t, "A", qm)}}
	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("QueryData() error: %v", err)
	}
	res, ok := resp.Responses["A"]
	if !ok {
		t.Fatalf("QueryData() response missing RefID \"A\": %+v", resp.Responses)
	}
	return res
}

// TestIntegrationQueryHandlers exercises all 5 query handlers (aggregate,
// find, count, distinct, command) against a real MongoDB server.
func TestIntegrationQueryHandlers(t *testing.T) {
	uri := startMongoContainer(t)
	seedMetrics(t, uri, []bson.D{
		{{Key: "host", Value: "a"}, {Key: "value", Value: int32(1)}},
		{{Key: "host", Value: "a"}, {Key: "value", Value: int32(3)}},
		{{Key: "host", Value: "b"}, {Key: "value", Value: int32(2)}},
	})

	ds := newDatasource(t, map[string]any{
		"connectionString":      uri,
		"database":              testDatabase,
		"timeoutSeconds":        15,
		"connectTimeoutSeconds": 10,
	}, nil)

	t.Run("aggregate", func(t *testing.T) {
		res := runSingleQuery(t, ds, queryModel{
			QueryType:  "aggregate",
			Collection: "metrics",
			QueryText:  `[{"$group": {"_id": "$host", "total": {"$sum": "$value"}}}, {"$sort": {"_id": 1}}]`,
		})
		if res.Error != nil {
			t.Fatalf("aggregate query failed: %v", res.Error)
		}
		if len(res.Frames) != 1 || res.Frames[0].Rows() != 2 {
			t.Fatalf("expected 1 frame with 2 rows, got %+v", res.Frames)
		}
	})

	t.Run("find", func(t *testing.T) {
		res := runSingleQuery(t, ds, queryModel{
			QueryType:  "find",
			Collection: "metrics",
			QueryText:  `{"host": "a"}`,
			Sort:       `{"value": 1}`,
		})
		if res.Error != nil {
			t.Fatalf("find query failed: %v", res.Error)
		}
		if res.Frames[0].Rows() != 2 {
			t.Fatalf("expected 2 rows, got %d", res.Frames[0].Rows())
		}
	})

	t.Run("count", func(t *testing.T) {
		res := runSingleQuery(t, ds, queryModel{
			QueryType:  "count",
			Collection: "metrics",
			QueryText:  `{}`,
		})
		if res.Error != nil {
			t.Fatalf("count query failed: %v", res.Error)
		}
		field := res.Frames[0].Fields[0]
		v, ok := field.ConcreteAt(0)
		if !ok || v.(int64) != 3 {
			t.Fatalf("count = %v, want 3", v)
		}
	})

	t.Run("distinct", func(t *testing.T) {
		res := runSingleQuery(t, ds, queryModel{
			QueryType:  "distinct",
			Collection: "metrics",
			QueryText:  `{}`,
			Field:      "host",
		})
		if res.Error != nil {
			t.Fatalf("distinct query failed: %v", res.Error)
		}
		if res.Frames[0].Rows() != 2 {
			t.Fatalf("expected 2 distinct values, got %d", res.Frames[0].Rows())
		}
	})

	t.Run("command", func(t *testing.T) {
		res := runSingleQuery(t, ds, queryModel{
			QueryType: "command",
			QueryText: `{"ping": 1}`,
		})
		if res.Error != nil {
			t.Fatalf("command query failed: %v", res.Error)
		}
		if res.Frames[0].Rows() != 1 {
			t.Fatalf("expected 1 row, got %d", res.Frames[0].Rows())
		}
	})
}

// TestIntegrationCheckHealth exercises CheckHealth against a real server.
func TestIntegrationCheckHealth(t *testing.T) {
	uri := startMongoContainer(t)
	ds := newDatasource(t, map[string]any{
		"connectionString":      uri,
		"database":              testDatabase,
		"timeoutSeconds":        15,
		"connectTimeoutSeconds": 10,
	}, nil)

	raw, err := json.Marshal(map[string]any{
		"connectionString":      uri,
		"database":              testDatabase,
		"timeoutSeconds":        15,
		"connectTimeoutSeconds": 10,
	})
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}

	res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{JSONData: raw},
		},
	})
	if err != nil {
		t.Fatalf("CheckHealth() error: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("CheckHealth() status = %v, message = %q, want Ok", res.Status, res.Message)
	}
}

// TestIntegrationAuthFailure exercises the auth-failure path: a datasource
// configured with the wrong password against a MongoDB server that requires
// authentication must surface an error from both CheckHealth and QueryData,
// never a silent success.
func TestIntegrationAuthFailure(t *testing.T) {
	ctx := context.Background()
	container, err := tcmongodb.Run(ctx, "mongo:7",
		tcmongodb.WithUsername("mongoadmin"),
		tcmongodb.WithPassword("correct-horse-battery-staple"),
	)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate mongodb container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "27017/tcp")
	if err != nil {
		t.Fatalf("failed to get container port: %v", err)
	}
	// Credentials are supplied separately (settings.Username /
	// secureJsonData.password) rather than embedded in the URI, mirroring
	// how the config editor collects them.
	bareURI := fmt.Sprintf("mongodb://%s:%s/?authSource=admin&connectTimeoutMS=3000&serverSelectionTimeoutMS=3000", host, port.Port())

	jsonData := map[string]any{
		"connectionString":      bareURI,
		"database":              testDatabase,
		"username":              "mongoadmin",
		"timeoutSeconds":        5,
		"connectTimeoutSeconds": 5,
	}

	t.Run("wrong password fails health check", func(t *testing.T) {
		ds := newDatasource(t, jsonData, map[string]string{"password": "wrong-password"})

		raw, err := json.Marshal(jsonData)
		if err != nil {
			t.Fatalf("failed to marshal settings: %v", err)
		}
		res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{
			PluginContext: backend.PluginContext{
				DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
					JSONData:                raw,
					DecryptedSecureJSONData: map[string]string{"password": "wrong-password"},
				},
			},
		})
		if err != nil {
			t.Fatalf("CheckHealth() error: %v", err)
		}
		if res.Status != backend.HealthStatusError {
			t.Fatalf("CheckHealth() status = %v, want Error", res.Status)
		}
	})

	t.Run("wrong password fails queries", func(t *testing.T) {
		ds := newDatasource(t, jsonData, map[string]string{"password": "wrong-password"})

		res := runSingleQuery(t, ds, queryModel{
			QueryType:  "find",
			Collection: "metrics",
			QueryText:  `{}`,
		})
		if res.Error == nil {
			t.Fatal("expected find query to fail with bad credentials, got nil error")
		}
	})

	t.Run("correct password succeeds", func(t *testing.T) {
		ds := newDatasource(t, jsonData, map[string]string{"password": "correct-horse-battery-staple"})

		res := runSingleQuery(t, ds, queryModel{
			QueryType:  "count",
			Collection: "metrics",
			QueryText:  `{}`,
		})
		if res.Error != nil {
			t.Fatalf("count query with correct credentials failed: %v", res.Error)
		}
	})
}
