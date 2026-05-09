package semcache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// OpenAIEmbedder calls the OpenAI /v1/embeddings endpoint to produce
// dense vectors suitable for high-quality semantic similarity.
type OpenAIEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
	logger  *zap.Logger
}

// NewOpenAIEmbedder creates an embedder backed by the OpenAI embeddings
// API. model should be e.g. "text-embedding-3-small".
func NewOpenAIEmbedder(apiKey, model string, logger *zap.Logger) *OpenAIEmbedder {
	return NewOpenAIEmbedderWithBase(apiKey, model, "https://api.openai.com", logger)
}

// NewOpenAIEmbedderWithBase is like NewOpenAIEmbedder but allows
// overriding the API base URL (useful for tests and proxies).
func NewOpenAIEmbedderWithBase(apiKey, model, baseURL string, logger *zap.Logger) *OpenAIEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
		logger:  logger,
	}
}

type embeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if text == "" {
		return nil, nil
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		vec, err := e.doEmbed(ctx, text)
		if err == nil {
			return vec, nil
		}
		lastErr = err
		if attempt == 0 {
			if e.logger != nil {
				e.logger.Debug("openai embedding retry", zap.Error(err))
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil, lastErr
}

func (e *OpenAIEmbedder) doEmbed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(embeddingRequest{Input: text, Model: e.model})
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding HTTP call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("embedding API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed embeddingResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse embedding response: %w", err)
	}

	if parsed.Error != nil {
		return nil, fmt.Errorf("embedding API error: %s", parsed.Error.Message)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API returned %d", resp.StatusCode)
	}

	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding response has no vectors")
	}

	vec := parsed.Data[0].Embedding
	l2Normalize(vec)
	return vec, nil
}

func l2Normalize(vec []float64) {
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vec {
			vec[i] /= norm
		}
	}
}
