// Package observability owns the gateway's Prometheus surface.
//
// We register a private *prometheus.Registry rather than using the global
// default so we don't accidentally export collectors registered by client
// libraries we depend on (e.g. the gRPC transport's keep-alive metrics)
// which would leak random labels into every scrape.
//
// All counters here are pull-based (Prometheus scrapes /metrics) so we
// rely on the existing daily_spend rollups for durable totals; resets
// across restart are expected and Prometheus' rate() handles them
// correctly.
package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "gatewayllm"

// metrics groups every collector the gateway publishes. We expose them as
// package-level functions (RecordRequest, etc.) so call sites stay free of
// any prometheus import; that keeps the rest of the codebase test-cheap.
type metrics struct {
	registry *prometheus.Registry

	requests          *prometheus.CounterVec
	requestDuration   *prometheus.HistogramVec
	tokens            *prometheus.CounterVec
	cost              *prometheus.CounterVec
	cacheHit          *prometheus.CounterVec
	cacheMiss         *prometheus.CounterVec
	guardrailBlock    *prometheus.CounterVec
	rateLimitHit      *prometheus.CounterVec
	smartRouteDecide  *prometheus.CounterVec
	circuitBreakerOpn *prometheus.GaugeVec

	// Smart-routing moat metrics. Labels deliberately exclude
	// trace_id / api_key_id to keep cardinality bounded; org_id is
	// optional but capped by tenancy. served_alias may equal alias
	// (no override) or differ when smart routing kicked in.
	moatRequests       *prometheus.CounterVec
	moatCostUSD        *prometheus.CounterVec
	moatBaselineUSD    *prometheus.CounterVec
	moatSavingsUSD     *prometheus.CounterVec
	moatQualityScore   *prometheus.HistogramVec
	moatJudgePasses    *prometheus.CounterVec
	moatRoutingOutcome *prometheus.CounterVec
}

