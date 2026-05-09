package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gateway-llm/gateway-llm/internal/providers/oaicompat"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

// Azure OpenAI uses a deployment-scoped URL and api-key header auth. The
// provider expects api_base to be the full resource URL (e.g.
// https://myresource.openai.azure.com) and provider_model to be the
// deployment name (e.g. "gpt-4o-chat"). A default api-version is appended
// when the caller does not supply one via apiBase.
type Provider struct{ *oaicompat.Base }

const defaultAPIVersion = "2024-08-01-preview"

func New() *Provider {
	b := oaicompat.NewBase("azure", "")
	b.AuthHeader = "api-key"
	b.AuthScheme = ""
	b.SupportsEmbeddings = true
	return &Provider{Base: b}
}

func (p *Provider) urlFor(path, model, apiBase string) string {
	base := strings.TrimRight(apiBase, "/")
	if base == "" {
		base = "https://api.openai.azure.com"
	}
	ver := defaultAPIVersion
	if idx := strings.Index(base, "?"); idx > 0 {
		ver = base[idx+1:]
		base = base[:idx]
	}
	sep := "?"
	if strings.Contains(ver, "=") {
		return fmt.Sprintf("%s/openai/deployments/%s%s%s%s", base, model, path, sep, ver)
	}
	return fmt.Sprintf("%s/openai/deployments/%s%s?api-version=%s", base, model, path, ver)
}

func (p *Provider) TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := p.urlFor("/chat/completions", req.Model, apiBase)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("api-key", apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}

func (p *Provider) TransformEmbeddingsRequest(ctx context.Context, req *types.EmbeddingRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := p.urlFor("/embeddings", req.Model, apiBase)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("api-key", apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	return hreq, nil
}
