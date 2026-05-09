package callbacks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HeliconeCallback forwards trace events to Helicone via the public
// `/v1/async/log/request` endpoint. Helicone's native integration is a
// proxy, but their async ingestion endpoint accepts the same payloads,
// which lets gateway-llm write-through every call without adding a
// second hop on the hot path.
type HeliconeCallback struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

func NewHelicone(endpoint, apiKey string) *HeliconeCallback {
	if endpoint == "" {
		endpoint = "https://api.helicone.ai"
	}
	return &HeliconeCallback{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (h *HeliconeCallback) Name() string { return "helicone" }

func (h *HeliconeCallback) Send(ctx context.Context, ev RequestEvent) error {
	if h.apiKey == "" {
		return nil
	}
	payload := map[string]any{
		"providerRequest": map[string]any{
			"url":  ev.Endpoint,
			"json": map[string]any{"model": ev.ModelAlias},
			"meta": map[string]any{
				"Helicone-Request-Id": ev.TraceID,
				"Helicone-User-Id":    ev.UserID,
			},
		},
		"providerResponse": map[string]any{
			"status":  ev.Status,
			"json":    map[string]any{"model": ev.ProviderModel},
			"textBody": ev.ErrorMessage,
		},
		"timing": map[string]any{
			"startTime": map[string]any{"seconds": ev.Timestamp.Unix()},
			"endTime":   map[string]any{"seconds": ev.Timestamp.Add(time.Duration(ev.DurationMS) * time.Millisecond).Unix()},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.endpoint+"/v1/async/log/request", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("helicone: http %d", resp.StatusCode)
	}
	return nil
}

func (h *HeliconeCallback) Close() error { return nil }