var m = newMetrics()

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	// Re-register the standard runtime + process collectors so operators
	// get go_goroutines, process_resident_memory_bytes, etc. without us
	// having to wire them ourselves.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	out := &metrics{registry: reg}

	out.requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "requests_total",
		Help:      "Total LLM requests served, labelled by model alias, upstream provider, and HTTP status class.",
	}, []string{"model", "provider", "status"})

	// Buckets cover a typical chat completion (sub-second) up through
	// long-running reasoning calls (~120s). Tweak via PR if real-world
	// p99s land outside the top bucket.
	out.requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "request_duration_seconds",
		Help:      "End-to-end request duration in seconds, from the moment the gateway dispatched to the upstream provider until the response was finalised.",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	}, []string{"model", "provider"})

	out.tokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tokens_total",
		Help:      "Total tokens billed by upstream providers (kind = prompt | completion).",
	}, []string{"model", "provider", "kind"})

	out.cost = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "cost_usd_total",
		Help:      "Total upstream spend in USD, computed from the gateway's pricing table at request time.",
	}, []string{"model", "provider"})

	out.cacheHit = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "cache_hit_total",
		Help:      "Cache hits served from the response cache (type = exact | semantic).",
	}, []string{"type"})

	out.cacheMiss = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "cache_miss_total",
		Help:      "Cache lookups that fell through to the upstream provider (type = exact | semantic).",
	}, []string{"type"})

	out.guardrailBlock = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "guardrail_block_total",
		Help:      "Requests blocked by the guardrails engine, grouped by reason (e.g. prompt_injection, blocked_pattern).",
	}, []string{"reason"})

	out.rateLimitHit = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "rate_limit_hit_total",
		Help:      "Requests rejected by the rate limiter (kind = rpm | tpm).",
	}, []string{"kind"})

	out.smartRouteDecide = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "smart_route_decisions_total",
		Help:      "SmartRoute classifier decisions, labelled by complexity bucket and whether the alias was overridden.",
	}, []string{"bucket", "override"})

	out.circuitBreakerOpn = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "circuit_breaker_open",
		Help:      "1 when the circuit breaker for a (deployment, provider) pair is open, 0 otherwise.",
	}, []string{"deployment", "provider"})

	// ---- Smart-routing moat metrics ----
	moatLabels := []string{"alias", "served_alias", "strategy", "retried", "org_id"}

	out.moatRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "moat_requests_total",
		Help:      "Smart-routing-aware request counter, labelled by requested alias, served alias, policy strategy, retry flag, and org.",
	}, moatLabels)

	out.moatCostUSD = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "moat_cost_usd_total",
		Help:      "Actual upstream cost in USD as charged after smart routing + negotiated discounts.",
	}, []string{"alias", "served_alias", "org_id"})

	out.moatBaselineUSD = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "moat_baseline_cost_usd_total",
		Help:      "Cost in USD that would have been billed had the request hit the baseline (un-routed) model.",
	}, []string{"alias", "served_alias", "org_id"})

	out.moatSavingsUSD = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "moat_savings_usd_total",
		Help:      "Per-request savings in USD (baseline - actual). The headline KPI of the smart-routing moat.",
	}, []string{"alias", "served_alias", "org_id"})

	out.moatQualityScore = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "moat_quality_score",
		Help:      "Quality scores produced by the inline or sampled judge for routed requests.",
		Buckets:   []float64{0, 0.25, 0.5, 0.6, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 1},
	}, []string{"alias"})

	out.moatJudgePasses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "moat_judge_passes_total",
		Help:      "Judge verdicts (result = pass | fail) for routed requests.",
	}, []string{"alias", "result"})

	out.moatRoutingOutcome = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "moat_routing_decisions_total",
		Help:      "Routing outcomes: passthrough (no override), overridden (cheaper alias served), retried (baseline retry).",
	}, []string{"decision"})

	reg.MustRegister(
		out.requests,
		out.requestDuration,
		out.tokens,
		out.cost,
		out.cacheHit,
		out.cacheMiss,
		out.guardrailBlock,
		out.rateLimitHit,
		out.smartRouteDecide,
		out.circuitBreakerOpn,
		out.moatRequests,
		out.moatCostUSD,
		out.moatBaselineUSD,
		out.moatSavingsUSD,
		out.moatQualityScore,
		out.moatJudgePasses,
		out.moatRoutingOutcome,
	)
	return out
}

// Handler returns the http.Handler that serves the /metrics endpoint.
// Mounted by server.go without auth so any standard scraper (Datadog
// Agent, Grafana Agent, plain Prometheus) can pull from it.
func Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

