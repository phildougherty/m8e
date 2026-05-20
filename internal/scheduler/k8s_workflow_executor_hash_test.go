// internal/scheduler/k8s_workflow_executor_hash_test.go
package scheduler

import (
	"testing"
)

// TestShortHash_SHA256Truncated verifies the workflow-name truncator returns
// a deterministic 8-char hex prefix backed by SHA-256 (the implementation
// previously used MD5 and was flagged by gosec).
func TestShortHash_SHA256Truncated(t *testing.T) {
	a := shortHash("workflow-with-a-very-long-name")
	b := shortHash("workflow-with-a-very-long-name")
	if a != b {
		t.Errorf("shortHash not deterministic: %q vs %q", a, b)
	}
	if len(a) != 8 {
		t.Errorf("expected 8-char short hash, got %d: %q", len(a), a)
	}
	if shortHash("alpha") == shortHash("beta") {
		t.Errorf("distinct inputs produced identical short hashes (unexpected collision)")
	}
}
