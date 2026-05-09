package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gateway-llm/gateway-llm/internal/providers"
)

type geminiModel struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

type geminiModelsResponse struct {
	Models []geminiModel `json:"models"`
}

func (p *GeminiProvider) DiscoverModels(ctx context.Context, apiKey string, apiBase string) ([]providers.DiscoveredModel, error) {
	base := defaultAPIBase
	if apiBase != "" {
		base = strings.TrimRight(apiBase, "/")
	}
	url := fmt.Sprintf("%s/v1beta/models", base)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gemini models API returned %d", resp.StatusCode)
	}

	var body geminiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var out []providers.DiscoveredModel
	for _, m := range body.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		caps := geminiCaps(m.SupportedGenerationMethods)
		if len(caps) == 0 {
			continue
		}
		out = append(out, providers.DiscoveredModel{
			ID:           id,
			Provider:     "gemini",
			OwnedBy:      "google",
			Capabilities: caps,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func geminiCaps(methods []string) []string {
	caps := make(map[string]bool)
	for _, m := range methods {
		switch m {
		case "generateContent":
			caps["chat"] = true
		case "embedContent", "batchEmbedContents":
			caps["embeddings"] = true
		case "countTokens":
			// utility, skip
		}
	}
	var out []string
	for c := range caps {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
