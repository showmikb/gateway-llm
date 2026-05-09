// Package bedrock implements a thin adapter for AWS Bedrock Runtime using
// long-lived API keys (bearer token auth, introduced by AWS in 2025) so we
// avoid pulling the full AWS SDK / SigV4 signer into the binary. The
// provider targets Bedrock's Converse API, which accepts an OpenAI-like
// messages array natively.
//
// Deployment config:
//
//	provider:      bedrock
//	model:         anthropic.claude-3-5-sonnet-20241022-v2:0
//	api_base:      https://bedrock-runtime.us-east-1.amazonaws.com
//	api_key_env:   AWS_BEDROCK_API_KEY   (long-lived bearer token)
package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

type Provider struct {
	httpClient *http.Client
}

func New() *Provider {
	return &Provider{httpClient: &http.Client{Timeout: 120 * time.Second}}
}

func (p *Provider) Name() string { return "bedrock" }

func (p *Provider) Capabilities() []providers.Capability {
	return []providers.Capability{providers.CapChat}
}

type converseMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Text string `json:"text,omitempty"`
}

type converseReq struct {
	Messages        []converseMessage `json:"messages"`
	System          []contentBlock    `json:"system,omitempty"`
	InferenceConfig map[string]any    `json:"inferenceConfig,omitempty"`
}

type converseResp struct {
	Output struct {
		Message converseMessage `json:"message"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
		TotalTokens  int `json:"totalTokens"`
	} `json:"usage"`
	StopReason string `json:"stopReason"`
}

func (p *Provider) TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("bedrock: missing api key")
	}
	if apiBase == "" {
		return nil, fmt.Errorf("bedrock: api_base must be set to https://bedrock-runtime.<region>.amazonaws.com")
	}

	var sys []contentBlock
	var msgs []converseMessage
	for _, m := range req.Messages {
		text, _ := rawToString(m.Content)
		if m.Role == "system" || m.Role == "developer" {
			if text != "" {
				sys = append(sys, contentBlock{Text: text})
			}
			continue
		}
		role := m.Role
		if role == "tool" {
			role = "user"
		}
		msgs = append(msgs, converseMessage{Role: role, Content: []contentBlock{{Text: text}}})
	}

	infer := map[string]any{}
	if req.Temperature != nil {
		infer["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		infer["topP"] = *req.TopP
	}
	maxTok := 0
	if req.MaxCompletionTokens != nil {
		maxTok = *req.MaxCompletionTokens
	} else if req.MaxTokens != nil {
		maxTok = *req.MaxTokens
	}
	if maxTok > 0 {
		infer["maxTokens"] = maxTok
	}

	body, err := json.Marshal(converseReq{Messages: msgs, System: sys, InferenceConfig: infer})
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(apiBase, "/") + "/model/" + req.Model + "/converse"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *Provider) TransformChatResponse(resp *http.Response) (*types.ChatCompletionResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bedrock: status %d: %s", resp.StatusCode, string(body))
	}
	var cr converseResp
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, err
	}
	var text strings.Builder
	for _, b := range cr.Output.Message.Content {
		text.WriteString(b.Text)
	}
	raw, _ := json.Marshal(text.String())
	finish := "stop"
	switch cr.StopReason {
	case "max_tokens":
		finish = "length"
	case "tool_use":
		finish = "tool_calls"
	}
	return &types.ChatCompletionResponse{
		ID:      "bedrock-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Choices: []types.Choice{{
			Index:        0,
			Message:      &types.ChatMessage{Role: "assistant", Content: raw},
			FinishReason: &finish,
		}},
		Usage: &types.Usage{
			PromptTokens:     cr.Usage.InputTokens,
			CompletionTokens: cr.Usage.OutputTokens,
			TotalTokens:      cr.Usage.TotalTokens,
		},
	}, nil
}

// StreamChatResponse is not implemented for Bedrock Converse in this
// minimal adapter; the gateway falls back to the non-streaming path. A
// future iteration can add the ConverseStream AWS event-stream binary
// decoder.
func (p *Provider) StreamChatResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	return nil, fmt.Errorf("bedrock: streaming not supported in this adapter; set stream=false")
}

func rawToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if t, _ := p["text"].(string); t != "" {
				b.WriteString(t)
			}
		}
		return b.String(), nil
	}
	return string(raw), nil
}
