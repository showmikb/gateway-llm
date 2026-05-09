package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/gateway-llm/gateway-llm/internal/db"
	"go.uber.org/zap"
)

type ModelPricing struct {
	Provider              string             `json:"provider"`
	Model                 string             `json:"model"`
	Mode                  string             `json:"mode"`
	InputCostPerToken     float64            `json:"input_cost_per_token"`
	OutputCostPerToken    float64            `json:"output_cost_per_token"`
	InputCostPerImage     float64            `json:"input_cost_per_image,omitempty"`
	InputCostPerCharacter float64            `json:"input_cost_per_character,omitempty"`
	InputCostPerSecond    float64            `json:"input_cost_per_second,omitempty"`
	CacheReadCostPerToken float64            `json:"cache_read_cost_per_token,omitempty"`
	MaxInputTokens        int                `json:"max_input_tokens,omitempty"`
	MaxOutputTokens       int                `json:"max_output_tokens,omitempty"`
	SizePricing           map[string]float64 `json:"size_pricing,omitempty"`
}

type CostResult struct {
	InputCost        float64 `json:"input_cost"`
	OutputCost       float64 `json:"output_cost"`
	TotalCost        float64 `json:"total_cost"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Currency         string  `json:"currency"`
}

type UsageInfo struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	ImageCount       int
	ImageSize        string
	CharacterCount   int
	DurationSeconds  float64
}

type Engine struct {
	builtIn  map[string]*ModelPricing
	custom   map[string]*ModelPricing
	database *db.DB
	logger   *zap.Logger
	mu       sync.RWMutex

	// cat carries the operator-signed catalog and per-org countersigned
	// discounts. Populated by LoadOperatorCatalog and
	// LoadEffectiveDiscounts at startup; the catalog has its own
	// RWMutex so a hot reload doesn't block GetPricing.
	cat catalogState
}

func NewEngine(database *db.DB, logger *zap.Logger) *Engine {
	e := &Engine{
		builtIn:  make(map[string]*ModelPricing),
		custom:   make(map[string]*ModelPricing),
		database: database,
		logger:   logger,
	}
	e.cat.operator = make(map[string]*ModelPricing)
	e.cat.orgDiscounts = make(map[string]map[string]float64)
	e.loadBuiltIn()
	return e
}

func (e *Engine) loadBuiltIn() {
	var prices map[string]*ModelPricing
	if err := json.Unmarshal(builtInPrices, &prices); err != nil {
		e.logger.Error("failed to load built-in pricing", zap.Error(err))
		return
	}
	for key, p := range prices {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			p.Provider = parts[0]
			p.Model = parts[1]
		}
	}
	e.builtIn = prices
}

func (e *Engine) LoadCustomPricing(ctx context.Context) error {
	if e.database == nil {
		return nil
	}
	pricing, err := e.database.ListCustomPricing(ctx)
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.custom = make(map[string]*ModelPricing)
	for _, p := range pricing {
		key := p.Provider + "/" + p.Model
		mp := &ModelPricing{
			Provider: p.Provider,
			Model:    p.Model,
		}
		if p.InputCostPerToken != nil {
			mp.InputCostPerToken = *p.InputCostPerToken
		}
		if p.OutputCostPerToken != nil {
			mp.OutputCostPerToken = *p.OutputCostPerToken
		}
		if p.CacheReadCostPerToken != nil {
			mp.CacheReadCostPerToken = *p.CacheReadCostPerToken
		}
		if p.InputCostPerImage != nil {
			mp.InputCostPerImage = *p.InputCostPerImage
		}
		if p.InputCostPerCharacter != nil {
			mp.InputCostPerCharacter = *p.InputCostPerCharacter
		}
		if p.InputCostPerSecond != nil {
			mp.InputCostPerSecond = *p.InputCostPerSecond
		}
		mp.SizePricing = p.SizePricing
		e.custom[key] = mp
	}
	return nil
}

// GetPricing returns the active pricing entry for (provider, model).
// Precedence: operator catalog (signed billing-truth) → custom_pricing
// (legacy DB rows, kept for backwards compat) → embedded JSON. The
// operator catalog winning means org admins cannot deflate costs even
// if they hold the legacy AdminOnly endpoint.
func (e *Engine) GetPricing(provider, model string) *ModelPricing {
	key := provider + "/" + model

	e.cat.mu.RLock()
	if p, ok := e.cat.operator[key]; ok {
		e.cat.mu.RUnlock()
		return p
	}
	e.cat.mu.RUnlock()

	e.mu.RLock()
	if p, ok := e.custom[key]; ok {
		e.mu.RUnlock()
		return p
	}
	e.mu.RUnlock()

	if p, ok := e.builtIn[key]; ok {
		return p
	}
	return nil
}

// GetAllPricing returns the merged catalog the UI renders. Operator
// rows shadow custom rows shadow built-in.
func (e *Engine) GetAllPricing() map[string]*ModelPricing {
	result := make(map[string]*ModelPricing)

	for k, v := range e.builtIn {
		result[k] = v
	}

	e.mu.RLock()
	for k, v := range e.custom {
		result[k] = v
	}
	e.mu.RUnlock()

	e.cat.mu.RLock()
	for k, v := range e.cat.operator {
		result[k] = v
	}
	e.cat.mu.RUnlock()

	return result
}

func (e *Engine) Calculate(endpoint, provider, model string, usage UsageInfo) (*CostResult, error) {
	pricing := e.GetPricing(provider, model)

	result := &CostResult{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		Currency:         "USD",
	}

	if pricing == nil {
		return result, nil
	}

	switch endpoint {
	case "chat", "responses", "completions":
		result.InputCost = float64(usage.PromptTokens) * pricing.InputCostPerToken
		result.OutputCost = float64(usage.CompletionTokens) * pricing.OutputCostPerToken

		if usage.CachedTokens > 0 && pricing.CacheReadCostPerToken > 0 {
			savings := float64(usage.CachedTokens) * (pricing.InputCostPerToken - pricing.CacheReadCostPerToken)
			result.InputCost -= savings
		}

	case "embeddings":
		result.InputCost = float64(usage.PromptTokens) * pricing.InputCostPerToken

	case "images":
		n := usage.ImageCount
		if n <= 0 {
			n = 1
		}
		if usage.ImageSize != "" && pricing.SizePricing != nil {
			if price, ok := pricing.SizePricing[usage.ImageSize]; ok {
				result.InputCost = float64(n) * price
			} else {
				result.InputCost = float64(n) * pricing.InputCostPerImage
			}
		} else {
			result.InputCost = float64(n) * pricing.InputCostPerImage
		}

	case "audio/speech":
		result.InputCost = float64(usage.CharacterCount) * pricing.InputCostPerCharacter

	case "audio/transcriptions":
		result.InputCost = usage.DurationSeconds * pricing.InputCostPerSecond

	case "moderations":
		result.InputCost = float64(usage.PromptTokens) * pricing.InputCostPerToken

	default:
		return result, fmt.Errorf("unknown endpoint type: %s", endpoint)
	}

	result.TotalCost = result.InputCost + result.OutputCost
	return result, nil
}
