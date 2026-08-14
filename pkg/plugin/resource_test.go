package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/alandave/mongo-db/pkg/models"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestCollectionFilterNoPatternsAllowsEverything(t *testing.T) {
	f := newCollectionFilter(nil)
	if !f.databaseAllowed("sampledb") {
		t.Error("expected database to be allowed with no patterns")
	}
	if !f.collectionAllowed("sampledb", "orders") {
		t.Error("expected collection to be allowed with no patterns")
	}
}

func TestCollectionFilterAllowList(t *testing.T) {
	f := newCollectionFilter([]string{"sampledb.*"})

	if !f.databaseAllowed("sampledb") {
		t.Error("expected sampledb to be allowed")
	}
	if f.databaseAllowed("other") {
		t.Error("expected other database to be denied")
	}
	if !f.collectionAllowed("sampledb", "orders") {
		t.Error("expected sampledb.orders to be allowed")
	}
	if f.collectionAllowed("other", "orders") {
		t.Error("expected other.orders to be denied")
	}
}

func TestCollectionFilterDenyWinsOverAllow(t *testing.T) {
	f := newCollectionFilter([]string{"sampledb.*", "!sampledb.system.*", "!*_internal"})

	if !f.collectionAllowed("sampledb", "orders") {
		t.Error("expected sampledb.orders to be allowed")
	}
	if f.collectionAllowed("sampledb", "system.indexes") {
		t.Error("expected sampledb.system.indexes to be denied")
	}
	if f.collectionAllowed("sampledb", "orders_internal") {
		t.Error("expected sampledb.orders_internal to be denied")
	}
}

func TestCollectionFilterDenyOnlyStillAllowsUnmatched(t *testing.T) {
	f := newCollectionFilter([]string{"!*.system.*"})

	if !f.collectionAllowed("sampledb", "orders") {
		t.Error("expected sampledb.orders to be allowed when only deny patterns are set")
	}
	if f.collectionAllowed("sampledb", "system.indexes") {
		t.Error("expected sampledb.system.indexes to be denied")
	}
}

type fakeResourceSender struct {
	resp *backend.CallResourceResponse
}

func (s *fakeResourceSender) Send(resp *backend.CallResourceResponse) error {
	s.resp = resp
	return nil
}

func TestCallResourceReturnsNotImplementedWhenDiscoveryDisabled(t *testing.T) {
	d := &Datasource{settings: &models.PluginSettings{SchemaDiscoveryEnabled: false}}
	sender := &fakeResourceSender{}

	err := d.CallResource(context.Background(), &backend.CallResourceRequest{Path: "databases", URL: "/databases"}, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp.Status != http.StatusNotImplemented {
		t.Errorf("expected status %d, got %d", http.StatusNotImplemented, sender.resp.Status)
	}

	var body map[string]string
	if err := json.Unmarshal(sender.resp.Body, &body); err != nil {
		t.Fatalf("could not unmarshal response body: %v", err)
	}
	if body["message"] == "" {
		t.Error("expected a message explaining discovery is disabled")
	}
}

func TestCallResourceUnknownPath(t *testing.T) {
	d := &Datasource{settings: &models.PluginSettings{SchemaDiscoveryEnabled: true}}
	sender := &fakeResourceSender{}

	err := d.CallResource(context.Background(), &backend.CallResourceRequest{Path: "nope", URL: "/nope"}, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp.Status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, sender.resp.Status)
	}
}

// explainRequestJSON marshals an explainRequest to a resource request body,
// failing the test on error rather than returning one, since every caller
// here is constructing a fixed, known-good literal.
func explainRequestJSON(t *testing.T, er explainRequest) []byte {
	t.Helper()
	body, err := json.Marshal(er)
	if err != nil {
		t.Fatalf("marshal explainRequest: %v", err)
	}
	return body
}

func TestHandleExplainWorksEvenWhenSchemaDiscoveryDisabled(t *testing.T) {
	// Explain doesn't list databases/collections/fields, so it isn't gated
	// behind SchemaDiscoveryEnabled the way the other resource endpoints are.
	// This case fails past that gate (missing collection) rather than
	// reaching the Mongo client, so it doesn't need a real connection.
	d := &Datasource{settings: &models.PluginSettings{SchemaDiscoveryEnabled: false, Database: "sampledb"}}
	sender := &fakeResourceSender{}

	body := explainRequestJSON(t, explainRequest{QueryType: "find", Database: "sampledb", QueryText: "{}"})
	err := d.CallResource(context.Background(), &backend.CallResourceRequest{Path: "explain", URL: "/explain", Body: body}, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp.Status != http.StatusBadRequest {
		t.Errorf("expected status %d for missing collection, got %d", http.StatusBadRequest, sender.resp.Status)
	}
}

func TestHandleExplainRejectsUnsupportedQueryType(t *testing.T) {
	d := &Datasource{settings: &models.PluginSettings{Database: "sampledb"}}
	sender := &fakeResourceSender{}

	body := explainRequestJSON(t, explainRequest{QueryType: "distinct", Database: "sampledb", Collection: "metrics"})
	err := d.CallResource(context.Background(), &backend.CallResourceRequest{Path: "explain", URL: "/explain", Body: body}, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp.Status != http.StatusBadRequest {
		t.Errorf("expected status %d for unsupported query type, got %d", http.StatusBadRequest, sender.resp.Status)
	}
}

func TestHandleExplainRejectsBlockedOperator(t *testing.T) {
	d := &Datasource{settings: &models.PluginSettings{Database: "sampledb"}}
	sender := &fakeResourceSender{}

	body := explainRequestJSON(t, explainRequest{
		QueryType:  "aggregate",
		Database:   "sampledb",
		Collection: "metrics",
		QueryText:  `[{"$out": "other"}]`,
	})
	err := d.CallResource(context.Background(), &backend.CallResourceRequest{Path: "explain", URL: "/explain", Body: body}, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp.Status != http.StatusForbidden {
		t.Errorf("expected status %d for blocked operator, got %d", http.StatusForbidden, sender.resp.Status)
	}
}

func TestHandleExplainRejectsInvalidQueryText(t *testing.T) {
	d := &Datasource{settings: &models.PluginSettings{Database: "sampledb"}}
	sender := &fakeResourceSender{}

	body := explainRequestJSON(t, explainRequest{QueryType: "find", Database: "sampledb", Collection: "metrics", QueryText: "not json"})
	err := d.CallResource(context.Background(), &backend.CallResourceRequest{Path: "explain", URL: "/explain", Body: body}, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp.Status != http.StatusBadRequest {
		t.Errorf("expected status %d for invalid query text, got %d", http.StatusBadRequest, sender.resp.Status)
	}
}
