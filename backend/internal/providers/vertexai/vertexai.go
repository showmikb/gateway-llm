// Package vertexai implements a minimal chat adapter for Google Cloud
// Vertex AI Gemini models. The provider uses a short-lived OAuth2 access
// token (supplied by the caller as the api key) to authenticate against
// the generateContent endpoint. Users are expected to refresh the token
// externally, e.g. via `gcloud auth print-access-token` or a service
// account key rotation job. This keeps the gateway binary free of the
// google-auth transitive tree while still giving Vertex coverage.
//
// Deployment config:
//
//	provider:      vertexai
//	model:         gemini-2.5-pro                  # bare model id
//	api_base:      https://{region}-aiplatform.googleapis.com/v1/projects/{project}/locations/{region}/publishers/google
//	api_key_env:   GCP_ACCESS_TOKEN                # bearer OAuth2 token
package vertexai

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

type Provider struct{ httpClient *http.Client }

func New() *Provider { return &Provider{httpClient: &http.Client{Timeout: 120 * time.Second}} }

func (p *Provider) Name() string { return "vertexai" }

func (p *Provider) Capabilities() []providers.Capability {
	return []providers.Capability{providers.CapChat}
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiReq struct {
	Contents         []geminiContent `json:"contents"`
	SystemInstruction *geminiContent `json:"systemInstruction,omitempty"`
	GenerationConfig map[string]any  `json:"generationConfig,omitempty"`
}

type geminiResp struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (p *Provider) TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("vertexai: missing access token")
	}
	if apiBase == "" {
		return nil, fmt.Errorf("vertexai: api_base must be set to the project/location publisher URL")
	}

	var sys *geminiContent
	var contents []geminiContent
	for _, m := range req.Messages {
		text, _ := rawToString(m.Content)
		if m.Role == "system" || m.Role == "developer" {
			if text != "" {
				sys = &geminiContent{Parts: []geminiPart{{Text: text}}}
			}
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: text}}})
	}

	cfg := map[string]any{}
	if req.Temperature != nil {
		cfg["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		cfg["topP"] = *req.TopP
	}
	maxTok := 0
	if req.MaxCompletionTokens != nil {
		maxTok = *req.MaxCompletionTokens
	} else if req.MaxTokens != nil {
		maxTok = *req.MaxTokens
	}
	if maxTok > 0 {
		cfg["maxOutputTokens"] = maxTok
	}

	body, err := json.Marshal(geminiReq{Contents: contents, SystemInstruction: sys, GenerationConfig: cfg})
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(apiBase, "/") + "/models/" + req.Model + ":generateContent"
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
		return nil, fmt.Errorf("vertexai: status %d: %s", resp.StatusCode, string(body))
	}
	var gr geminiResp
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, err
	}
	var choices []types.Choice
	for i, c := range gr.Candidates {
		var b strings.Builder
		for _, part := range c.Content.Parts {
			b.WriteString(part.Text)
		}
		raw, _ := json.Marshal(b.String())
		finish := "stop"
		switch c.FinishReason {
		case "MAX_TOKENS":
			finish = "length"
		case "SAFETY":
			finish = "content_filter"
		}
		choices = append(choices, types.Choice{
			Index:        i,
			Message:      &types.ChatMessage{Role: "assistant", Content: raw},
			FinishReason: &finish,
		})
	}
	return &types.ChatCompletionResponse{
		ID:      "vertex-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Choices: choices,
		Usage: &types.Usage{
			PromptTokens:     gr.UsageMetadata.PromptTokenCount,
			CompletionTokens: gr.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      gr.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (p *Provider) StreamChatResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	return nil, fmt.Errorf("vertexai: streaming not supported in this adapter; set stream=false")
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
