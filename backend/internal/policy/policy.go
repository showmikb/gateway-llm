// Package policy implements pillar 3b: policy-as-code gate that every
// request passes through after PII redaction and before provider
// egress. Policies are declared per-org (or global) and answer three
// questions:
//
//   1. May this request leave the gateway at all?
//   2. Which providers/regions is it allowed to reach?
//   3. Must additional guardrails (block on a PII class, require a
//      specific tag, cap max tokens, etc.) be applied?
//
// We ship an embedded interpreter for a small, auditable policy DSL
// rather than pulling in a full OPA binary. The DSL is deliberately a
// subset of Rego so a future swap to real OPA is mechanical. For
// customers who need full Rego today, DelegatedEvaluator lets them
// point at an out-of-process OPA sidecar — that integration is a thin
// adapter over the same Decision shape.
package policy

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Decision is the single return value of an Evaluate call.
type Decision struct {
	Allow          bool     `json:"allow"`
	Reason         string   `json:"reason,omitempty"`
	RequiredTags   []string `json:"required_tags,omitempty"`
	MaxTokensCap   int      `json:"max_tokens_cap,omitempty"`
	ProviderAllow  []string `json:"provider_allow,omitempty"`
	RegionAllow    []string `json:"region_allow,omitempty"`
	// RedactClasses instructs the redactor layer which PII classes must
	// be tokenized before egress. Empty means "whatever was configured
	// at the redactor level".
	RedactClasses []string `json:"redact_classes,omitempty"`
}

// Input is the shape the policy engine receives.
type Input struct {
	OrgID      string          `json:"org_id,omitempty"`
	Residency  string          `json:"residency,omitempty"`
	APIKeyID   string          `json:"api_key_id,omitempty"`
	UserID     string          `json:"user_id,omitempty"`
	Alias      string          `json:"alias"`
	Provider   string          `json:"provider"`
	Region     string          `json:"region,omitempty"`
	PIIClasses []string        `json:"pii_classes,omitempty"`
	Tags       []string        `json:"tags,omitempty"`
	Stream     bool            `json:"stream,omitempty"`
	MaxTokens  int             `json:"max_tokens,omitempty"`
	Extra      json.RawMessage `json:"extra,omitempty"`
}

// Evaluator is implemented by the in-process interpreter, by a remote
// OPA sidecar adapter, and by test fakes.
type Evaluator interface {
	Evaluate(in *Input) Decision
}

// Engine is the facade handlers call. It holds a per-org policy list
// that is consulted in declared order; the first `deny` wins, otherwise
// the decision is the conjunction of all `allow` clauses.
type Engine struct {
	mu       sync.RWMutex
	orgRules map[string][]Rule // "" key holds global rules
}

func NewEngine() *Engine {
	return &Engine{orgRules: make(map[string][]Rule)}
}

// SetRules replaces the rule list for orgID. Passing orgID="" sets
// the global baseline.
func (e *Engine) SetRules(orgID string, rs []Rule) {
	e.mu.Lock()
	e.orgRules[orgID] = rs
	e.mu.Unlock()
}

// Evaluate applies global rules then org-specific rules.
func (e *Engine) Evaluate(in *Input) Decision {
	if e == nil || in == nil {
		return Decision{Allow: true}
	}
	e.mu.RLock()
	global := e.orgRules[""]
	local := e.orgRules[in.OrgID]
	e.mu.RUnlock()

	d := Decision{Allow: true}
	for _, r := range append(append([]Rule(nil), global...), local...) {
		next := r.Apply(in)
		if !next.Allow {
			return next
		}
		d = mergeAllow(d, next)
	}
	if len(d.ProviderAllow) > 0 && !contains(d.ProviderAllow, in.Provider) {
		return Decision{Allow: false, Reason: fmt.Sprintf("policy: provider %q not in allowlist", in.Provider)}
	}
	if len(d.RegionAllow) > 0 && in.Region != "" && !contains(d.RegionAllow, in.Region) {
		return Decision{Allow: false, Reason: fmt.Sprintf("policy: region %q not allowed for org residency", in.Region)}
	}
	return d
}

