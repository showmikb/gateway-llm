package handlers

import (
	"net/http"
	"sort"

	"github.com/gateway-llm/gateway-llm/internal/providers"
)

// ProviderInfo is the metadata the UI needs to render provider pickers,
// model pickers, and credential creation forms without hardcoding lists.
type ProviderInfo struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	SupportsDiscovery bool   `json:"supports_discovery"`
	RequiresAPIBase   bool   `json:"requires_api_base"`
	APIKeyHint        string `json:"api_key_hint,omitempty"`
	APIBaseHint       string `json:"api_base_hint,omitempty"`
	ModelIDHint       string `json:"model_id_hint,omitempty"`
	DocsURL           string `json:"docs_url,omitempty"`
}

// providerCatalog holds the static, UI-facing metadata for every
// provider the gateway knows about. Discovery support is derived at
// request time from the registry so the catalog stays in sync with the
// real ModelDiscoveryProvider implementations.
//
// Adding a new provider only requires a new entry here plus the
// existing registration in server.New.
var providerCatalog = []ProviderInfo{
	{ID: "openai", Label: "OpenAI", APIKeyHint: "sk-...", ModelIDHint: "gpt-4o-mini", DocsURL: "https://platform.openai.com/docs/models"},
	{ID: "anthropic", Label: "Anthropic", APIKeyHint: "sk-ant-...", ModelIDHint: "claude-3-5-sonnet-20241022", DocsURL: "https://docs.anthropic.com/en/docs/about-claude/models"},
	{ID: "gemini", Label: "Google Gemini", APIKeyHint: "AIza...", ModelIDHint: "gemini-2.0-flash", DocsURL: "https://ai.google.dev/gemini-api/docs/models"},
	{ID: "azure", Label: "Azure OpenAI", RequiresAPIBase: true, APIKeyHint: "Azure resource key", APIBaseHint: "https://<your-resource>.openai.azure.com", ModelIDHint: "gpt-4o-mini", DocsURL: "https://learn.microsoft.com/azure/ai-services/openai/concepts/models"},
	{ID: "bedrock", Label: "AWS Bedrock", RequiresAPIBase: true, APIKeyHint: "Bedrock API key (Bearer token)", APIBaseHint: "https://bedrock-runtime.<region>.amazonaws.com", ModelIDHint: "anthropic.claude-3-5-sonnet-20241022-v2:0", DocsURL: "https://docs.aws.amazon.com/bedrock/latest/userguide/model-ids.html"},
	{ID: "cohere", Label: "Cohere", APIKeyHint: "Cohere API key", ModelIDHint: "command-r-plus", DocsURL: "https://docs.cohere.com/docs/models"},
	{ID: "groq", Label: "Groq", APIKeyHint: "gsk_...", ModelIDHint: "llama-3.3-70b-versatile", DocsURL: "https://console.groq.com/docs/models"},
	{ID: "mistral", Label: "Mistral", APIKeyHint: "Mistral API key", ModelIDHint: "mistral-large-latest", DocsURL: "https://docs.mistral.ai/getting-started/models/models_overview/"},
	{ID: "vertexai", Label: "Google Vertex AI", RequiresAPIBase: true, APIKeyHint: "Service account / OAuth token", APIBaseHint: "https://<region>-aiplatform.googleapis.com", ModelIDHint: "gemini-2.0-flash", DocsURL: "https://cloud.google.com/vertex-ai/generative-ai/docs/learn/models"},
	{ID: "xai", Label: "xAI", APIKeyHint: "xai-...", ModelIDHint: "grok-2-latest", DocsURL: "https://docs.x.ai/docs/models"},
}

// ListProviders returns provider metadata for every provider registered
// in the runtime registry. The supports_discovery flag is computed live
// against providers.ModelDiscoveryProvider so the UI never lies about
// what the gateway can do.
func (h *Handlers) ListProviders(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []ProviderInfo{}})
		return
	}

	registered := make(map[string]struct{}, 16)
	for _, name := range h.Registry.List() {
		registered[name] = struct{}{}
	}

	out := make([]ProviderInfo, 0, len(providerCatalog))
	for _, info := range providerCatalog {
		if _, ok := registered[info.ID]; !ok {
			continue
		}
		entry := info
		if prov, err := h.Registry.Get(info.ID); err == nil {
			if _, ok := prov.(providers.ModelDiscoveryProvider); ok {
				entry.SupportsDiscovery = true
			}
		}
		out = append(out, entry)
	}

	for name := range registered {
		known := false
		for _, info := range providerCatalog {
			if info.ID == name {
				known = true
				break
			}
		}
		if known {
			continue
		}
		entry := ProviderInfo{ID: name, Label: name}
		if prov, err := h.Registry.Get(name); err == nil {
			if _, ok := prov.(providers.ModelDiscoveryProvider); ok {
				entry.SupportsDiscovery = true
			}
		}
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}
