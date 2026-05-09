package router

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/providers"
	"go.uber.org/zap"
)

// litellmPrefixes maps the provider-prefix format used by LiteLLM (and now
// many customer configs) to our internal provider name. If an incoming
// request uses "anthropic/claude-3-opus", we strip the prefix and look up a
// deployment that matches the provider + model.
var litellmPrefixes = map[string]string{
	"openai":       "openai",
	"azure":        "openai",
	"anthropic":    "anthropic",
	"claude":       "anthropic",
	"gemini":       "gemini",
	"vertex_ai":    "gemini",
	"vertex":       "gemini",
	"google":       "gemini",
	"bedrock":      "bedrock",
	"mistral":      "mistral",
	"cohere":       "cohere",
	"groq":         "groq",
	"together_ai":  "together",
	"together":     "together",
	"fireworks_ai": "fireworks",
	"fireworks":    "fireworks",
	"deepseek":     "deepseek",
	"xai":          "xai",
	"ollama":       "ollama",
	"huggingface":  "huggingface",
}

type DeploymentInfo struct {
	Provider      string
	ProviderModel string
	APIKey        string
	APIBase       string
	Priority      int
	OrgID         *uuid.UUID

	// Weight controls traffic share when multiple deployments serve the
	// same alias. 0 or 1 are treated as default weight. Used by A/B
	// testing / variants.
	Weight int
	// VariantID optionally tags a deployment as a named variant (e.g.
	// "control", "gpt4-with-cot") for experimentation analytics.
	VariantID string
	// RoutingStrategy is the per-alias strategy. All deployments sharing
	// a model_alias should agree on this value — the router reads it off
	// the first deployment in the group and falls back to the global
	// router strategy if empty.
	RoutingStrategy string

	latencyEWMA atomic.Int64
	failures    atomic.Int32
	breaker     *CircuitBreaker
}

// PricingLookup resolves per-token input+output cost for a (provider, model)
// tuple. Returns (0, 0, false) if pricing is unknown for the tuple. It is
// injected at router construction so the router does not import the cost
// package directly (keeps the hot path allocation-free).
type PricingLookup func(provider, model string) (inputPerToken, outputPerToken float64, ok bool)

// Breaker returns (and lazy-creates) the per-deployment circuit breaker.
// We lazy-init so deployments reloaded from the DB don't need to know
// about the breaker type.
func (d *DeploymentInfo) Breaker() *CircuitBreaker {
	if d.breaker == nil {
		d.breaker = NewCircuitBreaker(5, 30*time.Second)
	}
	return d.breaker
}

type ModelRouter struct {
	aliases  map[string][]*DeploymentInfo
	strategy string
	counters map[string]*atomic.Uint64 // round-robin counters per alias
	// orgIndex is a pre-built per-alias index keyed by org UUID, computed on
	// Reload so the hot path of ResolveForOrg is a pure map lookup instead of
	// an O(n) scan + append. The special key `uuid.Nil` stores the slice of
	// global (non-org-scoped) deployments that serve as the fallback.
	orgIndex map[string]map[uuid.UUID][]*DeploymentInfo
	registry *providers.Registry
	logger   *zap.Logger
	mu       sync.RWMutex
	cfg      config.RoutingConfig
	pricing  PricingLookup
}

// SetPricingLookup wires in a cost-lookup function used by the "cheapest"
// routing strategy. Safe to call at startup before traffic flows.
func (r *ModelRouter) SetPricingLookup(fn PricingLookup) {
	r.mu.Lock()
	r.pricing = fn
	r.mu.Unlock()
}

func New(modelList []config.ModelAlias, routingCfg config.RoutingConfig, registry *providers.Registry, logger *zap.Logger) *ModelRouter {
	r := &ModelRouter{
		aliases:  make(map[string][]*DeploymentInfo),
		counters: make(map[string]*atomic.Uint64),
		orgIndex: make(map[string]map[uuid.UUID][]*DeploymentInfo),
		strategy: routingCfg.Strategy,
		registry: registry,
		logger:   logger,
		cfg:      routingCfg,
	}

	for _, alias := range modelList {
		var deps []*DeploymentInfo
		for _, d := range alias.Deployments {
			apiKey := os.Getenv(d.APIKeyEnv)
			if apiKey == "" {
				logger.Warn("API key env var not set", zap.String("env", d.APIKeyEnv), zap.String("alias", alias.ModelAlias))
			}
			deps = append(deps, &DeploymentInfo{
				Provider:      d.Provider,
				ProviderModel: d.Model,
				APIKey:        apiKey,
				APIBase:       d.APIBase,
				Priority:      d.Priority,
			})
		}
		r.aliases[alias.ModelAlias] = deps
		r.counters[alias.ModelAlias] = &atomic.Uint64{}
		r.orgIndex[alias.ModelAlias] = buildOrgIndex(deps)
	}

	return r
}