func statusClass(code int) string {
	switch {
	case code <= 0:
		return "unknown"
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

// RecordRequest is the canonical "this request finished" hook. Called
// from helpers.logSpendAsyncCtx so every code path that produces a
// SpendLog also produces Prometheus samples without per-handler wiring.
func RecordRequest(model, provider string, statusCode int, latencySeconds float64, promptTokens, completionTokens int, costUSD float64) {
	if model == "" {
		model = "unknown"
	}
	if provider == "" {
		provider = "unknown"
	}
	m.requests.WithLabelValues(model, provider, statusClass(statusCode)).Inc()
	if latencySeconds >= 0 {
		m.requestDuration.WithLabelValues(model, provider).Observe(latencySeconds)
	}
	if promptTokens > 0 {
		m.tokens.WithLabelValues(model, provider, "prompt").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		m.tokens.WithLabelValues(model, provider, "completion").Add(float64(completionTokens))
	}
	if costUSD > 0 {
		m.cost.WithLabelValues(model, provider).Add(costUSD)
	}
}

// RecordCache logs a cache lookup result. cacheType is "exact" or
// "semantic"; hit indicates whether the cache served the response.
func RecordCache(cacheType string, hit bool) {
	if cacheType == "" {
		cacheType = "exact"
	}
	if hit {
		m.cacheHit.WithLabelValues(cacheType).Inc()
	} else {
		m.cacheMiss.WithLabelValues(cacheType).Inc()
	}
}

// RecordGuardrailBlock records a guardrails-engine block. reason is the
// rule name that fired (already free-form in the engine itself).
func RecordGuardrailBlock(reason string) {
	if reason == "" {
		reason = "unspecified"
	}
	m.guardrailBlock.WithLabelValues(reason).Inc()
}

// RecordRateLimitHit records a 429 response. kind is "rpm" or "tpm".
func RecordRateLimitHit(kind string) {
	if kind != "rpm" && kind != "tpm" {
		kind = "unknown"
	}
	m.rateLimitHit.WithLabelValues(kind).Inc()
}

// RecordSmartRouteDecision records a SmartRoute classification.
// bucket is the complexity bucket label ("simple"/"medium"/"complex"
// today); overridden indicates whether SmartRoute swapped the alias.
func RecordSmartRouteDecision(bucket string, overridden bool) {
	if bucket == "" {
		bucket = "unknown"
	}
	override := "false"
	if overridden {
		override = "true"
	}
	m.smartRouteDecide.WithLabelValues(bucket, override).Inc()
}

// MoatSample is the per-request bundle of values the moat metrics
// record. All fields are optional; the helper trims to defaults so
// call sites can populate it lazily as data becomes available.
type MoatSample struct {
	Alias        string
	ServedAlias  string
	Strategy     string
	Retried      bool
	OrgID        string
	BaselineUSD  float64
	ActualUSD    float64
	SavingsUSD   float64
	QualityScore *float64
	QualityPass  *bool
	Overridden   bool
}

// RecordMoat is the canonical "this routing decision finished" hook.
// Fans out to every moat-namespaced collector so the chat handler
// only has to populate the bundle once.
func RecordMoat(s MoatSample) {
	alias := defaultLabel(s.Alias)
	served := defaultLabel(s.ServedAlias)
	if served == "unknown" {
		served = alias
	}
	strategy := defaultLabel(s.Strategy)
	org := s.OrgID
	if org == "" {
		org = "global"
	}
	retried := "false"
	if s.Retried {
		retried = "true"
	}

	m.moatRequests.WithLabelValues(alias, served, strategy, retried, org).Inc()
	if s.ActualUSD > 0 {
		m.moatCostUSD.WithLabelValues(alias, served, org).Add(s.ActualUSD)
	}
	if s.BaselineUSD > 0 {
		m.moatBaselineUSD.WithLabelValues(alias, served, org).Add(s.BaselineUSD)
	}
	// Savings can legitimately be negative (we picked the wrong horse);
	// only record positive deltas in the counter so rate() stays
	// monotonic. Negative values still surface in the savings ledger.
	if s.SavingsUSD > 0 {
		m.moatSavingsUSD.WithLabelValues(alias, served, org).Add(s.SavingsUSD)
	}
	if s.QualityScore != nil {
		m.moatQualityScore.WithLabelValues(alias).Observe(*s.QualityScore)
	}
	if s.QualityPass != nil {
		result := "fail"
		if *s.QualityPass {
			result = "pass"
		}
		m.moatJudgePasses.WithLabelValues(alias, result).Inc()
	}

	decision := "passthrough"
	switch {
	case s.Retried:
		decision = "retried"
	case s.Overridden:
		decision = "overridden"
	}
	m.moatRoutingOutcome.WithLabelValues(decision).Inc()
}

func defaultLabel(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// SetCircuitBreakerOpen toggles the per-deployment open/closed gauge.
// Useful for dashboards that need to alert when a provider is being
// shed at the router level.
func SetCircuitBreakerOpen(deployment, provider string, open bool) {
	if deployment == "" {
		deployment = "unknown"
	}
	if provider == "" {
		provider = "unknown"
	}
	v := 0.0
	if open {
		v = 1.0
	}
	m.circuitBreakerOpn.WithLabelValues(deployment, provider).Set(v)
}
