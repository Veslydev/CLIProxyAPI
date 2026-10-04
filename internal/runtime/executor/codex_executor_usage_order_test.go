package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCodexNonStreamPublishesBaseUsageBeforeImageToolUsage(t *testing.T) {
	for _, imageUsage := range []string{
		`{"input_tokens":0,"output_tokens":0,"total_tokens":0}`,
		`{"input_tokens":10,"output_tokens":20,"total_tokens":30}`,
	} {
		t.Run(imageUsage, func(t *testing.T) {
			alias := t.Name()
			capture := &codexResponseModelUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
			coreusage.RegisterNamedPlugin(alias, capture)
			t.Cleanup(func() { coreusage.RegisterNamedPlugin(alias, codexResponseModelNoopUsagePlugin{}) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"id":"fixture-response","object":"response","status":"completed","model":"gpt-6-luna","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":325,"output_tokens":5,"total_tokens":330},"tool_usage":{"image_gen":` + imageUsage + `}}}` + "\n\n"))
			}))
			defer server.Close()
			ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
			executor := NewCodexExecutor(&config.Config{})
			auth := &cliproxyauth.Auth{ID: "fixture-account", Provider: "codex", Attributes: map[string]string{"api_key": "fixture-key", "base_url": server.URL}}
			_, errExecute := executor.Execute(ctx, auth, cliproxyexecutor.Request{Model: "gpt-6-luna", Payload: []byte(`{"model":"gpt-6-luna","input":"Reply OK"}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")})
			if errExecute != nil {
				t.Fatalf("Execute failed: %v", errExecute)
			}
			record := capture.await(t)
			if record.Model != "gpt-6-luna" || record.Detail.InputTokens != 325 || record.Detail.OutputTokens != 5 || record.Detail.TotalTokens != 330 {
				t.Fatalf("base usage replaced by image-tool fallback: model=%s detail=%+v", record.Model, record.Detail)
			}
			if imageUsage != `{"input_tokens":0,"output_tokens":0,"total_tokens":0}` {
				imageRecord := capture.await(t)
				if imageRecord.Model != codexDefaultImageToolModel || imageRecord.Detail.TotalTokens != 30 {
					t.Fatalf("additional image-tool usage lost: model=%s detail=%+v", imageRecord.Model, imageRecord.Detail)
				}
			}
		})
	}
}