// buildOrgIndex groups a deployment slice by org UUID so ResolveForOrg can
// answer with a single map lookup. Global (nil-OrgID) deployments are stored
// under uuid.Nil and serve as the fallback when an org has no dedicated
// deployments for an alias.
func buildOrgIndex(deps []*DeploymentInfo) map[uuid.UUID][]*DeploymentInfo {
	idx := make(map[uuid.UUID][]*DeploymentInfo, 4)
	for _, d := range deps {
		var key uuid.UUID
		if d.OrgID != nil {
			key = *d.OrgID
		}
		idx[key] = append(idx[key], d)
	}
	return idx
}

func (r *ModelRouter) Resolve(modelAlias string) ([]*DeploymentInfo, error) {
	r.mu.RLock()
	deps, ok := r.aliases[modelAlias]
	r.mu.RUnlock()

	if ok && len(deps) > 0 {
		return r.order(modelAlias, deps), nil
	}

	// LiteLLM compatibility path: "anthropic/claude-3-opus-20240229" or
	// "bedrock/anthropic.claude-3-sonnet-20240229-v1:0" — find a deployment
	// whose (provider, provider_model) matches.
	if prov, model, ok := splitLitellmPrefix(modelAlias); ok {
		if deps := r.resolveByProviderModel(prov, model); len(deps) > 0 {
			return r.order(modelAlias, deps), nil
		}
	}

	return nil, fmt.Errorf("model %q not found", modelAlias)
}

// ResolveCanonical returns the canonical alias for a possibly-prefixed model
// name. Used by handlers so spend logs and records use a stable alias.
func (r *ModelRouter) ResolveCanonical(modelAlias string) string {
	r.mu.RLock()
	_, ok := r.aliases[modelAlias]
	r.mu.RUnlock()
	if ok {
		return modelAlias
	}
	if prov, model, ok := splitLitellmPrefix(modelAlias); ok {
		if deps := r.resolveByProviderModel(prov, model); len(deps) > 0 {
			// Use the first alias found that exposes this deployment; fall
			// back to "provider/model" so the alias is stable.
			if name := r.aliasNameForDeployment(prov, model); name != "" {
				return name
			}
			return prov + "/" + model
		}
	}
	return modelAlias
}

func splitLitellmPrefix(modelAlias string) (provider, model string, ok bool) {
	idx := strings.Index(modelAlias, "/")
	if idx <= 0 {
		return "", "", false
	}
	prefix := modelAlias[:idx]
	canonical, known := litellmPrefixes[prefix]
	if !known {
		return "", "", false
	}
	return canonical, modelAlias[idx+1:], true
}

func (r *ModelRouter) resolveByProviderModel(provider, model string) []*DeploymentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, deps := range r.aliases {
		for _, d := range deps {
			if d.Provider == provider && d.ProviderModel == model {
				return []*DeploymentInfo{d}
			}
		}
	}
	return nil
}

func (r *ModelRouter) aliasNameForDeployment(provider, model string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for alias, deps := range r.aliases {
		for _, d := range deps {
			if d.Provider == provider && d.ProviderModel == model {
				return alias
			}
		}
	}
	return ""
}

func (r *ModelRouter) ResolveForOrg(orgID *uuid.UUID, modelAlias string) ([]*DeploymentInfo, error) {
	r.mu.RLock()
	index, ok := r.orgIndex[modelAlias]
	r.mu.RUnlock()

	if !ok || len(index) == 0 {
		// Fall back to the LiteLLM prefix path which resolves against
		// (provider, provider_model) across the whole catalog. Org scoping
		// is preserved by filtering to the org-scoped deployments below.
		if deps, err := r.Resolve(modelAlias); err == nil && len(deps) > 0 {
			if orgID == nil {
				return deps, nil
			}
			// Keep only deployments that are global or match this org.
			filtered := make([]*DeploymentInfo, 0, len(deps))
			for _, d := range deps {
				if d.OrgID == nil || *d.OrgID == *orgID {
					filtered = append(filtered, d)
				}
			}
			if len(filtered) > 0 {
				return filtered, nil
			}
		}
		return nil, fmt.Errorf("model %q not found", modelAlias)
	}

	// No org context -> flatten: prefer all deployments for this alias. Since
	// we don't want to rebuild the slice each call, fall back to the aliases
	// map which is a single pointer deref.
	if orgID == nil {
		r.mu.RLock()
		deps := r.aliases[modelAlias]
		r.mu.RUnlock()
		return r.order(modelAlias, deps), nil
	}

	if orgDeps, hit := index[*orgID]; hit && len(orgDeps) > 0 {
		return r.order(modelAlias, orgDeps), nil
	}

	if globalDeps, hit := index[uuid.Nil]; hit && len(globalDeps) > 0 {
		return r.order(modelAlias, globalDeps), nil
	}

	return nil, fmt.Errorf("model %q not found for this organization", modelAlias)
}

