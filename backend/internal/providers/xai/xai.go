package xai

import "github.com/gateway-llm/gateway-llm/internal/providers/oaicompat"

// xAI (Grok) exposes an OpenAI-compatible endpoint at api.x.ai.
type Provider struct{ *oaicompat.Base }

func New() *Provider {
	b := oaicompat.NewBase("xai", "https://api.x.ai")
	b.SupportsEmbeddings = true
	return &Provider{Base: b}
}
