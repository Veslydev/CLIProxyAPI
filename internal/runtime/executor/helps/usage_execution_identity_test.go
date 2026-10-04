package helps

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestUsageReporterUsesLeaseExecutionIdentity(t *testing.T) {
	ctx := usage.WithExecutionRequestID(context.Background(), "fixture-lease-execution")
	reporter := NewUsageReporter(ctx, "codex", "fixture-model", nil)
	if reporter.RequestID() != "fixture-lease-execution" {
		t.Fatal("reporter identity does not match the routing lease evidence")
	}
	additional, ok := reporter.buildAdditionalModelRecord("fixture-image", usage.Detail{TotalTokens: 3})
	if !ok || additional.RequestID == reporter.RequestID() {
		t.Fatal("additional model must retain its distinct settlement identity")
	}
}
