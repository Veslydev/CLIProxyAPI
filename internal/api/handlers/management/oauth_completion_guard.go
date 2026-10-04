package management

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type oauthCompletionGuardKey struct{}
type oauthCompletionGuard struct {
	validate        func(*coreauth.Auth) error
	state, provider string
	expires         time.Time
	used            atomic.Bool
}

// SetOAuthCompletionValidator affects only this launch; unrelated sessions and
// legacy management routes retain their existing completion behavior.
func SetOAuthCompletionValidator(c *gin.Context, validate func(*coreauth.Auth) error) {
	c.Set("controlPlaneOAuthValidator", validate)
}

func bindOAuthCompletionGuard(ctx context.Context, c *gin.Context, state, provider string) context.Context {
	value, ok := c.Get("controlPlaneOAuthValidator")
	if !ok {
		return ctx
	}
	validate, ok := value.(func(*coreauth.Auth) error)
	if !ok || validate == nil {
		return ctx
	}
	return context.WithValue(ctx, oauthCompletionGuardKey{}, &oauthCompletionGuard{validate: validate, state: state, provider: provider, expires: time.Now().Add(5 * time.Minute)})
}

func validateOAuthCompletion(ctx context.Context, record *coreauth.Auth) error {
	guard, _ := ctx.Value(oauthCompletionGuardKey{}).(*oauthCompletionGuard)
	if guard == nil {
		return nil
	}
	if !time.Now().Before(guard.expires) || !guard.used.CompareAndSwap(false, true) {
		return errors.New("re-auth completion expired or already consumed")
	}
	// Atomically linearize completion against cancellation. Once saving begins,
	// cancellation must report failure rather than promise a rollback of OAuth.
	if !oauthSessions.claimSave(guard.state, guard.provider) {
		return errOAuthSessionNotPending
	}
	return guard.validate(record)
}
