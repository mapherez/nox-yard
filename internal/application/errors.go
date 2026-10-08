package application

import (
	"context"
	"errors"
	"net"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/client"
)

type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ClassifyError(err error, execution bool) (int, Problem) {
	var storage ManagedReadError
	var network net.Error
	switch {
	case errors.Is(err, ErrScheduleUnsupported):
		return 400, Problem{Code: "UNSUPPORTED_CONFIGURATION", Message: err.Error()}
	case errors.Is(err, recreate.ErrUnsupported):
		return 400, Problem{Code: "UNSUPPORTED_CONFIGURATION", Message: err.Error()}
	case errors.Is(err, recreate.ErrInvalid):
		return 400, Problem{Code: "INVALID_PAYLOAD", Message: err.Error()}
	case errors.As(err, &storage):
		return 500, Problem{Code: "INTERNAL_ERROR", Message: "Unable to read stored project metadata."}
	case errors.Is(err, context.DeadlineExceeded), errdefs.IsDeadlineExceeded(err):
		return 504, Problem{Code: "OPERATION_TIMEOUT", Message: "The operation timed out."}
	case errors.Is(err, context.Canceled), errdefs.IsCanceled(err):
		return 502, Problem{Code: "OPERATION_FAILED", Message: "The operation was canceled."}
	case errors.Is(err, inventory.ErrContainerNotFound), errors.Is(err, lifecycle.ErrNotFound):
		return 404, Problem{Code: "TARGET_NOT_FOUND", Message: "Target does not exist."}
	case errors.Is(err, lifecycle.ErrProtected):
		return 409, Problem{Code: "TARGET_PROTECTED", Message: "This target is protected from the requested operation."}
	case errors.Is(err, store.ErrOperationConflict), errors.Is(err, store.ErrJobChanged), errors.Is(err, lifecycle.ErrChanged), errors.Is(err, lifecycle.ErrConflict), errdefs.IsConflict(err), errdefs.IsAlreadyExists(err):
		return 409, Problem{Code: "OPERATION_CONFLICT", Message: "The operation conflicts with the current target state."}
	case errors.Is(err, lifecycle.ErrInvalidAction):
		return 400, Problem{Code: "INVALID_PAYLOAD", Message: "Action must be start, stop, or restart."}
	case errors.As(err, &network) && network.Timeout():
		return 504, Problem{Code: "OPERATION_TIMEOUT", Message: "The operation timed out."}
	case errors.Is(err, ErrDockerUnavailable), client.IsErrConnectionFailed(err), errdefs.IsUnavailable(err), errdefs.IsPermissionDenied(err), errdefs.IsUnauthorized(err):
		return 503, Problem{Code: "DOCKER_UNAVAILABLE", Message: "The local Docker Engine is unavailable."}
	case errdefs.IsNotFound(err), errdefs.IsInvalidArgument(err), errdefs.IsFailedPrecondition(err), errdefs.IsNotImplemented(err), errdefs.IsInternal(err), errdefs.IsDataLoss(err), errdefs.IsResourceExhausted(err):
		return 502, Problem{Code: "OPERATION_FAILED", Message: "The Docker request could not be completed."}
	case errors.Is(err, managed.ErrConflict), errors.Is(err, selfupdate.ErrUpdateInProgress), errors.Is(err, selfupdate.ErrCheckInProgress):
		return 409, Problem{Code: "OPERATION_CONFLICT", Message: err.Error()}
	case errors.Is(err, ErrInvalidRemoval), errors.Is(err, managed.ErrInvalidSource), errors.Is(err, selfupdate.ErrInvalidInterval):
		return 400, Problem{Code: "INVALID_PAYLOAD", Message: err.Error()}
	case errors.Is(err, ErrUnavailable):
		return 503, Problem{Code: "CAPABILITY_UNAVAILABLE", Message: err.Error()}
	case execution:
		return 502, Problem{Code: "OPERATION_FAILED", Message: "The operation could not be completed."}
	default:
		return 500, Problem{Code: "INTERNAL_ERROR", Message: "An unexpected internal error occurred."}
	}
}

func OperationProblem(ctx context.Context, err error, failed, succeeded, skipped, queued int, failures []lifecycle.Failure) (int, *Problem) {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	for _, failure := range failures {
		status, _ := ClassifyError(failure.Cause, true)
		if status == 504 {
			err = context.DeadlineExceeded
			break
		}
		if err == nil && (errors.Is(failure.Cause, context.Canceled) || errdefs.IsCanceled(failure.Cause)) {
			err = context.Canceled
		}
	}
	if err != nil {
		status, problem := ClassifyError(err, false)
		return status, &problem
	}
	if failed > 0 || len(failures) > 0 {
		status, problem := 502, Problem{Code: "OPERATION_FAILED", Message: "The operation could not be completed."}
		if succeeded+skipped+queued > 0 {
			problem = Problem{Code: "OPERATION_PARTIAL_FAILURE", Message: "The operation completed only partially."}
		} else if len(failures) > 0 {
			status, problem = ClassifyError(failures[0].Cause, true)
			for _, failure := range failures[1:] {
				_, next := ClassifyError(failure.Cause, true)
				if problem.Code != next.Code {
					status, problem = 502, Problem{Code: "OPERATION_FAILED", Message: "The operation could not be completed."}
					break
				}
			}
		}
		return status, &problem
	}
	if queued > 0 {
		return 202, nil
	}
	return 200, nil
}
