// Package smartroute is Gateway-LLM's autonomous cost-quality optimizer
// (the product's "moat"). It decides which model alias should serve a
// given request by classifying the request's complexity and mapping that
// bucket to a model tier defined in config.
//
// Phase 1 (this file) ships a rule-based classifier using request
// features that are cheap to extract: message length in tokens,
// presence of code blocks, tool/function-call demands, and system-prompt
// complexity. A richer ML-based classifier can later replace Pick() via
// a pluggable Classifier interface without changing the call sites.
//
// The goal is: send the ~70% of traffic that doesn't need a frontier
// model to a cheaper tier without the user noticing. Every routing
// decision is logged with its complexity score so the feedback loop can
// later re-calibrate weights.
package smartroute

import (
	"encoding/json"
	"math/rand"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

// Complexity is a coarse bucket mapped from a float score in [0,1].
type Complexity string

const (
	ComplexitySimple  Complexity = "simple"
	ComplexityMedium  Complexity = "medium"
	ComplexityComplex Complexity = "complex"
)

// Classifier scores a request in [0,1] where higher means more complex
// reasoning is needed. A Classifier implementation can be either the
// built-in rule-based scorer or a future ML model.
type Classifier interface {
	Score(req *types.ChatCompletionRequest) float64
}

// Router is the public SmartRoute façade used by request handlers.
type Router struct {
	cfg        config.SmartRouteConfig
	classifier Classifier
	shadow     *ShadowRunner
	ml         *MLClassifier

	// decisions / overrides are monotonically incremented counters used
	// by /v1/metrics to show savings in the admin UI.
	decisions atomic.Uint64
	overrides atomic.Uint64
}

func New(cfg config.SmartRouteConfig) *Router {
	return &Router{cfg: cfg, classifier: &ruleClassifier{}}
}

// WithML enables the ML classifier. The *MLClassifier is retained so
// admin endpoints can reload weights and inspect training metadata.
func (r *Router) WithML(m *MLClassifier) *Router {
	if m == nil {
		return r
	}
	r.ml = m
	r.classifier = m
	return r
}

// WithShadow attaches a ShadowRunner so the router can report shadow
// stats via /v1/metrics.
func (r *Router) WithShadow(s *ShadowRunner) *Router {
	r.shadow = s
	return r
}

// Shadow returns the attached runner (may be nil).
func (r *Router) Shadow() *ShadowRunner { return r.shadow }

// ChallengerAlias returns the configured shadow challenger or "".
func (r *Router) ChallengerAlias() string {
	if r == nil {
		return ""
	}
	return r.cfg.ShadowChallenger
}

// ModelInfo exposes the loaded ML classifier status for telemetry.
func (r *Router) ModelInfo() (hasModel bool, trainedAt time.Time, samples int) {
	if r == nil || r.ml == nil {
		return false, time.Time{}, 0
	}
	return r.ml.Info()
}

// WithClassifier swaps the scoring backend. Intended for tests and the
// Phase-2+ ML classifier.
func (r *Router) WithClassifier(c Classifier) *Router {
	if c != nil {
		r.classifier = c
	}
	return r
}

// Pick returns the model alias to actually route to, or "" if no override
// applies (caller should use the originally requested alias). When
// SmartRoute is disabled this is always a no-op.
func (r *Router) Pick(req *types.ChatCompletionRequest, requestedAlias string) string {
	d := r.Decide(req, requestedAlias)
	if d.Overridden {
		return d.Alias
	}
	return ""
}

// Decision captures the SmartRoute classifier's output for one request.
// Handlers surface it as response headers (X-Gateway-Llm-Routing-*) and
// store it in spend/recording logs so the "savings dashboard" can later
// attribute actual $ saved.
type Decision struct {
	// Alias is the final alias we chose: either a cheaper tier or the
	// original caller-requested alias.
	Alias string
	// FromAlias is what the caller asked for. Never empty.
	FromAlias string
	// Score is the complexity score in [0,1]. 0 means "easy" (send to
	// cheap model), 1 means "hard" (keep on frontier model).
	Score float64
	// Bucket is the coarse complexity label. Exposed so the UI can show
	// the classifier's reasoning.
	Bucket Complexity
	// Overridden is true iff the router picked a different alias than
	// the caller requested (i.e. a real smart-route win vs. a no-op).
	Overridden bool
	// Enabled reports whether SmartRoute was actually consulted. When
	// false the handler should skip emitting any smart-route headers.
	Enabled bool
}

// Decide is the richer variant of Pick. It always returns a populated
// Decision, even when SmartRoute is disabled (Enabled=false). Handlers
// are expected to call Decide once per request and then use Alias as
// the routing key and the remaining fields for headers/logs.
func (r *Router) Decide(req *types.ChatCompletionRequest, requestedAlias string) Decision {
	if r == nil || req == nil || !r.cfg.Enabled {
		return Decision{Alias: requestedAlias, FromAlias: requestedAlias}
	}
	r.decisions.Add(1)
	score := r.classifier.Score(req)
	bucket := bucketOf(score)
	d := Decision{
		Alias:     requestedAlias,
		FromAlias: requestedAlias,
		Score:     score,
		Bucket:    bucket,
		Enabled:   true,
	}
	alias, ok := r.cfg.Tiers[string(bucket)]
	if !ok || alias == "" || alias == requestedAlias {
		return d
	}
	r.overrides.Add(1)
	d.Alias = alias
	d.Overridden = true
	return d
}

// ShouldShadow returns true if this request should also be sent to a
// shadow challenger in the background for quality comparison.
func (r *Router) ShouldShadow() bool {
	if r == nil || !r.cfg.Enabled || r.cfg.ShadowPercent <= 0 {
		return false
	}
	return rand.Intn(100) < r.cfg.ShadowPercent //nolint:gosec
}

func (r *Router) Stats() (decisions, overrides uint64) {
	if r == nil {
		return 0, 0
	}
	return r.decisions.Load(), r.overrides.Load()
}

func bucketOf(score float64) Complexity {
	switch {
	case score < 0.33:
		return ComplexitySimple
	case score < 0.66:
		return ComplexityMedium
	default:
		return ComplexityComplex
	}
}

// ruleClassifier scores complexity using cheap request features. The
// weights are deliberately small integers so the scoring stays
// interpretable: each signal adds a bounded amount, then we squash to
// [0,1] via a soft ceiling. Early telemetry will let us tune these.
type ruleClassifier struct{}

var (
	codeFenceRE = regexp.MustCompile("```")
	mathRE      = regexp.MustCompile(`\\(frac|sum|int|lim|alpha|beta|theta)|\$\$|equation|integral|derivative`)
	reasoningKW = regexp.MustCompile(`(?i)\b(reason|prove|derive|analyze|compare|explain why|step[- ]by[- ]step|chain of thought|think carefully)\b`)
)

func (c *ruleClassifier) Score(req *types.ChatCompletionRequest) float64 {
	if req == nil {
		return 0
	}

	// Concatenate textual content from all messages for quick feature
	// extraction. We cap at 32KB of considered text to avoid spending
	// more time classifying than serving.
	const maxScan = 32 * 1024
	var b strings.Builder
	for _, m := range req.Messages {
		text, _ := extractText(m.Content)
		if text != "" {
			if b.Len()+len(text) > maxScan {
				text = text[:maxScan-b.Len()]
			}
			b.WriteString(text)
			b.WriteByte('\n')
			if b.Len() >= maxScan {
				break
			}
		}
	}
	text := b.String()
	runes := utf8.RuneCountInString(text)

	score := 0.0
	// Length: very short prompts tend to be simple (greetings,
	// extractions); very long prompts tend to need context handling.
	switch {
	case runes < 200:
		score += 0.0
	case runes < 1000:
		score += 0.1
	case runes < 4000:
		score += 0.25
	case runes < 12000:
		score += 0.4
	default:
		score += 0.55
	}
	// Code blocks: code generation / refactoring is usually medium-complex.
	if codeFenceRE.MatchString(text) {
		score += 0.15
	}
	// Math indicators: math problems benefit from frontier models.
	if mathRE.MatchString(text) {
		score += 0.2
	}
	// Reasoning/analysis keywords.
	if reasoningKW.MatchString(text) {
		score += 0.2
	}
	// Tools: a request wielding tools is doing agent-like work.
	if len(req.Tools) > 0 {
		score += 0.15
	}
	// JSON-response-format requests with strict schemas are
	// complexity-neutral but benefit from deterministic models.
	if len(req.ResponseFormat) > 0 && strings.Contains(string(req.ResponseFormat), "json_schema") {
		score += 0.05
	}

	if score > 1 {
		score = 1
	}
	return score
}

func extractText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if t, _ := p["text"].(string); t != "" {
				b.WriteString(t)
				b.WriteByte('\n')
			}
		}
		return b.String(), true
	}
	return "", false
}
