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

// SlackCallback posts *only* events that look noteworthy to a Slack
// incoming webhook. The common case for a gateway-llm log sink is
// high-volume and deeply uninteresting; we filter to errors and to the
// first spike of cost for a new alias so the channel stays signal-rich.
type SlackCallback struct {
	webhookURL string
	minCost    float64
	client     *http.Client
}

// NewSlack wires a webhook. minCostUSD filters out events below the
// threshold; a 0 value means "post everything non-error".
func NewSlack(webhookURL string, minCostUSD float64) *SlackCallback {
	return &SlackCallback{
		webhookURL: webhookURL,
		minCost:    minCostUSD,
		client:     &http.Client{Timeout: 3 * time.Second},
	}
}

func (s *SlackCallback) Name() string { return "slack" }

func (s *SlackCallback) Send(ctx context.Context, ev RequestEvent) error {
	if s.webhookURL == "" {
		return nil
	}
	interesting := ev.Status >= 400 || ev.ErrorMessage != "" || ev.CostUSD >= s.minCost
	if !interesting {
		return nil
	}
	color := "good"
	if ev.Status >= 400 {
		color = "danger"
	} else if ev.CostUSD >= s.minCost && s.minCost > 0 {
		color = "warning"
	}
	text := fmt.Sprintf("*%s* `%s` — %d tokens, $%.4f, %dms",
		ev.ModelAlias, ev.ProviderModel, ev.TotalTokens, ev.CostUSD, ev.DurationMS)
	if ev.ErrorMessage != "" {
		text = fmt.Sprintf(":rotating_light: *%s* — %s", ev.ModelAlias, ev.ErrorMessage)
	}
	payload := map[string]any{
		"attachments": []map[string]any{
			{
				"color": color,
				"text":  text,
				"fields": []map[string]any{
					{"title": "trace", "value": ev.TraceID, "short": true},
					{"title": "user", "value": ev.UserID, "short": true},
				},
				"ts": ev.Timestamp.Unix(),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook: http %d", resp.StatusCode)
	}
	return nil
}

func (s *SlackCallback) Close() error { return nil }