func (r *ModelRouter) order(alias string, deps []*DeploymentInfo) []*DeploymentInfo {
	if len(deps) <= 1 {
		return deps
	}

	// Per-alias strategy read off the first deployment — all targets
	// sharing an alias are expected to agree (enforced at write time
	// by the deployments handler).
	strategy := deps[0].RoutingStrategy
	if strategy == "" {
		strategy = r.strategy
	}

	switch strategy {
	case "least-latency":
		return r.leastLatency(deps)
	case "weighted", "variant", "ab":
		return r.weighted(alias, deps)
	case "priority":
		return r.priorityOrdered(alias, deps)
	case "cheapest":
		return r.cheapest(alias, deps)
	default: // round-robin
		return r.roundRobin(alias, deps)
	}
}

// priorityOrdered returns the deployment slice sorted by Priority ASC (lower
// first). Deployments with equal priority are rotated via the round-robin
// counter so ties don't always route to the same backend.
func (r *ModelRouter) priorityOrdered(alias string, deps []*DeploymentInfo) []*DeploymentInfo {
	n := len(deps)
	ordered := make([]*DeploymentInfo, n)
	copy(ordered, deps)

	for i := 1; i < n; i++ {
		for j := i; j > 0; j-- {
			if ordered[j].Priority < ordered[j-1].Priority {
				ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
			} else {
				break
			}
		}
	}

	// Tie-break groups of equal Priority using the alias RR counter so the
	// same-priority bucket is rotated fairly across requests.
	counter := r.counters[alias]
	if counter != nil {
		start := 0
		for start < n {
			end := start + 1
			for end < n && ordered[end].Priority == ordered[start].Priority {
				end++
			}
			if end-start > 1 {
				idx := int(counter.Add(1)-1) % (end - start)
				group := append([]*DeploymentInfo(nil), ordered[start:end]...)
				for i := 0; i < end-start; i++ {
					ordered[start+i] = group[(idx+i)%(end-start)]
				}
			}
			start = end
		}
	}
	return ordered
}

// cheapest sorts deployments by the sum of per-token input+output cost. When
// pricing is unavailable (missing catalog entry or no PricingLookup wired in)
// the target is pushed to the end of the slice so it still serves as a
// fallback. Ties fall back to round-robin rotation.
func (r *ModelRouter) cheapest(alias string, deps []*DeploymentInfo) []*DeploymentInfo {
	n := len(deps)
	type scored struct {
		dep   *DeploymentInfo
		cost  float64
		known bool
		idx   int
	}
	scoredList := make([]scored, n)
	for i, d := range deps {
		s := scored{dep: d, idx: i}
		if r.pricing != nil {
			if in, out, ok := r.pricing(d.Provider, d.ProviderModel); ok {
				s.cost = in + out
				s.known = true
			}
		}
		scoredList[i] = s
	}

	// Insertion sort: known cheapest first, unknown to the end (preserving
	// input order for unknowns to keep results stable).
	for i := 1; i < n; i++ {
		for j := i; j > 0; j-- {
			a, b := scoredList[j], scoredList[j-1]
			better := false
			switch {
			case a.known && !b.known:
				better = true
			case !a.known && !b.known:
				better = a.idx < b.idx
			case a.known && b.known:
				better = a.cost < b.cost
			}
			if better {
				scoredList[j], scoredList[j-1] = scoredList[j-1], scoredList[j]
			} else {
				break
			}
		}
	}

	ordered := make([]*DeploymentInfo, n)
	for i, s := range scoredList {
		ordered[i] = s.dep
	}

	// Rotate runs of equal-cost known targets via RR so we don't always
	// pin to the same deployment when two models price identically.
	counter := r.counters[alias]
	if counter != nil {
		start := 0
		for start < n {
			end := start + 1
			for end < n && scoredList[end].known == scoredList[start].known &&
				scoredList[end].cost == scoredList[start].cost {
				end++
			}
			if end-start > 1 && scoredList[start].known {
				idx := int(counter.Add(1)-1) % (end - start)
				group := append([]*DeploymentInfo(nil), ordered[start:end]...)
				for i := 0; i < end-start; i++ {
					ordered[start+i] = group[(idx+i)%(end-start)]
				}
			}
			start = end
		}
	}
	return ordered
}

