package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/providers/anthropic"
	"github.com/gateway-llm/gateway-llm/internal/providers/azure"
	"github.com/gateway-llm/gateway-llm/internal/providers/bedrock"
	"github.com/gateway-llm/gateway-llm/internal/providers/cohere"
	"github.com/gateway-llm/gateway-llm/internal/providers/gemini"
	"github.com/gateway-llm/gateway-llm/internal/providers/groq"
	"github.com/gateway-llm/gateway-llm/internal/providers/mistral"
	"github.com/gateway-llm/gateway-llm/internal/providers/openai"
	"github.com/gateway-llm/gateway-llm/internal/providers/vertexai"
	"github.com/gateway-llm/gateway-llm/internal/providers/xai"
)

func TestListProviders(t *testing.T) {
	reg := providers.NewRegistry()
	reg.Register(openai.New())
	reg.Register(anthropic.New())
	reg.Register(gemini.New())
	reg.Register(azure.New())
	reg.Register(bedrock.New())
	reg.Register(cohere.New())
	reg.Register(groq.New())
	reg.Register(mistral.New())
	reg.Register(vertexai.New())
	reg.Register(xai.New())

	h := &Handlers{Registry: reg}

	req := httptest.NewRequest(http.MethodGet, "/v1/management/providers", nil)
	w := httptest.NewRecorder()
	h.ListProviders(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body struct {
		Data []ProviderInfo `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	expected := map[string]bool{
		"openai":    true,
		"anthropic": true,
		"gemini":    true,
		"azure":     false,
		"bedrock":   false,
		"cohere":    false,
		"groq":      false,
		"mistral":   false,
		"vertexai":  false,
		"xai":       false,
	}

	got := make(map[string]ProviderInfo, len(body.Data))
	for _, p := range body.Data {
		got[p.ID] = p
	}

	for id, wantDiscovery := range expected {
		info, ok := got[id]
		if !ok {
			t.Errorf("provider %q missing from response", id)
			continue
		}
		if info.SupportsDiscovery != wantDiscovery {
			t.Errorf("provider %q SupportsDiscovery = %v, want %v", id, info.SupportsDiscovery, wantDiscovery)
		}
		if info.Label == "" {
			t.Errorf("provider %q has empty label", id)
		}
		if info.ModelIDHint == "" {
			t.Errorf("provider %q has empty model_id_hint", id)
		}
	}
	if got["bedrock"].RequiresAPIBase != true {
		t.Errorf("bedrock RequiresAPIBase = false, want true")
	}
	if got["azure"].RequiresAPIBase != true {
		t.Errorf("azure RequiresAPIBase = false, want true")
	}
}

func TestListProvidersEmptyRegistry(t *testing.T) {
	h := &Handlers{Registry: providers.NewRegistry()}

	req := httptest.NewRequest(http.MethodGet, "/v1/management/providers", nil)
	w := httptest.NewRecorder()
	h.ListProviders(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body struct {
		Data []ProviderInfo `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 0 {
		t.Errorf("expected empty providers list, got %d", len(body.Data))
	}
}
