package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/gateway-llm/gateway-llm/internal/providers"
)

type anthropicModel struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type anthropicModelsResponse struct {
	Data []anthropicModel `json:"data"`
}

func (p *AnthropicProvider) DiscoverModels(ctx context.Context, apiKey string, apiBase string) ([]providers.DiscoveredModel, error) {
	base := defaultAnthropicBase
	if apiBase != "" {
		base = apiBase
	}
	url := base + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic models API returned %d", resp.StatusCode)
	}

	var body anthropicModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var out []providers.DiscoveredModel
	for _, m := range body.Data {
		out = append(out, providers.DiscoveredModel{
			ID:           m.ID,
			Provider:     "anthropic",
			OwnedBy:      "anthropic",
			Capabilities: []string{"chat"},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
