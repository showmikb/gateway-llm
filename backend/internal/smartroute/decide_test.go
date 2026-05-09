package smartroute

import (
	"encoding/json"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

func TestDecide_DisabledReturnsCallerAlias(t *testing.T) {
	r := New(config.SmartRouteConfig{Enabled: false})
	d := r.Decide(&types.ChatCompletionRequest{}, "gpt-4o")
	if d.Enabled {
		t.Fatalf("Enabled should be false when smartroute disabled")
	}
	if d.Alias != "gpt-4o" || d.FromAlias != "gpt-4o" {
		t.Fatalf("expected caller alias to pass through, got %+v", d)
	}
	if d.Overridden {
		t.Fatalf("Overridden should be false")
	}
}

func TestDecide_SimpleRouteOverride(t *testing.T) {
	cfg := config.SmartRouteConfig{
		Enabled: true,
		Tiers: map[string]string{
			"simple":  "gpt-4o-mini",
			"medium":  "gpt-4o-mini",
			"complex": "gpt-4o",
		},
	}
	r := New(cfg)
	// short greeting -> simple
	req := &types.ChatCompletionRequest{Messages: []types.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	d := r.Decide(req, "gpt-4o")
	if !d.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if !d.Overridden {
		t.Fatalf("expected override from gpt-4o to mini, got %+v", d)
	}
	if d.Alias != "gpt-4o-mini" {
		t.Fatalf("expected gpt-4o-mini, got %s", d.Alias)
	}
	if d.Score < 0 || d.Score > 1 {
		t.Fatalf("score out of range: %f", d.Score)
	}
}

func TestDecide_ComplexKeepsCallerAlias(t *testing.T) {
	cfg := config.SmartRouteConfig{
		Enabled: true,
		Tiers: map[string]string{
			"simple":  "gpt-4o-mini",
			"medium":  "gpt-4o",
			"complex": "gpt-4o",
		},
	}
	r := New(cfg)
	// Long prompt with reasoning keywords -> medium/complex bucket,
	// both of which map back to gpt-4o so no override should happen.
	filler := make([]byte, 12000)
	for i := range filler {
		filler[i] = 'x'
	}
	long := `"Please prove the following theorem step by step. Derive the answer and explain why it is correct. ` +
		`Consider every edge case. Here is context: ` + string(filler) + `"`
	req := &types.ChatCompletionRequest{Messages: []types.ChatMessage{{Role: "user", Content: json.RawMessage(long)}}}
	d := r.Decide(req, "gpt-4o")
	if d.Overridden {
		t.Fatalf("complex prompt should not be downgraded, got %+v", d)
	}
	if d.Bucket == ComplexitySimple {
		t.Fatalf("expected medium/complex bucket, got %s", d.Bucket)
	}
}
