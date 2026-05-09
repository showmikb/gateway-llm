// Package gatewayllm is the official Go SDK for Gateway-LLM.
//
// It is deliberately minimal: a small typed struct wrapping net/http.
// Callers who already have an OpenAI-compatible client library can
// skip this package and just set base_url; the value we add is typed
// hint helpers and Ed25519 receipt verification.
package gatewayllm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	HeaderTags        = "X-Gateway-LLM-Tags"
	HeaderQuality     = "X-Gateway-LLM-Quality"
	HeaderSpeculative = "X-Gateway-LLM-Speculative"
	HeaderMaxCost     = "X-Gateway-LLM-Max-Cost-USD"
	HeaderTraceID     = "X-Gateway-LLM-Trace-Id"
	HeaderRecord      = "X-Gateway-LLM-Record"

	HeaderReceiptID   = "X-Gateway-LLM-Receipt-Id"
	HeaderSavings     = "X-Gateway-LLM-Estimated-Savings-USD"
	HeaderComplexity  = "X-Gateway-LLM-Complexity-Score"
	HeaderRouting     = "X-Gateway-LLM-Routing-Decision"
	HeaderPII         = "X-Gateway-LLM-Pii-Redactions"
)

// Hints are Gateway-LLM extension fields, encoded into headers.
type Hints struct {
	Tags        []string
	Quality     string  // "cheap" | "balanced" | "best"
	Speculative bool
	MaxCostUSD  float64
	TraceID     string
	Record      bool
}

// Telemetry is decoded from response headers.
type Telemetry struct {
	ReceiptID           string
	ComplexityScore     float64
	RoutingDecision     string
	EstimatedSavingsUSD float64
	PIIRedactions       int
}

// Client is a thin wrapper around net/http targeting Gateway-LLM's
// OpenAI-compatible API.
type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

// New returns a Client with a sensible default http.Client.
func New(apiKey, baseURL string) *Client {
	return &Client{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// ChatRequest covers the minimal chat shape. For full OpenAI fidelity,
// prefer an OpenAI Go client with BaseURL set to `base/v1`.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	GatewayLLM Telemetry `json:"-"`
}

// Chat performs a non-streaming chat completion with Gateway-LLM hints.
func (c *Client) Chat(ctx context.Context, in ChatRequest, hints Hints) (*ChatResponse, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	applyHints(req.Header, hints)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gateway-llm: http %d: %s", resp.StatusCode, string(raw))
	}
	var out ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.GatewayLLM = telemetryFromHeader(resp.Header)
	return &out, nil
}

func applyHints(h http.Header, hints Hints) {
	if len(hints.Tags) > 0 {
		h.Set(HeaderTags, strings.Join(hints.Tags, ","))
	}
	if hints.Quality != "" {
		h.Set(HeaderQuality, hints.Quality)
	}
	if hints.Speculative {
		h.Set(HeaderSpeculative, "true")
	}
	if hints.MaxCostUSD > 0 {
		h.Set(HeaderMaxCost, fmt.Sprintf("%.6f", hints.MaxCostUSD))
	}
	if hints.TraceID != "" {
		h.Set(HeaderTraceID, hints.TraceID)
	}
	if hints.Record {
		h.Set(HeaderRecord, "true")
	}
}

func telemetryFromHeader(h http.Header) Telemetry {
	t := Telemetry{
		ReceiptID:       h.Get(HeaderReceiptID),
		RoutingDecision: h.Get(HeaderRouting),
	}
	if v := h.Get(HeaderComplexity); v != "" {
		fmt.Sscanf(v, "%f", &t.ComplexityScore)
	}
	if v := h.Get(HeaderSavings); v != "" {
		fmt.Sscanf(v, "%f", &t.EstimatedSavingsUSD)
	}
	if v := h.Get(HeaderPII); v != "" {
		fmt.Sscanf(v, "%d", &t.PIIRedactions)
	}
	return t
}

