package plugin

import (
	"errors"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// MongoDB server error codes relevant to classifyQueryError. See
// https://github.com/mongodb/mongo/blob/master/src/mongo/base/error_codes.yml
const (
	mongoErrCodeUnauthorized         = 13
	mongoErrCodeAuthenticationFailed = 18
)

// classifyQueryError maps a query execution error to a backend.Status so
// the panel inspector and alerting can distinguish a bad query from a
// genuine outage, replacing the previous "message contains 'invalid'"
// heuristic.
func classifyQueryError(err error) backend.Status {
	if cmdErr, ok := errors.AsType[mongo.CommandError](err); ok {
		if cmdErr.HasErrorCode(mongoErrCodeUnauthorized) || cmdErr.HasErrorCode(mongoErrCodeAuthenticationFailed) {
			return backend.StatusUnauthorized
		}
		// MongoDB parsed and rejected the request itself (unknown operator,
		// invalid filter, bad type, ...) -- the caller's fault, not the
		// plugin's or the server's.
		return backend.StatusBadRequest
	}
	if _, ok := errors.AsType[mongo.WriteException](err); ok {
		return backend.StatusBadRequest
	}
	if mongo.IsTimeout(err) {
		return backend.StatusTimeout
	}
	if mongo.IsNetworkError(err) {
		return backend.StatusBadGateway
	}
	// Anything else (invalid extended JSON, a missing required field, an
	// operator/command blocked by safety settings) never reached MongoDB at
	// all, so it's the caller's fault too.
	return backend.StatusBadRequest
}
