package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/controlplane"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type controlPlaneLoadExecutor struct {
	controlPlaneHTTPExecutor
	gate <-chan struct{}
}

func TestControlPlaneWebsocketReconnectKeepsScopeAndRejectsUnreplayableContinuation(t *testing.T) {
	s, _ := controlPlaneHTTPFixture(t)
	stateRequest := httptest.NewRequest("GET", "/api/control-plane/state", nil)
	stateRequest.Header.Set("Authorization", "Bearer "+os.Getenv("CP_TEST_ADMIN"))
	stateResponse := httptest.NewRecorder()
	s.engine.ServeHTTP(stateResponse, stateRequest)
	var state struct{ Accounts []controlplane.Account }
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	var account string
	for _, a := range state.Accounts {
		if a.Label == "http-a" {
			account = a.ID
		}
	}
	_, secret, err := s.controlPlane.CreateKey(controlplane.APIKey{Name: "ws-fixture", Bindings: map[string]controlplane.Binding{"codex": {Accounts: []string{account}}}})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	close(gate)
	e := &controlPlaneLoadExecutor{gate: gate}
	s.handlers.AuthManager.RegisterExecutor(e)
	server := httptest.NewServer(s.engine)
	defer server.Close()
	var responseID string
	for turn := 0; turn < 3; turn++ {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", http.Header{"Authorization": []string{"Bearer " + secret}})
		if err != nil {
			t.Fatal(err)
		}
		request := map[string]any{"type": "response.create", "model": "gpt-test", "input": []any{}}
		if turn == 2 {
			request["previous_response_id"] = responseID
		}
		if err = conn.WriteJSON(request); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		terminal := false
		for frames := 0; frames < 8; frames++ {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				_ = conn.Close()
				t.Fatal(err)
			}
			var event struct {
				Type   string `json:"type"`
				Status int    `json:"status"`
				Error  struct {
					Code string `json:"code"`
				} `json:"error"`
				Response struct {
					ID string `json:"id"`
				} `json:"response"`
			}
			if err = json.Unmarshal(payload, &event); err != nil {
				_ = conn.Close()
				t.Fatal(err)
			}
			if event.Type == "error" {
				if turn != 2 || event.Status != 409 || event.Error.Code != "previous_response_not_found" {
					_ = conn.Close()
					t.Fatalf("unexpected websocket error: %s", payload)
				}
				terminal = true
				break
			}
			if event.Type == "response.completed" {
				if turn == 2 {
					_ = conn.Close()
					t.Fatal("unreplayable continuation succeeded after reconnect")
				}
				responseID = event.Response.ID
				terminal = responseID != ""
				break
			}
		}
		_ = conn.Close()
		if !terminal {
			t.Fatal("websocket did not return the expected terminal event")
		}
		if turn == 1 {
			for _, a := range state.Accounts {
				if a.ID == account {
					a.Paused = true
					if err = s.controlPlane.SaveAccount(a); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.calls) != 2 {
		t.Fatalf("reconnect calls: %v", e.calls)
	}
	for _, id := range e.calls {
		if id != "http-a" {
			t.Fatal("websocket reconnect escaped scope")
		}
	}
}

func (e *controlPlaneLoadExecutor) ExecuteStream(ctx context.Context, a *auth.Auth, req executor.Request, _ executor.Options) (*executor.StreamResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, a.ID)
	e.mu.Unlock()
	// Reporters initialize the credential index before handing auth to a worker;
	// the manager can clone the same credential after ExecuteStream returns.
	reporter := helps.NewUsageReporter(ctx, "codex", req.Model, a)
	chunks := make(chan executor.StreamChunk)
	go func() {
		defer close(chunks)
		id := "resp_" + usage.ExecutionRequestIDFromContext(ctx)
		select {
		case chunks <- executor.StreamChunk{Payload: []byte(fmt.Sprintf("data: {\"type\":\"response.created\",\"response\":{\"id\":%q}}\n\n", id))}:
		case <-ctx.Done():
			return
		}
		select {
		case <-e.gate:
		case <-ctx.Done():
			return
		}
		reporter.Publish(ctx, usage.Detail{InputTokens: 1, OutputTokens: 1, TotalTokens: 2})
		select {
		case chunks <- executor.StreamChunk{Payload: []byte(fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n", id))}:
		case <-ctx.Done():
		}
	}()
	return &executor.StreamResult{Chunks: chunks}, nil
}

// Opt-in bounded HTTP load uses fake accounts only. Each wave holds all streams
// at a barrier, cancels a quarter, then publishes actual fixture usage for the rest.
func TestControlPlaneConcurrentHTTPStreamLoad(t *testing.T) {
	if os.Getenv("CP_PERF_SMOKE") == "" {
		t.Skip("set CP_PERF_SMOKE=1 for isolated bounded load")
	}
	s, _ := controlPlaneHTTPFixture(t)
	var account string
	stateRequest := httptest.NewRequest("GET", "/api/control-plane/state", nil)
	stateRequest.Header.Set("Authorization", "Bearer "+os.Getenv("CP_TEST_ADMIN"))
	stateResponse := httptest.NewRecorder()
	s.engine.ServeHTTP(stateResponse, stateRequest)
	var state struct{ Accounts []controlplane.Account }
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	for _, a := range state.Accounts {
		if a.Label == "http-a" {
			account = a.ID
		}
	}
	key, secret, err := s.controlPlane.CreateKey(controlplane.APIKey{Name: "load-fixture", Bindings: map[string]controlplane.Binding{"codex": {Accounts: []string{account}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	finished := make(chan struct{}, 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.engine.ServeHTTP(w, r); finished <- struct{}{} }))
	defer server.Close()
	var durations []time.Duration
	for wave := 0; wave < 4; wave++ {
		gate := make(chan struct{})
		e := &controlPlaneLoadExecutor{gate: gate}
		s.handlers.AuthManager.RegisterExecutor(e)
		ready := make(chan error, 16)
		results := make(chan struct {
			duration time.Duration
			err      error
		}, 16)
		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(abandon bool) {
				defer wg.Done()
				start := time.Now()
				requestCtx, stop := context.WithCancel(ctx)
				defer stop()
				req, _ := http.NewRequestWithContext(requestCtx, "POST", server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"fixture","stream":true}`))
				req.Header.Set("Authorization", "Bearer "+secret)
				req.Header.Set("Content-Type", "application/json")
				response, err := server.Client().Do(req)
				if err != nil {
					ready <- err
					results <- struct {
						duration time.Duration
						err      error
					}{time.Since(start), err}
					return
				}
				defer response.Body.Close()
				reader := bufio.NewReader(response.Body)
				line, err := reader.ReadString('\n')
				if err == nil && (response.StatusCode != 200 || !strings.Contains(line, "response.created")) {
					err = fmt.Errorf("stream bootstrap status=%d", response.StatusCode)
				}
				if abandon {
					stop()
					_ = response.Body.Close()
				}
				ready <- err
				if err == nil && !abandon {
					var body []byte
					body, err = io.ReadAll(reader)
					if err == nil && !strings.Contains(string(body), "response.completed") {
						err = fmt.Errorf("missing completed frame")
					}
				}
				results <- struct {
					duration time.Duration
					err      error
				}{time.Since(start), err}
			}(i < 4)
		}
		for range 16 {
			if err := <-ready; err != nil {
				close(gate)
				t.Fatal(err)
			}
		}
		// Cancellation travels over HTTP asynchronously. Wait for all abandoned
		// handlers before releasing the upstream barrier; otherwise a cancelled
		// client can still legitimately receive a final upstream settlement.
		for range 4 {
			select {
			case <-finished:
			case <-ctx.Done():
				close(gate)
				t.Fatal(ctx.Err())
			}
		}
		close(gate)
		wg.Wait()
		for range 12 {
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		for range 16 {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			durations = append(durations, result.duration)
		}
		if s.controlPlane.InFlight(account) != 0 {
			t.Fatal("HTTP disconnect leaked in-flight lease")
		}
		e.mu.Lock()
		for _, id := range e.calls {
			if id != "http-a" {
				t.Fatal("load escaped key scope")
			}
		}
		e.mu.Unlock()
	}
	logs, err := s.controlPlane.RequestLogs(ctx, controlplane.LogFilter{APIKey: key.ID, Limit: 100})
	if err != nil || len(logs) != 48 {
		t.Fatalf("exact settlements: %d %v", len(logs), err)
	}
	for _, row := range logs {
		if row.Total != 2 {
			t.Fatal("unexpected usage")
		}
	}
	unresolved, err := s.controlPlane.UnresolvedExecutions(ctx)
	if err != nil || len(unresolved) != 16 {
		t.Fatalf("cancelled execution evidence: %d %v", len(unresolved), err)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("fixture HTTP SSE: 64 attempts, 16 concurrent, 16 intentional disconnects, 48 exact settlements, 96 tokens; end-to-end including barrier p50=%s p95=%s max=%s; no unexpected errors; temporary database/listener cleaned by test", durations[31], durations[60], durations[63])
}
