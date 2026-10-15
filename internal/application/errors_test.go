package application

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/managed"
)

func TestUnsupportedManagedComparisonIsNotInvalidCompose(t *testing.T) {
	err := fmt.Errorf("service web: %w: security_opt", managed.ErrUnsupportedComparison)
	status, problem := ClassifyError(err, false)
	if status != 400 || problem.Code != "UNSUPPORTED_CONFIGURATION" || !strings.Contains(problem.Message, "security_opt") || strings.Contains(problem.Message, "invalid Compose source") {
		t.Fatalf("unsupported comparison misclassified: %d %+v", status, problem)
	}
	status, problem = ClassifyError(fmt.Errorf("%w: invalid logging settings", managed.ErrInvalidSource), false)
	if status != 400 || problem.Code != "INVALID_PAYLOAD" {
		t.Fatalf("malformed Compose misclassified: %d %+v", status, problem)
	}
}
