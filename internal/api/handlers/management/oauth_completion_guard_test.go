package management

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func completionContext(t *testing.T, state string, calls *atomic.Int64) context.Context {
	t.Helper()
	RegisterOAuthSession(state, "codex")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetOAuthCompletionValidator(c, func(*coreauth.Auth) error { calls.Add(1); return nil })
	t.Cleanup(func() { CompleteOAuthSession(state) })
	return bindOAuthCompletionGuard(context.Background(), c, state, "codex")
}

func TestCompletionGuardConcurrentReplayAndIndependentSessions(t *testing.T) {
	var calls atomic.Int64
	ctx := completionContext(t, "guard-one", &calls)
	other := completionContext(t, "guard-two", &calls)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = validateOAuthCompletion(ctx, &coreauth.Auth{}) }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("one-time completion replayed")
	}
	if CancelOAuthSession("guard-one") {
		t.Fatal("cancellation falsely promised rollback after completion began")
	}
	if err := validateOAuthCompletion(other, &coreauth.Auth{}); err != nil || calls.Load() != 2 {
		t.Fatal("one session consumed another")
	}
}

func TestCompletionGuardCancelExpiryAndWrongState(t *testing.T) {
	for _, mode := range []string{"cancel", "expire", "wrong_state", "wrong_provider"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			ctx := completionContext(t, "guard-"+mode, &calls)
			g := ctx.Value(oauthCompletionGuardKey{}).(*oauthCompletionGuard)
			switch mode {
			case "cancel":
				CancelOAuthSession(g.state)
			case "expire":
				g.expires = time.Now().Add(-time.Second)
			case "wrong_state":
				g.state = "not-registered"
			case "wrong_provider":
				g.provider = "anthropic"
			}
			if err := validateOAuthCompletion(ctx, &coreauth.Auth{}); err == nil || calls.Load() != 0 {
				t.Fatal("invalid session reached persistence validator")
			}
		})
	}
}
