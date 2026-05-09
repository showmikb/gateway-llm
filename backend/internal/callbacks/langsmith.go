package callbacks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type LangsmithCallback struct {
	endpoint string
	apiKey   string
	project  string
	client   *http.Client
}

func NewLangsmith(endpoint, apiKeyEnv, project string) *LangsmithCallback {
	if endpoint == "" {
		endpoint = "https://api.smith.langchain.com"
	}
	if project == "" {
		project = "default"
	}
	return &LangsmithCallback{
		endpoint: endpoint,
		apiKey:   os.Getenv(apiKeyEnv),
		project:  project,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (l *LangsmithCallback) Name() string { return "langsmith" }

func (l *LangsmithCallback) Send(ctx context.Context, event RequestEvent) error {
	run := map[string]interface{}{
		"name":         event.ModelAlias + "/" + event.Endpoint,
		"run_type":     "llm",
		"session_name": l.project,
		"start_time":   event.Timestamp.Format(time.RFC3339Nano),
		"end_time":     event.Timestamp.Add(time.Duration(event.DurationMS) * time.Millisecond).Format(time.RFC3339Nano),
		"inputs":       map[string]interface{}{"model": event.ModelAlias, "provider": event.Provider},
		"outputs":      map[string]interface{}{"status": event.Status},
		"extra": map[string]interface{}{
			"metadata": map[string]interface{}{
				"prompt_tokens":     event.PromptTokens,
				"completion_tokens": event.CompletionTokens,
				"total_tokens":      event.TotalTokens,
				"cost_usd":          event.CostUSD,
				"provider_model":    event.ProviderModel,
			},
		},
	}
	body, err := json.Marshal(run)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", l.endpoint+"/runs", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", l.apiKey)
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("langsmith send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("langsmith returned status %d", resp.StatusCode)
	}
	return nil
}

func (l *LangsmithCallback) Close() error { return nil }
