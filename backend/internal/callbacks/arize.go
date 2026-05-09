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

type ArizeCallback struct {
	apiKey   string
	spaceKey string
	modelID  string
	client   *http.Client
}

func NewArize(apiKeyEnv, spaceKeyEnv, modelID string) *ArizeCallback {
	if modelID == "" {
		modelID = "gateway-llm"
	}
	return &ArizeCallback{
		apiKey:   os.Getenv(apiKeyEnv),
		spaceKey: os.Getenv(spaceKeyEnv),
		modelID:  modelID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *ArizeCallback) Name() string { return "arize" }

func (a *ArizeCallback) Send(ctx context.Context, event RequestEvent) error {
	payload := map[string]interface{}{
		"space_key": a.spaceKey,
		"model_id":  a.modelID,
		"prediction": map[string]interface{}{
			"model_version": event.ProviderModel,
			"features": map[string]interface{}{
				"model_alias":       event.ModelAlias,
				"provider":          event.Provider,
				"endpoint":          event.Endpoint,
				"prompt_tokens":     event.PromptTokens,
				"completion_tokens": event.CompletionTokens,
			},
			"tags": map[string]interface{}{
				"cost_usd":    event.CostUSD,
				"duration_ms": event.DurationMS,
				"status":      event.Status,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://otlp.arize.com/v1", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization", a.apiKey)
	req.Header.Set("space_key", a.spaceKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("arize send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("arize returned status %d", resp.StatusCode)
	}
	return nil
}

func (a *ArizeCallback) Close() error { return nil }
