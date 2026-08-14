package plugin

import (
	"fmt"

	"github.com/alandave/mongo-db/pkg/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// defaultBlockedOperators are pipeline/filter operator keys blocked
// regardless of settings.BlockedOperators, unless OperatorSafetyMode is
// "off": $out/$merge can turn a nominally read-only datasource into a write
// path, and $where/$function/$accumulator execute arbitrary server-side
// JavaScript.
var defaultBlockedOperators = []string{"$out", "$merge", "$where", "$function", "$accumulator"}

// defaultBlockedCommands are "command" query type top-level command names
// blocked regardless of settings.BlockedCommands, unless OperatorSafetyMode
// is "off".
var defaultBlockedCommands = []string{"dropDatabase", "shutdown", "eval"}

// operatorGuard evaluates parsed pipelines, filters and commands against a
// datasource's operator safety settings before they reach MongoDB.
type operatorGuard struct {
	blockedOperators map[string]bool
	blockedCommands  map[string]bool
}

// newOperatorGuard builds a guard from settings. When OperatorSafetyMode is
// "off" the returned guard blocks nothing.
func newOperatorGuard(settings *models.PluginSettings) *operatorGuard {
	g := &operatorGuard{blockedOperators: map[string]bool{}, blockedCommands: map[string]bool{}}
	if settings.OperatorSafetyMode == "off" {
		return g
	}
	for _, op := range defaultBlockedOperators {
		g.blockedOperators[op] = true
	}
	for _, op := range settings.BlockedOperators {
		g.blockedOperators[op] = true
	}
	for _, cmd := range defaultBlockedCommands {
		g.blockedCommands[cmd] = true
	}
	for _, cmd := range settings.BlockedCommands {
		g.blockedCommands[cmd] = true
	}
	return g
}

// checkPipeline recursively walks every stage of an aggregation pipeline for
// blocked operator keys.
func (g *operatorGuard) checkPipeline(stages []bson.D) error {
	for _, stage := range stages {
		if err := g.checkDoc(stage); err != nil {
			return err
		}
	}
	return nil
}

// checkDoc recursively walks a filter/pipeline-stage document (and any
// nested documents/arrays) for blocked operator keys, e.g. $where inside a
// find filter or $function nested inside a $group accumulator.
func (g *operatorGuard) checkDoc(doc bson.D) error {
	for _, e := range doc {
		if len(e.Key) > 0 && e.Key[0] == '$' && g.blockedOperators[e.Key] {
			return fmt.Errorf("operator %q is not permitted by this datasource's safety settings", e.Key)
		}
		if err := g.checkValue(e.Value); err != nil {
			return err
		}
	}
	return nil
}

func (g *operatorGuard) checkValue(v any) error {
	switch val := v.(type) {
	case bson.D:
		return g.checkDoc(val)
	case bson.A:
		for _, item := range val {
			if err := g.checkValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyMaxDocumentsLimit appends a trailing $limit stage to an aggregation
// pipeline when maxDocuments is positive, so a careless match-only pipeline
// gets bounded instead of materializing the whole collection. Any stage
// already limiting below maxDocuments stays authoritative -- the appended
// stage is a cap, not an override. maxDocuments <= 0 leaves the pipeline
// unchanged (the guard is disabled).
func applyMaxDocumentsLimit(pipeline []bson.D, maxDocuments int64) []bson.D {
	if maxDocuments <= 0 {
		return pipeline
	}
	return append(pipeline, bson.D{{Key: "$limit", Value: maxDocuments}})
}

// resolveFindLimit combines a query's own "find" limit with the datasource's
// maxDocuments guard: the smaller of the two positive values wins, and an
// unset query limit (<= 0) falls back to maxDocuments. maxDocuments <= 0
// disables the guard, leaving the query's own limit (possibly none) as-is.
func resolveFindLimit(queryLimit, maxDocuments int64) int64 {
	if maxDocuments > 0 && (queryLimit <= 0 || queryLimit > maxDocuments) {
		return maxDocuments
	}
	return queryLimit
}

// checkCommand validates a "command" query type's top-level command name
// (its first key, e.g. "dropDatabase" in {"dropDatabase": 1}) against the
// blocked commands list, then walks the rest of the document for blocked
// operators.
func (g *operatorGuard) checkCommand(cmd bson.D) error {
	if len(cmd) == 0 {
		return nil
	}
	name := cmd[0].Key
	if g.blockedCommands[name] {
		return fmt.Errorf("command %q is not permitted by this datasource's safety settings", name)
	}
	return g.checkDoc(cmd)
}
