package agent

import "testing"

func TestReferenceMethodRequiresCanonicalImage(t *testing.T) {
	req := ExpressionReferenceRequest{Expression: "happy"}
	if len(req.CanonicalImage) != 0 {
		t.Fatal("test precondition: canonical image should be empty")
	}
}
