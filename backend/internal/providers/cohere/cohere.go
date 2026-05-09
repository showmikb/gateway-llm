package cohere

import "github.com/gateway-llm/gateway-llm/internal/providers/oaicompat"

// Cohere's compatibility endpoint mirrors the OpenAI chat API at
// api.cohere.com/compatibility/v1. Embeddings also supported.
type Provider struct{ *oaicompat.Base }

func New() *Provider {
	b := oaicompat.NewBase("cohere", "https://api.cohere.com/compatibility")
	b.ChatPath = "/v1/chat/completions"
	b.EmbeddingsPath = "/v1/embeddings"
	b.SupportsEmbeddings = true
	return &Provider{Base: b}
}