// weighted implements deterministic weighted sampling for A/B testing
// and variant traffic. Each deployment's Weight (default 1) contributes
// to its probability of being chosen as the primary, with the remaining
// deployments preserved for fallback so reliability is not sacrificed
// for experimentation. When all weights are 1 this degenerates to
// roundRobin, which is the intended invariant for users who add variants
// without specifying weights yet.
func (r *ModelRouter) weighted(alias string, deps []*DeploymentInfo) []*DeploymentInfo {
	total := 0
	weights := make([]int, len(deps))
	for i, d := range deps {
		w := d.Weight
		if w <= 0 {
			w = 1
		}
		weights[i] = w
		total += w
	}
	if total <= len(deps) {
		// all weights are effectively default -> preserve existing RR
		// behavior so switching strategy doesn't change anything.
		return r.roundRobin(alias, deps)
	}

	pick := rand.Intn(total) //nolint:gosec
	primary := 0
	for i, w := range weights {
		if pick < w {
			primary = i
			break
		}
		pick -= w
	}

	ordered := make([]*DeploymentInfo, 0, len(deps))
	ordered = append(ordered, deps[primary])
	for i := range deps {
		if i != primary {
			ordered = append(ordered, deps[i])
		}
	}
	return ordered
}

func (r *ModelRouter) roundRobin(alias string, deps []*DeploymentInfo) []*DeploymentInfo {
	counter := r.counters[alias]
	idx := counter.Add(1) - 1
	n := uint64(len(deps))

	ordered := make([]*DeploymentInfo, len(deps))
	for i := range deps {
		ordered[i] = deps[(int(idx)+i)%int(n)]
	}
	return ordered
}

func (r *ModelRouter) leastLatency(deps []*DeploymentInfo) []*DeploymentInfo {
	n := len(deps)
	ordered := make([]*DeploymentInfo, n)
	copy(ordered, deps)

	// Snapshot atomic latency values once so the inner sort loop does O(n^2)
	// stack comparisons instead of O(n^2) atomic loads, which dominate cost
	// on ARM. For <=32 deployments we keep the scratch on the stack.
	var stack [32]int64
	var latencies []int64
	if n <= len(stack) {
		latencies = stack[:n]
	} else {
		latencies = make([]int64, n)
	}
	for i, d := range ordered {
		latencies[i] = d.latencyEWMA.Load()
	}

	// Insertion sort by latency ascending with a small jitter so ties don't
	// cause thundering-herd routing to the same deployment.
	for i := 1; i < n; i++ {
		for j := i; j > 0; j-- {
			jitter := rand.Int63n(10) //nolint:gosec
			if latencies[j]+jitter < latencies[j-1] {
				ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
				latencies[j], latencies[j-1] = latencies[j-1], latencies[j]
			} else {
				break
			}
		}
	}
	return ordered
}

func (r *ModelRouter) ReportLatency(d *DeploymentInfo, latency time.Duration) {
	ms := latency.Milliseconds()
	old := d.latencyEWMA.Load()
	if old == 0 {
		d.latencyEWMA.Store(ms)
		return
	}
	// EWMA with alpha=0.3
	newVal := int64(float64(old)*0.7 + float64(ms)*0.3)
	d.latencyEWMA.Store(newVal)
}

func (r *ModelRouter) ReportFailure(d *DeploymentInfo) {
	d.failures.Add(1)
	d.Breaker().ReportFailure()
}

func (r *ModelRouter) ReportSuccess(d *DeploymentInfo) {
	d.failures.Store(0)
	d.Breaker().ReportSuccess()
}

func (r *ModelRouter) ListAliases() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	aliases := make([]string, 0, len(r.aliases))
	for a := range r.aliases {
		aliases = append(aliases, a)
	}
	return aliases
}

func (r *ModelRouter) GetDeployments(alias string) []*DeploymentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.aliases[alias]
}

func (r *ModelRouter) Config() config.RoutingConfig {
	return r.cfg
}

func (r *ModelRouter) Reload(aliases map[string][]*DeploymentInfo) {
	newIndex := make(map[string]map[uuid.UUID][]*DeploymentInfo, len(aliases))
	for alias, deps := range aliases {
		newIndex[alias] = buildOrgIndex(deps)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.aliases = aliases
	r.orgIndex = newIndex
	for alias := range aliases {
		if _, ok := r.counters[alias]; !ok {
			r.counters[alias] = &atomic.Uint64{}
		}
	}
	for alias := range r.counters {
		if _, ok := aliases[alias]; !ok {
			delete(r.counters, alias)
		}
	}
}
