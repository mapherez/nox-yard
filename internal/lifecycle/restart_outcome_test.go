package lifecycle

import "testing"

func TestSelfRestartPreservesGroupFailure(t *testing.T) {
	for _, test := range []struct {
		payload         map[string]string
		status, outcome string
	}{
		{map[string]string{}, "succeeded", "verified"},
		{map[string]string{"partialFailure": "true"}, "failed", "failed"},
		{map[string]string{"partialFailure": "true", "interrupted": "true"}, "failed", "recovery_required"},
	} {
		status, outcome, _ := restartOutcome(test.payload)
		if status != test.status || outcome != test.outcome {
			t.Fatalf("group result: %s %s", status, outcome)
		}
	}
}
