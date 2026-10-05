package httpapi

import (
	"context"
	"net/http"

	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

func writeControlError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, controlError{Code: code, Message: message})
}
func classifyControlError(err error, execution bool) (int, controlError) {
	status, p := application.ClassifyError(err, execution)
	return status, controlError{Code: p.Code, Message: p.Message}
}
func writeControlCause(w http.ResponseWriter, err error) {
	status, p := classifyControlError(err, false)
	writeJSON(w, status, p)
}
func writeControlResult(w http.ResponseWriter, ctx context.Context, err error, result any, failed, succeeded, skipped, queued int, failures []lifecycle.Failure) {
	status, p := application.OperationProblem(ctx, err, failed, succeeded, skipped, queued, failures)
	if p != nil {
		writeJSON(w, status, controlError{Code: p.Code, Message: p.Message, Result: result})
		return
	}
	writeJSON(w, status, result)
}
