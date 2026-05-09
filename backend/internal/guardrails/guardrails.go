// Package guardrails implements pre-request and post-response content
// safety checks for LLM traffic. The engine operates on chat completion
// requests and responses using a config-driven set of rules:
//
//  1. Prompt-injection heuristics (e.g. "ignore previous instructions")
//  2. PII redaction (email, US SSN, credit-card shaped digits)
//  3. Arbitrary blocked regex patterns
//
// The engine is intentionally stateless and cheap (regex-only) so it can
// run on every request without meaningfully adding latency. Users who
// need a real ML guardrail service can plug in AWS Bedrock Guardrails /
// Azure Content Safety / Patronus via a future callback-like adapter;
// this package ships with the zero-dependency fallback that works out of
// the box.
package guardrails

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

type Engine struct {
	cfg              config.GuardrailsConfig
	blockedPatterns  []*regexp.Regexp
	injectionMatcher *regexp.Regexp
	emailRE          *regexp.Regexp
	ssnRE            *regexp.Regexp
	ccRE             *regexp.Regexp
}

// Known prompt-injection triggers. Kept conservative to minimize false
// positives; expand over time based on telemetry.
var defaultInjectionPattern = regexp.MustCompile(`(?i)\b(ignore (all|previous) instructions|disregard (your|the) (prior|previous) (instructions|system prompt)|you are now|jailbreak)\b`)

// NewEngine returns a ready-to-use engine. If cfg.Enabled is false the
// returned engine's methods are cheap no-ops.
func NewEngine(cfg config.GuardrailsConfig) *Engine {
	e := &Engine{cfg: cfg}
	if !cfg.Enabled {
		return e
	}
	for _, p := range cfg.BlockedPatterns {
		re, err := regexp.Compile(p)
		if err == nil {
			e.blockedPatterns = append(e.blockedPatterns, re)
		}
	}
	if cfg.BlockPromptInjection {
		e.injectionMatcher = defaultInjectionPattern
	}
	if cfg.RedactPII {
		e.emailRE = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
		e.ssnRE = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
		e.ccRE = regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)
	}
	return e
}

// Result describes the outcome of a guardrail check.
type Result struct {
	Blocked  bool
	Reason   string
	Redacted bool
}

// CheckRequest runs pre-request guardrails on a chat request. It mutates
// req.Messages in place if PII redaction is enabled. Returns Blocked=true
// if the request must be rejected.
func (e *Engine) CheckRequest(req *types.ChatCompletionRequest) Result {
	if e == nil || !e.cfg.Enabled || req == nil {
		return Result{}
	}

	var redacted bool
	for i := range req.Messages {
		text, _ := messageToText(req.Messages[i].Content)
		if text == "" {
			continue
		}
		for _, re := range e.blockedPatterns {
			if re.MatchString(text) {
				return Result{Blocked: true, Reason: "matched blocked pattern"}
			}
		}
		if e.injectionMatcher != nil && e.injectionMatcher.MatchString(text) {
			return Result{Blocked: true, Reason: "potential prompt injection detected"}
		}
		if e.cfg.RedactPII {
			newText, changed := e.redact(text)
			if changed {
				redacted = true
				raw, _ := json.Marshal(newText)
				req.Messages[i].Content = raw
			}
		}
	}
	return Result{Redacted: redacted}
}

// CheckResponse inspects completion text and can flag unsafe output.
// Currently mirrors the block-pattern check; PII redaction on outputs is
// intentionally opt-in via BlockedPatterns rather than auto-redact, since
// altering model output without the caller noticing is surprising.
func (e *Engine) CheckResponse(text string) Result {
	if e == nil || !e.cfg.Enabled || text == "" {
		return Result{}
	}
	for _, re := range e.blockedPatterns {
		if re.MatchString(text) {
			return Result{Blocked: true, Reason: "response matched blocked pattern"}
		}
	}
	return Result{}
}

func (e *Engine) redact(s string) (string, bool) {
	orig := s
	if e.emailRE != nil {
		s = e.emailRE.ReplaceAllString(s, "[REDACTED_EMAIL]")
	}
	if e.ssnRE != nil {
		s = e.ssnRE.ReplaceAllString(s, "[REDACTED_SSN]")
	}
	if e.ccRE != nil {
		s = e.ccRE.ReplaceAllString(s, "[REDACTED_CC]")
	}
	return s, s != orig
}

func messageToText(raw json.RawMessage) (string, bool) {
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
			}
		}
		return b.String(), true
	}
	return "", false
}
