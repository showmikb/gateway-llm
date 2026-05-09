package callbacks

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LangfuseCallback pushes gateway-llm trace events to a Langfuse
// instance (cloud or self-hosted). Uses Langfuse's `/api/public/ingestion`
// batch endpoint with public/secret key Basic Auth.
//
// This is part of the open-core story: every LLM observability tool
// already offered by competitors as a premium feature is available in
// OSS gateway-llm as a one-line callback entry.
type LangfuseCallback struct {
	endpoint  string
	publicKey string
	secretKey string
	client    *http.Client
}

// NewLangfuse returns a configured callback. endpoint may be empty, in
// which case Langfuse Cloud EU is used. publicKey and secretKey are
// required; an empty secretKey disables the callback (no-op).
func NewLangfuse(endpoint, publicKey, secretKey string) *LangfuseCallback {
	if endpoint == "" {
		endpoint = "https://cloud.langfuse.com"
	}
	return &LangfuseCallback{
		endpoint:  endpoint,
		publicKey: publicKey,
		secretKey: secretKey,
		client:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (l *LangfuseCallback) Name() string { return "langfuse" }

// Send maps a RequestEvent to a Langfuse `generation` object inside
// one batch. We use trace_id as the Langfuse trace id so joins against
// the gateway's recording store are trivial.
func (l *LangfuseCallback) Send(ctx context.Context, ev RequestEvent) error {
	if l.secretKey == "" {
		return nil
	}
	batch := map[string]any{
		"batch": []map[string]any{
			{
				"id":   ev.TraceID + ":trace",
				"type": "trace-create",
				"timestamp": ev.Timestamp.UTC().Format(time.RFC3339Nano),
				"body": map[string]any{
					"id":       ev.TraceID,
					"name":     "gateway-llm:" + ev.Endpoint,
					"userId":   ev.UserID,
					"metadata": ev.Metadata,
				},
			},
			{
				"id":   ev.TraceID + ":gen",
				"type": "generation-create",
				"timestamp": ev.Timestamp.UTC().Format(time.RFC3339Nano),
				"body": map[string]any{
					"id":                ev.SpanID,
					"traceId":           ev.TraceID,
					"name":              ev.ModelAlias,
					"model":             ev.ProviderModel,
					"startTime":         ev.Timestamp.UTC().Format(time.RFC3339Nano),
					"endTime":           ev.Timestamp.Add(time.Duration(ev.DurationMS) * time.Millisecond).UTC().Format(time.RFC3339Nano),
					"promptTokens":     ev.PromptTokens,
					"completionTokens": ev.CompletionTokens,
					"totalTokens":      ev.TotalTokens,
					"usage": map[string]any{
						"promptTokens":     ev.PromptTokens,
						"completionTokens": ev.CompletionTokens,
						"totalTokens":      ev.TotalTokens,
						"totalCost":        ev.CostUSD,
					},
					"metadata": map[string]any{
						"provider":    ev.Provider,
						"status":      ev.Status,
						"api_key_id":  ev.APIKeyID,
						"team_id":     ev.TeamID,
						"error":       ev.ErrorMessage,
					},
				},
			},
		},
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		l.endpoint+"/api/public/ingestion", bytes.NewReader(body))
	if err != nil {
		return err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(l.publicKey + ":" + l.secretKey))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusMultiStatus {
		return fmt.Errorf("langfuse: http %d", resp.StatusCode)
	}
	return nil
}

func (l *LangfuseCallback) Close() error { return nil }
