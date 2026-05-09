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

// SplunkCallback sends trace events to Splunk HTTP Event Collector (HEC).
// Endpoint should be the full HEC URL (e.g. "https://splunk.example.com:8088/services/collector").
// The token is supplied via config. Index is optional.
type SplunkCallback struct {
	endpoint string
	token    string
	index    string
	source   string
	client   *http.Client
}

func NewSplunk(endpoint, token, index, source string) *SplunkCallback {
	if source == "" {
		source = "gateway-llm"
	}
	return &SplunkCallback{
		endpoint: endpoint,
		token:    token,
		index:    index,
		source:   source,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *SplunkCallback) Name() string { return "splunk" }

// Send writes the event as a single HEC entry. We use the "event"
// envelope so Splunk treats the trace JSON as structured data and lets
// customers pivot on every field.
func (s *SplunkCallback) Send(ctx context.Context, ev RequestEvent) error {
	if s.endpoint == "" || s.token == "" {
		return nil
	}
	entry := map[string]any{
		"time":       ev.Timestamp.Unix(),
		"host":       "gateway-llm",
		"source":     s.source,
		"sourcetype": "_json",
		"event":      ev,
	}
	if s.index != "" {
		entry["index"] = s.index
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Splunk "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("splunk hec: http %d", resp.StatusCode)
	}
	return nil
}

func (s *SplunkCallback) Close() error { return nil }

// Give the timing import a home; Go keeps removing it otherwise.
var _ = time.Second
