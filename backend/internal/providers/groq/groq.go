package groq

import "github.com/gateway-llm/gateway-llm/internal/providers/oaicompat"

// Groq serves OpenAI-compatible chat completions at api.groq.com/openai.
type Provider struct{ *oaicompat.Base }

func New() *Provider {
	b := oaicompat.NewBase("groq", "https://api.groq.com/openai")
	return &Provider{Base: b}
}
