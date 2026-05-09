package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gateway-llm/gateway-llm/internal/providers"
)

type openAIModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

type openAIModelsResponse struct {
	Data []openAIModel `json:"data"`
}

func (p *OpenAIProvider) DiscoverModels(ctx context.Context, apiKey string, apiBase string) ([]providers.DiscoveredModel, error) {
	url := baseURL(apiBase) + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai models API returned %d", resp.StatusCode)
	}

	var body openAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var out []providers.DiscoveredModel
	for _, m := range body.Data {
		caps := inferOpenAICaps(m.ID)
		if len(caps) == 0 {
			continue
		}
		out = append(out, providers.DiscoveredModel{
			ID:           m.ID,
			Provider:     "openai",
			OwnedBy:      m.OwnedBy,
			Capabilities: caps,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func inferOpenAICaps(id string) []string {
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4") || strings.HasPrefix(lower, "chatgpt"):
		return []string{"chat"}
	case strings.HasPrefix(lower, "text-embedding"):
		return []string{"embeddings"}
	case strings.HasPrefix(lower, "dall-e"):
		return []string{"images"}
	case strings.HasPrefix(lower, "tts"):
		return []string{"audio_speech"}
	case strings.HasPrefix(lower, "whisper"):
		return []string{"audio_transcribe"}
	case strings.Contains(lower, "moderation"):
		return []string{"moderations"}
	case strings.HasPrefix(lower, "codex"):
		return []string{"chat"}
	default:
		return nil
	}
}
