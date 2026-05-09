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

type WebhookCallback struct {
	name     string
	endpoint string
	method   string
	headers  map[string]string
	client   *http.Client
}

func NewWebhook(name, endpoint, method string, headers map[string]string, timeout time.Duration) *WebhookCallback {
	if method == "" {
		method = "POST"
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &WebhookCallback{
		name:     name,
		endpoint: endpoint,
		method:   method,
		headers:  headers,
		client:   &http.Client{Timeout: timeout},
	}
}

func (w *WebhookCallback) Name() string { return w.name }

func (w *WebhookCallback) Send(ctx context.Context, event RequestEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, w.method, w.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func (w *WebhookCallback) Close() error { return nil }