func mergeAllow(base, next Decision) Decision {
	out := base
	if len(next.RequiredTags) > 0 {
		out.RequiredTags = append(out.RequiredTags, next.RequiredTags...)
	}
	if len(next.ProviderAllow) > 0 {
		out.ProviderAllow = intersect(out.ProviderAllow, next.ProviderAllow)
	}
	if len(next.RegionAllow) > 0 {
		out.RegionAllow = intersect(out.RegionAllow, next.RegionAllow)
	}
	if len(next.RedactClasses) > 0 {
		out.RedactClasses = append(out.RedactClasses, next.RedactClasses...)
	}
	if next.MaxTokensCap > 0 && (out.MaxTokensCap == 0 || next.MaxTokensCap < out.MaxTokensCap) {
		out.MaxTokensCap = next.MaxTokensCap
	}
	return out
}

// Rule is declarative and serializable. Each rule has a guard (a list
// of condition predicates) and a body that adds to the Decision.
type Rule struct {
	Name        string       `json:"name"`
	When        []Condition  `json:"when,omitempty"`
	Allow       *bool        `json:"allow,omitempty"`
	Deny        bool         `json:"deny,omitempty"`
	Reason      string       `json:"reason,omitempty"`
	Providers   []string     `json:"providers,omitempty"`
	Regions     []string     `json:"regions,omitempty"`
	RequireTags []string     `json:"require_tags,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	RedactPII   []string     `json:"redact_pii,omitempty"`
}

// Condition is a single predicate over the Input. Only one field is
// populated per condition; exactly-one-of semantics.
type Condition struct {
	ResidencyEquals string   `json:"residency_equals,omitempty"`
	OrgEquals       string   `json:"org_equals,omitempty"`
	ProviderIn      []string `json:"provider_in,omitempty"`
	RegionIn        []string `json:"region_in,omitempty"`
	PIIClassIn      []string `json:"pii_class_in,omitempty"`
	TagIn           []string `json:"tag_in,omitempty"`
	AliasPrefix     string   `json:"alias_prefix,omitempty"`
}

// Apply returns the Decision contribution of this rule for input `in`.
func (r *Rule) Apply(in *Input) Decision {
	if !r.match(in) {
		return Decision{Allow: true}
	}
	if r.Deny {
		reason := r.Reason
		if reason == "" {
			reason = fmt.Sprintf("policy: %s denied", r.Name)
		}
		return Decision{Allow: false, Reason: reason}
	}
	return Decision{
		Allow:         true,
		RequiredTags:  r.RequireTags,
		ProviderAllow: r.Providers,
		RegionAllow:   r.Regions,
		MaxTokensCap:  r.MaxTokens,
		RedactClasses: r.RedactPII,
	}
}

func (r *Rule) match(in *Input) bool {
	for _, c := range r.When {
		if !c.match(in) {
			return false
		}
	}
	return true
}

func (c *Condition) match(in *Input) bool {
	if c.ResidencyEquals != "" && in.Residency != c.ResidencyEquals {
		return false
	}
	if c.OrgEquals != "" && in.OrgID != c.OrgEquals {
		return false
	}
	if len(c.ProviderIn) > 0 && !contains(c.ProviderIn, in.Provider) {
		return false
	}
	if len(c.RegionIn) > 0 && !contains(c.RegionIn, in.Region) {
		return false
	}
	if len(c.PIIClassIn) > 0 {
		hit := false
		for _, p := range c.PIIClassIn {
			if contains(in.PIIClasses, p) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(c.TagIn) > 0 {
		hit := false
		for _, t := range c.TagIn {
			if contains(in.Tags, t) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if c.AliasPrefix != "" && !strings.HasPrefix(in.Alias, c.AliasPrefix) {
		return false
	}
	return true
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func intersect(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		if contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}
