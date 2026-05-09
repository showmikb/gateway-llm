package router

import (
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"go.uber.org/zap"
)

// BenchmarkResolve measures how fast a model alias lookup (including the
// LiteLLM prefix fallback path) returns a deployment. This is on the hot
// path of every /v1/chat/completions call, so its regression budget is
// tight (see bench/thresholds.yml).
func benchRouter() *ModelRouter {
	return New([]config.ModelAlias{{
		ModelAlias: "gpt-4o",
		Deployments: []config.Deployment{{
			Provider: "openai", Model: "gpt-4o", APIKeyEnv: "OPENAI_API_KEY",
		}},
	}, {
		ModelAlias: "claude-sonnet",
		Deployments: []config.Deployment{{
			Provider: "anthropic", Model: "claude-3-sonnet-20240229", APIKeyEnv: "ANTHROPIC_API_KEY",
		}},
	}}, config.RoutingConfig{Strategy: "round-robin"}, nil, zap.NewNop())
}

// BenchmarkResolve measures how fast a model alias lookup returns a
// deployment. This is on the hot path of every /v1/chat/completions call.
func BenchmarkResolve(b *testing.B) {
	r := benchRouter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Resolve("gpt-4o")
	}
}

// BenchmarkResolveLitellmPrefix exercises the fallback path that scans
// deployments for a (provider, model) match. Slower than direct lookup,
// but still expected to complete in well under a microsecond.
func BenchmarkResolveLitellmPrefix(b *testing.B) {
	r := benchRouter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Resolve("anthropic/claude-3-sonnet-20240229")
	}
}
