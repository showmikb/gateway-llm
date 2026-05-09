package mistral

import "github.com/gateway-llm/gateway-llm/internal/providers/oaicompat"

type Provider struct{ *oaicompat.Base }

func New() *Provider {
	b := oaicompat.NewBase("mistral", "https://api.mistral.ai")
	b.SupportsEmbeddings = true
	return &Provider{Base: b}
}
