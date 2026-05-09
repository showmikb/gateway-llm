package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const sampleLitellm = `
model_list:
  - model_name: gpt-4o
    litellm_params:
      model: openai/gpt-4o
      api_key: os.environ/OPENAI_API_KEY
  - model_name: gpt-4o
    litellm_params:
      model: azure/prod-gpt-4o
      api_key: os.environ/AZURE_OPENAI_API_KEY
      api_base: https://mycompany.openai.azure.com/
  - model_name: claude-3-sonnet
    litellm_params:
      model: anthropic/claude-3-sonnet-20240229
      api_key: os.environ/ANTHROPIC_API_KEY
  - model_name: kitchen-sink
    litellm_params:
      model: bedrock/anthropic.claude-3-sonnet-20240229-v1:0

router_settings:
  routing_strategy: least-busy
  num_retries: 4

litellm_settings:
  master_key: os.environ/LITELLM_MASTER_KEY
`

func TestConvertGroupsAliases(t *testing.T) {
	var lcfg litellmConfig
	if err := yaml.Unmarshal([]byte(sampleLitellm), &lcfg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	g := convert(&lcfg)
	aliases := map[string]int{}
	for _, a := range g.ModelList {
		aliases[a.ModelAlias] = len(a.Deployments)
	}
	if aliases["gpt-4o"] != 2 {
		t.Errorf("gpt-4o should collapse two deployments, got %d", aliases["gpt-4o"])
	}
	if aliases["claude-3-sonnet"] != 1 {
		t.Errorf("claude-3-sonnet should have 1 deployment, got %d", aliases["claude-3-sonnet"])
	}
	if aliases["kitchen-sink"] != 1 {
		t.Errorf("kitchen-sink should have 1 deployment, got %d", aliases["kitchen-sink"])
	}
}

func TestConvertRoutingStrategy(t *testing.T) {
	var lcfg litellmConfig
	if err := yaml.Unmarshal([]byte(sampleLitellm), &lcfg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	g := convert(&lcfg)
	if g.Routing["strategy"] != "least-latency" {
		t.Errorf("expected least-latency, got %v", g.Routing["strategy"])
	}
	if g.Routing["retries"] != 4 {
		t.Errorf("expected retries=4, got %v", g.Routing["retries"])
	}
}

func TestConvertProviderInference(t *testing.T) {
	cases := []struct {
		in             string
		wantProvider   string
		wantModelStart string
	}{
		{"openai/gpt-4o", "openai", "gpt-4o"},
		{"azure/my-deployment", "openai", "my-deployment"},
		{"anthropic/claude-3-opus-20240229", "anthropic", "claude-3-opus"},
		{"gemini/gemini-1.5-pro", "gemini", "gemini-1.5-pro"},
		{"vertex_ai/gemini-1.5-pro", "gemini", "gemini-1.5-pro"},
		{"bedrock/anthropic.claude-3-sonnet-20240229-v1:0", "bedrock", "anthropic.claude-3-sonnet"},
		{"together_ai/meta-llama/Llama-3-70b", "together", "meta-llama/Llama-3-70b"},
		{"gpt-4o", "openai", "gpt-4o"},
	}
	for _, c := range cases {
		p, m := splitProviderModel(c.in)
		if p != c.wantProvider {
			t.Errorf("%s: provider want %s, got %s", c.in, c.wantProvider, p)
		}
		if !strings.HasPrefix(m, c.wantModelStart) {
			t.Errorf("%s: model should start with %s, got %s", c.in, c.wantModelStart, m)
		}
	}
}
