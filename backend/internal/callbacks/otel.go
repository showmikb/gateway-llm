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

type OTELCallback struct {
	endpoint    string
	serviceName string
	headers     map[string]string
	client      *http.Client
}

func NewOTEL(endpoint, serviceName string, headers map[string]string) *OTELCallback {
	if serviceName == "" {
		serviceName = "gateway-llm"
	}
	return &OTELCallback{
		endpoint:    endpoint,
		serviceName: serviceName,
		headers:     headers,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (o *OTELCallback) Name() string { return "otel" }

func (o *OTELCallback) Send(ctx context.Context, event RequestEvent) error {
	attrs := []map[string]interface{}{
		{"key": "llm.model", "value": map[string]string{"stringValue": event.ModelAlias}},
		{"key": "llm.provider", "value": map[string]string{"stringValue": event.Provider}},
		{"key": "llm.prompt_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", event.PromptTokens)}},
		{"key": "llm.completion_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", event.CompletionTokens)}},
		{"key": "llm.cost_usd", "value": map[string]interface{}{"doubleValue": event.CostUSD}},
		{"key": "http.status_code", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", event.Status)}},
	}
	// Smart-routing moat attributes — namespaced so dashboards stay
	// stable even if other attribute keys evolve.
	if event.Routing != nil {
		attrs = append(attrs,
			map[string]interface{}{"key": "routing.requested_alias", "value": map[string]string{"stringValue": event.Routing.RequestedAlias}},
			map[string]interface{}{"key": "routing.served_alias", "value": map[string]string{"stringValue": event.Routing.ServedAlias}},
			map[string]interface{}{"key": "routing.policy_strategy", "value": map[string]string{"stringValue": event.Routing.Strategy}},
			map[string]interface{}{"key": "routing.complexity_score", "value": map[string]interface{}{"doubleValue": event.Routing.ComplexityScore}},
			map[string]interface{}{"key": "routing.complexity_bucket", "value": map[string]string{"stringValue": event.Routing.ComplexityBucket}},
			map[string]interface{}{"key": "routing.overridden", "value": map[string]interface{}{"boolValue": event.Routing.Overridden}},
			map[string]interface{}{"key": "routing.retried", "value": map[string]interface{}{"boolValue": event.Routing.Retried}},
		)
	}
	if event.Cost != nil {
		attrs = append(attrs,
			map[string]interface{}{"key": "cost.baseline_usd", "value": map[string]interface{}{"doubleValue": event.Cost.BaselineUSD}},
			map[string]interface{}{"key": "cost.actual_usd", "value": map[string]interface{}{"doubleValue": event.Cost.ActualUSD}},
			map[string]interface{}{"key": "cost.savings_usd", "value": map[string]interface{}{"doubleValue": event.Cost.SavingsUSD}},
			map[string]interface{}{"key": "cost.discount_pct_applied", "value": map[string]interface{}{"doubleValue": event.Cost.DiscountPct}},
		)
	}
	if event.Quality != nil {
		if event.Quality.Score != nil {
			attrs = append(attrs, map[string]interface{}{"key": "quality.score", "value": map[string]interface{}{"doubleValue": *event.Quality.Score}})
		}
		if event.Quality.Pass != nil {
			attrs = append(attrs, map[string]interface{}{"key": "quality.pass", "value": map[string]interface{}{"boolValue": *event.Quality.Pass}})
		}
		if event.Quality.Scorer != "" {
			attrs = append(attrs, map[string]interface{}{"key": "quality.scorer", "value": map[string]string{"stringValue": event.Quality.Scorer}})
		}
	}
	if event.Receipt != nil {
		attrs = append(attrs,
			map[string]interface{}{"key": "receipt.id", "value": map[string]string{"stringValue": event.Receipt.ID}},
			map[string]interface{}{"key": "receipt.signed_by_key_id", "value": map[string]string{"stringValue": event.Receipt.SignedByKeyID}},
		)
	}

	span := map[string]interface{}{
		"resourceSpans": []map[string]interface{}{
			{
				"resource": map[string]interface{}{
					"attributes": []map[string]interface{}{
						{"key": "service.name", "value": map[string]string{"stringValue": o.serviceName}},
					},
				},
				"scopeSpans": []map[string]interface{}{
					{
						"scope": map[string]interface{}{"name": "gateway-llm"},
						"spans": []map[string]interface{}{
							{
								"traceId":           event.TraceID,
								"spanId":            event.SpanID,
								"name":              event.Endpoint,
								"kind":              3,
								"startTimeUnixNano": fmt.Sprintf("%d", event.Timestamp.UnixNano()),
								"endTimeUnixNano":   fmt.Sprintf("%d", event.Timestamp.Add(time.Duration(event.DurationMS)*time.Millisecond).UnixNano()),
								"status":            map[string]interface{}{"code": 1},
								"attributes":        attrs,
							},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(span)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/v1/traces", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range o.headers {
		req.Header.Set(k, v)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("otel export: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("otel returned status %d", resp.StatusCode)
	}
	return nil
}

func (o *OTELCallback) Close() error { return nil }
