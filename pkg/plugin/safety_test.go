package plugin

import (
	"testing"

	"github.com/alandave/mongo-db/pkg/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestOperatorGuardBlocksDefaultDenylistInPipeline(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{})
	pipeline, err := parsePipeline(`[{"$match": {"a": 1}}, {"$out": "other"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.checkPipeline(pipeline); err == nil {
		t.Error("expected $out to be blocked")
	}
}

func TestOperatorGuardBlocksNestedOperator(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{})
	pipeline, err := parsePipeline(`[{"$group": {"_id": null, "total": {"$accumulator": {}}}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.checkPipeline(pipeline); err == nil {
		t.Error("expected nested $accumulator to be blocked")
	}
}

func TestOperatorGuardAllowsOrdinaryPipeline(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{})
	pipeline, err := parsePipeline(`[{"$match": {"a": 1}}, {"$group": {"_id": "$b", "total": {"$sum": "$c"}}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.checkPipeline(pipeline); err != nil {
		t.Errorf("unexpected error for ordinary pipeline: %v", err)
	}
}

func TestOperatorGuardBlocksFilterOperator(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{})
	filter, err := parseDocument(`{"$where": "this.a == this.b"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.checkDoc(filter); err == nil {
		t.Error("expected $where to be blocked")
	}
}

func TestOperatorGuardExtraBlockedOperator(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{BlockedOperators: []string{"$lookup"}})
	pipeline, err := parsePipeline(`[{"$lookup": {"from": "other", "localField": "a", "foreignField": "b", "as": "joined"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.checkPipeline(pipeline); err == nil {
		t.Error("expected extra-configured $lookup to be blocked")
	}
}

func TestOperatorGuardModeOffDisablesChecks(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{OperatorSafetyMode: "off"})
	pipeline, err := parsePipeline(`[{"$out": "other"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.checkPipeline(pipeline); err != nil {
		t.Errorf("expected $out to be allowed when safety mode is off, got: %v", err)
	}
}

func TestOperatorGuardBlocksDefaultCommand(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{})
	if err := guard.checkCommand(bson.D{{Key: "dropDatabase", Value: 1}}); err == nil {
		t.Error("expected dropDatabase command to be blocked")
	}
}

func TestOperatorGuardAllowsOrdinaryCommand(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{})
	if err := guard.checkCommand(bson.D{{Key: "dbStats", Value: 1}}); err != nil {
		t.Errorf("unexpected error for ordinary command: %v", err)
	}
}

func TestOperatorGuardExtraBlockedCommand(t *testing.T) {
	guard := newOperatorGuard(&models.PluginSettings{BlockedCommands: []string{"collMod"}})
	if err := guard.checkCommand(bson.D{{Key: "collMod", Value: "coll"}}); err == nil {
		t.Error("expected extra-configured collMod command to be blocked")
	}
}

func TestApplyAggregatePagingNoQueryLimitFallsBackToMaxDocuments(t *testing.T) {
	pipeline := []bson.D{{{Key: "$match", Value: bson.D{}}}}
	out := applyAggregatePaging(pipeline, 0, 0, 5000)
	if len(out) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(out))
	}
	if out[1][0].Key != "$limit" || out[1][0].Value != int64(5000) {
		t.Errorf("expected trailing $limit stage of 5000, got %+v", out[1])
	}
}

func TestApplyAggregatePagingDisabledLeavesPipelineUnchanged(t *testing.T) {
	pipeline := []bson.D{{{Key: "$match", Value: bson.D{}}}}
	out := applyAggregatePaging(pipeline, 0, 0, -1)
	if len(out) != 1 {
		t.Fatalf("expected pipeline unchanged, got %d stages", len(out))
	}
}

func TestApplyAggregatePagingAppendsSkipAndLimit(t *testing.T) {
	pipeline := []bson.D{{{Key: "$match", Value: bson.D{}}}}
	out := applyAggregatePaging(pipeline, 20, 10, 5000)
	if len(out) != 3 {
		t.Fatalf("expected 3 stages, got %+v", out)
	}
	if out[1][0].Key != "$skip" || out[1][0].Value != int64(20) {
		t.Errorf("expected $skip stage of 20, got %+v", out[1])
	}
	if out[2][0].Key != "$limit" || out[2][0].Value != int64(10) {
		t.Errorf("expected $limit stage of 10, got %+v", out[2])
	}
}

func TestApplyAggregatePagingQueryLimitClampedByMaxDocuments(t *testing.T) {
	pipeline := []bson.D{{{Key: "$match", Value: bson.D{}}}}
	out := applyAggregatePaging(pipeline, 0, 50000, 5000)
	if len(out) != 2 {
		t.Fatalf("expected 2 stages, got %+v", out)
	}
	if out[1][0].Key != "$limit" || out[1][0].Value != int64(5000) {
		t.Errorf("expected $limit stage clamped to 5000, got %+v", out[1])
	}
}

func TestResolveFindLimit(t *testing.T) {
	cases := []struct {
		name                     string
		queryLimit, maxDocuments int64
		want                     int64
	}{
		{"no query limit falls back to maxDocuments", 0, 10000, 10000},
		{"query limit under cap wins", 100, 10000, 100},
		{"query limit over cap is clamped", 50000, 10000, 10000},
		{"guard disabled leaves query limit alone", 50000, -1, 50000},
		{"guard disabled with no query limit stays unlimited", 0, -1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveFindLimit(tc.queryLimit, tc.maxDocuments); got != tc.want {
				t.Errorf("resolveFindLimit(%d, %d) = %d, want %d", tc.queryLimit, tc.maxDocuments, got, tc.want)
			}
		})
	}
}
