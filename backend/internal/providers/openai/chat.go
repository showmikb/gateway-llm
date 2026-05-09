package openai

import (
	"bufio"
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

// OpenAIProvider is a passthrough provider for the OpenAI HTTP API.
type OpenAIProvider struct {
	httpClient *http.Client
}

// New returns an OpenAI provider with a default HTTP client (120s timeout).
func New() *OpenAIProvider {
	return &OpenAIProvider{
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Capabilities() []providers.Capability {
	return []providers.Capability{
		providers.CapChat,
		providers.CapResponses,
		providers.CapEmbeddings,
		providers.CapCompletions,
		providers.CapImages,
		providers.CapAudioSpeech,
		providers.CapAudioTranscribe,
		providers.CapModerations,
		providers.CapPassthrough,
	}
}

func baseURL(apiBase string) string {
	if apiBase != "" {
		return strings.TrimRight(apiBase, "/")
	}
	return "https://api.openai.com"
}

func (p *OpenAIProvider) TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := baseURL(apiBase) + "/v1/chat/completions"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *OpenAIProvider) TransformChatResponse(resp *http.Response) (*types.ChatCompletionResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai chat completions: status %d: %s", resp.StatusCode, string(body))
	}
	var out types.ChatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *OpenAIProvider) StreamChatResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	ch := make(chan providers.StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		// Allow long SSE JSON lines.
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				ch <- providers.StreamEvent{Error: ctx.Err()}
				return
			default:
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if payload == "[DONE]" {
				ch <- providers.StreamEvent{Done: true}
				return
			}
			ch <- providers.StreamEvent{Data: []byte(payload)}
		}
		if err := scanner.Err(); err != nil {
			ch <- providers.StreamEvent{Error: err}
		}
	}()
	return ch, nil
}

func (p *OpenAIProvider) TransformEmbeddingsRequest(ctx context.Context, req *types.EmbeddingRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := baseURL(apiBase) + "/v1/embeddings"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *OpenAIProvider) TransformEmbeddingsResponse(resp *http.Response) (*types.EmbeddingResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai embeddings: status %d: %s", resp.StatusCode, string(body))
	}
	var out types.EmbeddingResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *OpenAIProvider) TransformCompletionsRequest(ctx context.Context, req *types.CompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := baseURL(apiBase) + "/v1/completions"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *OpenAIProvider) TransformCompletionsResponse(resp *http.Response) (*types.CompletionResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai completions: status %d: %s", resp.StatusCode, string(body))
	}
	var out types.CompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *OpenAIProvider) TransformImageRequest(ctx context.Context, req *types.ImageRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := baseURL(apiBase) + "/v1/images/generations"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *OpenAIProvider) TransformImageResponse(resp *http.Response) (*types.ImageResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai images: status %d: %s", resp.StatusCode, string(body))
	}
	var out types.ImageResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *OpenAIProvider) TransformSpeechRequest(ctx context.Context, req *types.SpeechRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := baseURL(apiBase) + "/v1/audio/speech"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *OpenAIProvider) StreamSpeechResponse(resp *http.Response) (io.ReadCloser, string, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, "", fmt.Errorf("openai speech: status %d: %s", resp.StatusCode, string(b))
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}
	return resp.Body, ct, nil
}

func (p *OpenAIProvider) TransformModerationRequest(ctx context.Context, req *types.ModerationRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := baseURL(apiBase) + "/v1/moderations"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *OpenAIProvider) TransformModerationResponse(resp *http.Response) (*types.ModerationResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai moderations: status %d: %s", resp.StatusCode, string(body))
	}
	var out types.ModerationResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *OpenAIProvider) TransformResponsesRequest(ctx context.Context, req *types.ResponsesRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := baseURL(apiBase) + "/v1/responses"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *OpenAIProvider) TransformResponsesResponse(resp *http.Response) (*types.ResponsesAPIResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai responses: status %d: %s", resp.StatusCode, string(body))
	}
	var out types.ResponsesAPIResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *OpenAIProvider) StreamResponsesResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	ch := make(chan providers.StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				ch <- providers.StreamEvent{Error: ctx.Err()}
				return
			default:
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if payload == "[DONE]" {
				ch <- providers.StreamEvent{Done: true}
				return
			}
			ch <- providers.StreamEvent{Data: []byte(payload)}
		}
		if err := scanner.Err(); err != nil {
			ch <- providers.StreamEvent{Error: err}
		}
	}()
	return ch, nil
}

func (p *OpenAIProvider) ForwardRequest(ctx context.Context, method, path, rawQuery string, header http.Header, body io.Reader, apiKey string, apiBase string) (*http.Request, error) {
	u := baseURL(apiBase) + path
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	hreq, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	for k, vals := range header {
		switch strings.ToLower(k) {
		case "host", "authorization", "connection":
			continue
		}
		for _, v := range vals {
			hreq.Header.Add(k, v)
		}
	}
	hreq.Header.Set("Authorization", "Bearer "+apiKey)
	return hreq, nil
}
