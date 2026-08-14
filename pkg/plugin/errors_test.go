package plugin

import (
	"fmt"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestClassifyQueryErrorAuthCommandError(t *testing.T) {
	err := mongo.CommandError{Code: mongoErrCodeUnauthorized, Message: "not authorized"}
	if got := classifyQueryError(err); got != backend.StatusUnauthorized {
		t.Errorf("classifyQueryError(unauthorized) = %v, want StatusUnauthorized", got)
	}

	err = mongo.CommandError{Code: mongoErrCodeAuthenticationFailed, Message: "auth failed"}
	if got := classifyQueryError(err); got != backend.StatusUnauthorized {
		t.Errorf("classifyQueryError(authentication failed) = %v, want StatusUnauthorized", got)
	}
}

func TestClassifyQueryErrorOtherCommandErrorIsBadRequest(t *testing.T) {
	err := mongo.CommandError{Code: 9, Name: "FailedToParse", Message: "unknown operator"}
	if got := classifyQueryError(err); got != backend.StatusBadRequest {
		t.Errorf("classifyQueryError(command error) = %v, want StatusBadRequest", got)
	}
}

func TestClassifyQueryErrorLocalValidationErrorIsBadRequest(t *testing.T) {
	err := fmt.Errorf("filter: %w", fmt.Errorf("invalid extended JSON document: unexpected token"))
	if got := classifyQueryError(err); got != backend.StatusBadRequest {
		t.Errorf("classifyQueryError(local validation error) = %v, want StatusBadRequest", got)
	}
}

func TestClassifyQueryErrorSafetyRejectionIsBadRequest(t *testing.T) {
	err := fmt.Errorf("operator %q is not permitted by this datasource's safety settings", "$out")
	if got := classifyQueryError(err); got != backend.StatusBadRequest {
		t.Errorf("classifyQueryError(safety rejection) = %v, want StatusBadRequest", got)
	}
}
