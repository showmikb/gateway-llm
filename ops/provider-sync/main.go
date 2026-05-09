// provider-sync is a nightly job that refreshes Gateway-LLM's pricing
// and context-window catalog. It fetches upstream sources, diffs them
// against the embedded model_prices.json, and emits two artifacts:
//
//   * backend/internal/cost/model_prices.json (updated in place)
//   * ops/provider-sync/diff.md                (human-readable changelog)
//
// A GitHub Actions workflow invokes this program and opens a pull
// request when the diff is non-empty. The goal is zero-toil pricing
// updates so gateway-llm's cost engine never drifts from reality, which
// is the foundation of the FinOps moat.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// sources holds the upstream catalogs to pull. LiteLLM publishes the
// most comprehensive public pricing index, so we use it as the primary.
// Additional provider-native endpoints can be added over time.
var sources = []source{
	{
		Name: "litellm",
		URL:  "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
		// LiteLLM uses `litellm_provider`, normalize to `openai/model` etc.
		Normalize: normalizeLiteLLM,
	},
}

type source struct {
	Name      string
	URL       string
	Normalize func(raw map[string]any) map[string]ModelPrice
}

// ModelPrice mirrors the subset of fields gateway-llm actually consumes.
type ModelPrice struct {
	Mode                string  `json:"mode,omitempty"`
	InputCostPerToken   float64 `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken  float64 `json:"output_cost_per_token,omitempty"`
	CacheReadCost       float64 `json:"cache_read_cost_per_token,omitempty"`
	MaxInputTokens      int     `json:"max_input_tokens,omitempty"`
	MaxOutputTokens     int     `json:"max_output_tokens,omitempty"`
	SupportsVision      bool    `json:"supports_vision,omitempty"`
	SupportsFnCalling   bool    `json:"supports_function_calling,omitempty"`
	SupportsPromptCache bool    `json:"supports_prompt_caching,omitempty"`
}

func main() {
	var (
		target = flag.String("target", "backend/internal/cost/model_prices.json", "path to pricing JSON")
		diff   = flag.String("diff", "ops/provider-sync/diff.md", "path to write human diff")
		dry    = flag.Bool("dry-run", false, "print diff, do not write")
	)
	flag.Parse()

	existing, err := loadJSON(*target)
	if err != nil {
		fatalf("read target: %v", err)
	}

	fresh := map[string]ModelPrice{}
	for _, s := range sources {
		raw, err := fetch(s.URL)
		if err != nil {
			warnf("source %s fetch: %v", s.Name, err)
			continue
		}
		norm := s.Normalize(raw)
		for k, v := range norm {
			// First writer wins; sources are ordered by trust.
			if _, ok := fresh[k]; !ok {
				fresh[k] = v
			}
		}
	}

	if len(fresh) == 0 {
		fatalf("no pricing data fetched; aborting")
	}

	changes := diffMaps(existing, fresh)
	report := renderDiff(changes)
	fmt.Print(report)

	if *dry {
		return
	}

	// Write merged catalog: keep all existing entries, overlay fresh
	// values, but never drop an entry we already ship (defensive against
	// upstream regressions).
	merged := make(map[string]ModelPrice, len(existing))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range fresh {
		merged[k] = v
	}

	if err := writeJSON(*target, merged); err != nil {
		fatalf("write target: %v", err)
	}
	if err := os.WriteFile(*diff, []byte(report), 0o644); err != nil {
		fatalf("write diff: %v", err)
	}
	fmt.Fprintf(os.Stderr, "wrote %d models to %s (%d changed)\n", len(merged), *target, len(changes))
}

func fetch(url string) (map[string]any, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func loadJSON(path string) (map[string]ModelPrice, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]ModelPrice
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func writeJSON(path string, data map[string]ModelPrice) error {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ord := make(map[string]ModelPrice, len(keys))
	for _, k := range keys {
		ord[k] = data[k]
	}
	b, err := json.MarshalIndent(ord, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// normalizeLiteLLM converts LiteLLM's pricing shape into ours. LiteLLM
// keys look like "gpt-4o" with a `litellm_provider` field; we store as
// "openai/gpt-4o" so the cost engine can key on the same string the
// provider bridge uses.
func normalizeLiteLLM(raw map[string]any) map[string]ModelPrice {
	out := map[string]ModelPrice{}
	for k, vAny := range raw {
		if k == "sample_spec" {
			continue
		}
		m, ok := vAny.(map[string]any)
		if !ok {
			continue
		}
		provider, _ := m["litellm_provider"].(string)
		if provider == "" {
			// Keep the raw key as a fallback.
			out[k] = toModelPrice(m)
			continue
		}
		// Strip provider-specific prefixes that sometimes sneak in.
		alias := strings.TrimPrefix(k, provider+"/")
		out[provider+"/"+alias] = toModelPrice(m)
	}
	return out
}

func toModelPrice(m map[string]any) ModelPrice {
	return ModelPrice{
		Mode:                str(m["mode"]),
		InputCostPerToken:   f(m["input_cost_per_token"]),
		OutputCostPerToken:  f(m["output_cost_per_token"]),
		CacheReadCost:       f(m["cache_read_input_token_cost"]),
		MaxInputTokens:      i(m["max_input_tokens"]),
		MaxOutputTokens:     i(m["max_output_tokens"]),
		SupportsVision:      b(m["supports_vision"]),
		SupportsFnCalling:   b(m["supports_function_calling"]),
		SupportsPromptCache: b(m["supports_prompt_caching"]),
	}
}

func str(v any) string { s, _ := v.(string); return s }
func b(v any) bool     { x, _ := v.(bool); return x }
func f(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}
func i(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	}
	return 0
}

// change represents a pricing delta for one model alias.
type change struct {
	Key   string
	Old   *ModelPrice
	New   ModelPrice
	Kind  string // added | updated | removed
	Notes []string
}

func diffMaps(old, new map[string]ModelPrice) []change {
	var out []change
	for k, n := range new {
		o, ok := old[k]
		if !ok {
			out = append(out, change{Key: k, New: n, Kind: "added"})
			continue
		}
		if !equalPrice(o, n) {
			c := change{Key: k, Old: &o, New: n, Kind: "updated"}
			if o.InputCostPerToken != n.InputCostPerToken {
				c.Notes = append(c.Notes, fmt.Sprintf("input: %g -> %g", o.InputCostPerToken, n.InputCostPerToken))
			}
			if o.OutputCostPerToken != n.OutputCostPerToken {
				c.Notes = append(c.Notes, fmt.Sprintf("output: %g -> %g", o.OutputCostPerToken, n.OutputCostPerToken))
			}
			if o.MaxInputTokens != n.MaxInputTokens {
				c.Notes = append(c.Notes, fmt.Sprintf("ctx in: %d -> %d", o.MaxInputTokens, n.MaxInputTokens))
			}
			if o.MaxOutputTokens != n.MaxOutputTokens {
				c.Notes = append(c.Notes, fmt.Sprintf("ctx out: %d -> %d", o.MaxOutputTokens, n.MaxOutputTokens))
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func equalPrice(a, b ModelPrice) bool {
	return a.Mode == b.Mode &&
		a.InputCostPerToken == b.InputCostPerToken &&
		a.OutputCostPerToken == b.OutputCostPerToken &&
		a.CacheReadCost == b.CacheReadCost &&
		a.MaxInputTokens == b.MaxInputTokens &&
		a.MaxOutputTokens == b.MaxOutputTokens
}

func renderDiff(c []change) string {
	if len(c) == 0 {
		return "# provider-sync: no changes\n"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# provider-sync — %d change(s)\n\n", len(c)))
	sb.WriteString(fmt.Sprintf("_generated %s_\n\n", time.Now().UTC().Format(time.RFC3339)))
	for _, ch := range c {
		sb.WriteString(fmt.Sprintf("## %s (%s)\n", ch.Key, ch.Kind))
		for _, n := range ch.Notes {
			sb.WriteString("- ")
			sb.WriteString(n)
			sb.WriteString("\n")
		}
		if ch.Kind == "added" {
			sb.WriteString(fmt.Sprintf("- input=%g output=%g ctx=%d/%d\n",
				ch.New.InputCostPerToken, ch.New.OutputCostPerToken,
				ch.New.MaxInputTokens, ch.New.MaxOutputTokens))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "provider-sync: "+format+"\n", args...)
	os.Exit(1)
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warn: "+format+"\n", args...)
}
