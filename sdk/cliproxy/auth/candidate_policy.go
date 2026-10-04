package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

// RequestAdmission authorizes once per logical invocation, before retries.
type RequestAdmission interface {
	AdmitRequest(context.Context, cliproxyexecutor.Request, cliproxyexecutor.Options) error
}
type ExecutionLease interface {
	BeginExecution(context.Context, *Auth, cliproxyexecutor.Options) func()
}

// ExecutionObserver associates each upstream attempt with routing evidence,
// independently of the lease retained across stream bootstrap/retry handling.
type ExecutionObserver interface {
	ObserveExecution(context.Context, *Auth, cliproxyexecutor.Options)
}

// ExecutionCompletionObserver closes attempt journals independently of the
// account lease, which is acquired before stream bootstrap creates attempt IDs.
type ExecutionCompletionObserver interface {
	FinishExecution(context.Context)
}

func (m *Manager) finishExecution(ctx context.Context) {
	if holder := m.candidatePolicy.Load(); holder != nil {
		if observer, ok := holder.policy.(ExecutionCompletionObserver); ok {
			observer.FinishExecution(ctx)
		}
	}
}

func (m *Manager) executionContext(ctx context.Context, a *Auth, opts cliproxyexecutor.Options) context.Context {
	if holder := m.candidatePolicy.Load(); holder != nil {
		if observer, ok := holder.policy.(ExecutionObserver); ok {
			ctx = usage.WithExecutionRequestID(ctx, uuid.NewString())
			observer.ObserveExecution(ctx, a, opts)
		}
	}
	return ctx
}

// AdmitRequest applies invocation limits to routes that bypass Execute.
// Call once before credential selection, not once per transport retry.
func (m *Manager) AdmitRequest(ctx context.Context, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
	return m.admitRequest(ctx, req, opts)
}

// BeginExecution tracks a selected raw HTTP execution until its response closes.
func (m *Manager) BeginExecution(ctx context.Context, a *Auth, opts cliproxyexecutor.Options) func() {
	return m.beginExecution(ctx, a, opts)
}

type policyRequestContextKey struct{}

// WithPolicyRequestContext carries opaque host authorization across handler
// contexts without coupling the SDK to a particular control-plane implementation.
func WithPolicyRequestContext(ctx context.Context, value any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, policyRequestContextKey{}, value)
}

func PolicyRequestContext(ctx context.Context) any {
	if ctx == nil {
		return nil
	}
	return ctx.Value(policyRequestContextKey{})
}

func CopyPolicyRequestContext(dst, src context.Context) context.Context {
	if value := PolicyRequestContext(src); value != nil {
		return WithPolicyRequestContext(dst, value)
	}
	return dst
}

func (m *Manager) admitRequest(ctx context.Context, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
	if holder := m.candidatePolicy.Load(); holder != nil {
		if m.HomeEnabled() {
			return &Error{Code: "control_plane_home_conflict", Message: "local candidate policy cannot authorize Home dispatch", HTTPStatus: http.StatusServiceUnavailable}
		}
		if admission, ok := holder.policy.(RequestAdmission); ok {
			return admission.AdmitRequest(ctx, req, opts)
		}
	}
	return nil
}

func (m *Manager) beginExecution(ctx context.Context, a *Auth, opts cliproxyexecutor.Options) func() {
	if holder := m.candidatePolicy.Load(); holder != nil {
		if lease, ok := holder.policy.(ExecutionLease); ok {
			return lease.BeginExecution(ctx, a, opts)
		}
	}
	return func() {}
}

func (m *Manager) executeWithLease(ctx context.Context, exec ProviderExecutor, a *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	ctx = m.executionContext(ctx, a, opts)
	release := m.beginExecution(ctx, a, opts)
	defer release()
	return exec.Execute(ctx, a, req, opts)
}

// ResponseObserver persists account-bound object ownership before forwarding it.
type ResponseObserver interface {
	ObserveResponse(context.Context, *Auth, cliproxyexecutor.Options, []byte) error
}

func (m *Manager) observeResponse(ctx context.Context, a *Auth, opts cliproxyexecutor.Options, payload []byte) error {
	if holder := m.candidatePolicy.Load(); holder != nil {
		if observer, ok := holder.policy.(ResponseObserver); ok {
			return observer.ObserveResponse(ctx, a, opts, payload)
		}
	}
	return nil
}

// SetCandidatePolicy installs a policy independent of selector/config reloads.
func (m *Manager) SetCandidatePolicy(policy CandidatePolicy) {
	if policy == nil {
		m.candidatePolicy.Store(nil)
		return
	}
	m.candidatePolicy.Store(&candidatePolicyHolder{policy: policy})
}

func (m *Manager) filterCandidates(ctx context.Context, model string, opts cliproxyexecutor.Options, candidates []*Auth) ([]*Auth, error) {
	holder := m.candidatePolicy.Load()
	if holder == nil {
		return candidates, nil
	}
	metadata := make(map[string]any, len(opts.Metadata)+1)
	for key, value := range opts.Metadata {
		metadata[key] = value
	}
	requestedProviders, _ := opts.Metadata[cliproxyexecutor.CandidateProvidersMetadataKey].([]string)
	providers := append([]string(nil), requestedProviders...)
	for _, a := range candidates {
		providers = append(providers, a.Provider)
	}
	metadata[cliproxyexecutor.CandidateProvidersMetadataKey] = providers
	opts.Metadata = metadata
	ready := make([]*Auth, 0, len(candidates))
	for _, a := range candidates {
		if blocked, _, _ := isAuthBlockedForModel(a, m.selectionModelForAuth(a, model), time.Now()); !blocked {
			ready = append(ready, a)
		}
	}
	filtered, err := holder.policy.FilterCandidates(ctx, model, opts, ready)
	if err != nil {
		return nil, err
	}
	// A host policy may reduce eligibility, never introduce another credential.
	allowed := make(map[string]*Auth, len(candidates))
	for _, a := range candidates {
		allowed[a.ID] = a
	}
	out := make([]*Auth, 0, len(filtered))
	for _, a := range filtered {
		if a != nil {
			if original := allowed[a.ID]; original != nil {
				out = append(out, original)
			}
		}
	}
	return out, nil
}
