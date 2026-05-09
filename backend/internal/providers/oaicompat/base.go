// Package oaicompat implements a reusable base for providers that speak
// OpenAI's HTTP API (chat completions, embeddings, optional streaming).
// Subpackages (mistral, groq, xai, azure, cohere, etc.) embed Base and
// only override the provider name, default endpoint, and any header
// quirks, keeping transformation logic DRY.
package oaicompat

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

// Base provides default OpenAI-compatible request/response transforms.
// Embedders can override Name() and DefaultURL() (and any individual
// Transform method) to specialize behavior.
type Base struct {
	ProviderName string
	URL          string
	// AuthHeader defaults to "Authorization: Bearer <key>". If AuthScheme
	// is empty the default scheme is "Bearer". Providers that use a
	// non-standard header (e.g. Azure's "api-key") can set AuthHeader and
	// leave AuthScheme empty.
	AuthHeader string
	AuthScheme string
	// ChatPath overrides the chat path (default "/v1/chat/completions").
	ChatPath string
	// EmbeddingsPath overrides the embeddings path (default "/v1/embeddings").
	EmbeddingsPath string
	// ExtraHeaders are attached to every request.
	ExtraHeaders map[string]string
	// HTTPClient is used for discovery; request execution uses the
	// gateway's http.DefaultClient via the router.
	HTTPClient *http.Client
	// SupportsStream indicates whether streaming is supported.
	SupportsStream bool
	// SupportsEmbeddings indicates whether embeddings is supported.
	SupportsEmbeddings bool
}

func NewBase(name, url string) *Base {
	return &Base{
		ProviderName:       name,
		URL:                url,
		ChatPath:           "/v1/chat/completions",
		EmbeddingsPath:     "/v1/embeddings",
		AuthHeader:         "Authorization",
		AuthScheme:         "Bearer",
		HTTPClient:         &http.Client{Timeout: 120 * time.Second},
		SupportsStream:     true,
		SupportsEmbeddings: false,
	}
}

func (b *Base) Name() string { return b.ProviderName }

func (b *Base) Capabilities() []providers.Capability {
	caps := []providers.Capability{providers.CapChat}
	if b.SupportsEmbeddings {
		caps = append(caps, providers.CapEmbeddings)
	}
	return caps
}

func (b *Base) BaseURL(apiBase string) string {
	if apiBase != "" {
		return strings.TrimRight(apiBase, "/")
	}
	return strings.TrimRight(b.URL, "/")
}

func (b *Base) applyAuth(req *http.Request, apiKey string) {
	header := b.AuthHeader
	if header == "" {
		header = "Authorization"
	}
	scheme := b.AuthScheme
	if scheme != "" {
		req.Header.Set(header, scheme+" "+apiKey)
	} else {
		req.Header.Set(header, apiKey)
	}
	for k, v := range b.ExtraHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
}

func (b *Base) TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := b.BaseURL(apiBase) + b.ChatPath
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	b.applyAuth(hreq, apiKey)
	return hreq, nil
}

func (b *Base) TransformChatResponse(resp *http.Response) (*types.ChatCompletionResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s chat: status %d: %s", b.ProviderName, resp.StatusCode, string(body))
	}
	var out types.ChatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Base) StreamChatResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
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

func (b *Base) TransformEmbeddingsRequest(ctx context.Context, req *types.EmbeddingRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := b.BaseURL(apiBase) + b.EmbeddingsPath
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	b.applyAuth(hreq, apiKey)
	return hreq, nil
}

func (b *Base) TransformEmbeddingsResponse(resp *http.Response) (*types.EmbeddingResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s embeddings: status %d: %s", b.ProviderName, resp.StatusCode, string(body))
	}
	var out types.EmbeddingResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
