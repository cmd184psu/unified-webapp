package opencode

import (
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// opencode 2.x error events carry type/message/status at the top of "error";
// a daily-allowance 429 must be a rate limit, not a crash (AF-7).
func TestOpencode2RateLimitEvent(t *testing.T) {
	var o outcome
	line := `{"type":"error","error":{"type":"provider.rate-limit","message":"Your organization has used its daily output token allowance","status":429}}`
	o.add([]byte(line))
	if !o.hasError || o.errorClass() != runner.ErrRateLimit || !strings.Contains(o.errMessage, "daily output token allowance") {
		t.Fatalf("hasError=%v class=%v msg=%q", o.hasError, o.errorClass(), o.errMessage)
	}
}

// The 1.x shape keeps working.
func TestOpencode1ErrorEvent(t *testing.T) {
	var o outcome
	o.add([]byte(`{"type":"error","error":{"name":"APIError","data":{"message":"slow down","statusCode":429}}}`))
	if o.errorClass() != runner.ErrRateLimit || o.errMessage != "slow down" {
		t.Fatalf("class=%v msg=%q", o.errorClass(), o.errMessage)
	}
}
