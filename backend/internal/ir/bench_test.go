package ir

import (
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

// BenchmarkFromOpenAI captures the cost of normalizing an OpenAI-wire
// request into the canonical IR. Runs on every recorded request.
func BenchmarkFromOpenAI(b *testing.B) {
	req := &types.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []types.ChatMessage{
			{Role: "system", Content: []byte(`"You are a helpful assistant."`)},
			{Role: "user", Content: []byte(`"What is the capital of France?"`)},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FromOpenAI(req)
	}
}

// BenchmarkRoundTrip captures the cost of round-tripping through IR.
func BenchmarkRoundTrip(b *testing.B) {
	req := &types.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []types.ChatMessage{
			{Role: "user", Content: []byte(`"hi"`)},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToOpenAI(FromOpenAI(req))
	}
}