// Receipt mirrors the server-side envelope.
type Receipt struct {
	ID            string  `json:"id"`
	TraceID       string  `json:"trace_id,omitempty"`
	OrgID         string  `json:"org_id,omitempty"`
	APIKeyID      string  `json:"api_key_id,omitempty"`
	Alias         string  `json:"alias"`
	Provider      string  `json:"provider"`
	ProviderModel string  `json:"provider_model"`
	Region        string  `json:"region,omitempty"`
	Timestamp     string  `json:"ts"`
	ReqHash       string  `json:"req_hash"`
	RespHash      string  `json:"resp_hash"`
	PromptTokens  int     `json:"prompt_tokens"`
	OutputTokens  int     `json:"completion_tokens"`
	TotalTokens   int     `json:"total_tokens"`
	CostUSD       float64 `json:"cost_usd"`
	PrevReceiptID string  `json:"prev_receipt_id,omitempty"`
	PrevHash      string  `json:"prev_hash,omitempty"`
	PublicKeyID   string  `json:"public_key_id"`
	Signature     string  `json:"sig"`
}

// VerifyReceipt fetches and validates a signed receipt. The public key
// is fetched once per Client and cached.
func (c *Client) VerifyReceipt(ctx context.Context, id string) (*Receipt, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/receipts/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("receipt http %d", resp.StatusCode)
	}
	var r Receipt
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	pub, err := c.publicKey(ctx)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return nil, fmt.Errorf("bad sig b64: %w", err)
	}
	body, err := canonicalBytes(&r)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(pub, body, sig) {
		return nil, fmt.Errorf("signature mismatch")
	}
	return &r, nil
}

func (c *Client) publicKey(ctx context.Context) (ed25519.PublicKey, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/.well-known/gateway-llm-receipts.json", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("well-known http %d", resp.StatusCode)
	}
	var wk struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wk); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(wk.PublicKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size %d", len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// canonicalBytes must exactly match the server's CanonicalBytes.
// Field order and omit-empty rules are copied verbatim so the signature
// verifies without an extra round-trip through the gateway.
func canonicalBytes(r *Receipt) ([]byte, error) {
	type canonical struct {
		ID            string  `json:"id"`
		TraceID       string  `json:"trace_id,omitempty"`
		OrgID         string  `json:"org_id,omitempty"`
		APIKeyID      string  `json:"api_key_id,omitempty"`
		Alias         string  `json:"alias"`
		Provider      string  `json:"provider"`
		ProviderModel string  `json:"provider_model"`
		Region        string  `json:"region,omitempty"`
		Timestamp     string  `json:"ts"`
		ReqHash       string  `json:"req_hash"`
		RespHash      string  `json:"resp_hash"`
		PromptTokens  int     `json:"prompt_tokens"`
		OutputTokens  int     `json:"completion_tokens"`
		TotalTokens   int     `json:"total_tokens"`
		CostUSD       float64 `json:"cost_usd"`
		PrevReceiptID string  `json:"prev_receipt_id,omitempty"`
		PrevHash      string  `json:"prev_hash,omitempty"`
		PublicKeyID   string  `json:"public_key_id"`
	}
	c := canonical{
		ID:            r.ID,
		TraceID:       r.TraceID,
		OrgID:         r.OrgID,
		APIKeyID:      r.APIKeyID,
		Alias:         r.Alias,
		Provider:      r.Provider,
		ProviderModel: r.ProviderModel,
		Region:        r.Region,
		Timestamp:     r.Timestamp,
		ReqHash:       r.ReqHash,
		RespHash:      r.RespHash,
		PromptTokens:  r.PromptTokens,
		OutputTokens:  r.OutputTokens,
		TotalTokens:   r.TotalTokens,
		CostUSD:       r.CostUSD,
		PrevReceiptID: r.PrevReceiptID,
		PrevHash:      r.PrevHash,
		PublicKeyID:   r.PublicKeyID,
	}
	return json.Marshal(c)
}
