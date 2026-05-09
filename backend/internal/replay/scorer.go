package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/gateway-llm/gateway-llm/internal/ir"
)

// Scorer decides whether a replayed response is "good enough" relative to
// the original recording. Scores are [0, 1] where higher is better; Pass is
// a coarse binary decision driven by a scorer-specific threshold.
//
// Scorers are deliberately small and composable. A replay run can stack
// them (e.g. JSONSchema + Regex) by wrapping them in Combined().
type Scorer interface {
	Name() string
	Score(ctx context.Context, orig, replay *ir.ChatResponse) (Score, error)
}

type Score struct {
	Value  float64         `json:"value"`
	Pass   bool            `json:"pass"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

// --- Regex scorer --------------------------------------------------------

// RegexScorer passes if every regex matches the replayed response text.
type RegexScorer struct {
	Patterns []*regexp.Regexp
	Name_    string
}

func NewRegexScorer(patterns []string) (*RegexScorer, error) {
	out := &RegexScorer{Name_: "regex"}
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("regex %q: %w", p, err)
		}
		out.Patterns = append(out.Patterns, re)
	}
	return out, nil
}

func (s *RegexScorer) Name() string { return s.Name_ }

func (s *RegexScorer) Score(_ context.Context, _ *ir.ChatResponse, replay *ir.ChatResponse) (Score, error) {
	text := firstContent(replay)
	matched := 0
	missed := []string{}
	for _, re := range s.Patterns {
		if re.MatchString(text) {
			matched++
		} else {
			missed = append(missed, re.String())
		}
	}
	val := 0.0
	if len(s.Patterns) > 0 {
		val = float64(matched) / float64(len(s.Patterns))
	}
	detail, _ := json.Marshal(map[string]any{
		"matched": matched,
		"total":   len(s.Patterns),
		"missed":  missed,
	})
	return Score{Value: val, Pass: val == 1.0, Detail: detail}, nil
}

// --- JSON Schema scorer --------------------------------------------------

// JSONSchemaScorer checks that the replay response is valid JSON with
// a set of required keys. It's not a full JSON Schema implementation — just
// the subset that is cheap to evaluate and useful in practice (required
// keys, rough type match). For full JSON Schema we'd wire ajv in the UI;
// here we focus on the 90% case.
type JSONSchemaScorer struct {
	RequiredKeys []string          `json:"required_keys"`
	TypeHints    map[string]string `json:"type_hints,omitempty"`
}

func (s *JSONSchemaScorer) Name() string { return "json_schema" }

func (s *JSONSchemaScorer) Score(_ context.Context, _ *ir.ChatResponse, replay *ir.ChatResponse) (Score, error) {
	text := firstContent(replay)
	text = strings.TrimSpace(text)
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		detail, _ := json.Marshal(map[string]any{"parse_error": err.Error()})
		return Score{Value: 0, Pass: false, Detail: detail}, nil
	}
	total := len(s.RequiredKeys)
	have := 0
	missing := []string{}
	for _, k := range s.RequiredKeys {
		if _, ok := obj[k]; ok {
			have++
		} else {
			missing = append(missing, k)
		}
	}
	mismatched := []string{}
	for k, wantType := range s.TypeHints {
		v, ok := obj[k]
		if !ok {
			continue
		}
		if !matchesJSONType(v, wantType) {
			mismatched = append(mismatched, k+"::"+wantType)
		}
	}
	val := 1.0
	if total > 0 {
		val = float64(have) / float64(total)
	}
	if len(mismatched) > 0 {
		val *= 0.75
	}
	pass := len(missing) == 0 && len(mismatched) == 0
	detail, _ := json.Marshal(map[string]any{
		"required":    total,
		"have":        have,
		"missing":     missing,
		"type_mismatches": mismatched,
	})
	return Score{Value: val, Pass: pass, Detail: detail}, nil
}

func matchesJSONType(v any, want string) bool {
	switch want {
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "bool", "boolean":
		_, ok := v.(bool)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	default:
		return true
	}
}

// --- Cosine-similarity scorer -------------------------------------------

// CosineScorer measures similarity between original and replay response
// text using TF-IDF-free cosine similarity on word hashes. It's a quick
// guardrail for "is this roughly the same thing"; for semantic quality use
// LLMJudge below.
type CosineScorer struct {
	Threshold float64 // pass threshold; default 0.75
}

func (s *CosineScorer) Name() string { return "cosine" }

func (s *CosineScorer) Score(_ context.Context, orig, replay *ir.ChatResponse) (Score, error) {
	thr := s.Threshold
	if thr <= 0 {
		thr = 0.75
	}
	a := tokenize(firstContent(orig))
	b := tokenize(firstContent(replay))
	sim := cosineSim(bagOfWords(a), bagOfWords(b))
	detail, _ := json.Marshal(map[string]any{"similarity": sim, "threshold": thr})
	return Score{Value: sim, Pass: sim >= thr, Detail: detail}, nil
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	out := []string{}
	cur := strings.Builder{}
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == ';' || r == ':' || r == '!' || r == '?' {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func bagOfWords(tokens []string) map[string]int {
	m := map[string]int{}
	for _, t := range tokens {
		m[t]++
	}
	return m
}

func cosineSim(a, b map[string]int) float64 {
	var dot, an, bn float64
	for k, v := range a {
		an += float64(v * v)
		if bv, ok := b[k]; ok {
			dot += float64(v * bv)
		}
	}
	for _, v := range b {
		bn += float64(v * v)
	}
	if an == 0 || bn == 0 {
		return 0
	}
	return dot / (math.Sqrt(an) * math.Sqrt(bn))
}

// --- LLMJudge scorer -----------------------------------------------------

// LLMJudge scores with a separate LLM. The judge is itself a gateway-llm
// call (so its cost is tracked, it's replayable, and it's subject to the
// same privacy layer as everything else).
//
// The prompt is pinned in code so the judge is deterministic across
// releases. Customers can override via the scorer_config.prompt field.
type LLMJudge struct {
	Model  string
	Prompt string // optional override
	Invoke func(ctx context.Context, model, prompt string) (string, error)
}

func (s *LLMJudge) Name() string { return "llm_judge" }

const defaultJudgePrompt = `You are a strict evaluation judge. You will be given an ORIGINAL answer and a REPLAY answer to the same question.
Rate how similar the REPLAY is to the ORIGINAL in content quality and correctness, on a 0-10 integer scale:
  10 = indistinguishable or strictly better
   7 = equivalent, minor phrasing differences
   4 = partially correct, missing detail
   0 = contradictory or wrong
Respond with JSON: {"score": <int>, "reason": "<one sentence>"}.

ORIGINAL:
%s

REPLAY:
%s
`

func (s *LLMJudge) Score(ctx context.Context, orig, replay *ir.ChatResponse) (Score, error) {
	if s.Invoke == nil {
		return Score{}, fmt.Errorf("llm_judge scorer requires Invoke")
	}
	promptTpl := s.Prompt
	if promptTpl == "" {
		promptTpl = defaultJudgePrompt
	}
	prompt := fmt.Sprintf(promptTpl, firstContent(orig), firstContent(replay))
	raw, err := s.Invoke(ctx, s.Model, prompt)
	if err != nil {
		return Score{}, err
	}
	var parsed struct {
		Score  int    `json:"score"`
		Reason string `json:"reason"`
	}
	// The judge may wrap JSON in prose; scan for the first {...} block.
	jsonText := extractJSON(raw)
	if err := json.Unmarshal([]byte(jsonText), &parsed); err != nil {
		detail, _ := json.Marshal(map[string]any{"parse_error": err.Error(), "raw": raw})
		return Score{Value: 0, Pass: false, Detail: detail}, nil
	}
	val := float64(parsed.Score) / 10.0
	if val < 0 {
		val = 0
	}
	if val > 1 {
		val = 1
	}
	detail, _ := json.Marshal(map[string]any{"score_10": parsed.Score, "reason": parsed.Reason})
	return Score{Value: val, Pass: parsed.Score >= 7, Detail: detail}, nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}

// --- helpers -------------------------------------------------------------

func firstContent(resp *ir.ChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	c := resp.Choices[0]
	if c.Message != nil {
		if c.Message.ContentText != "" {
			return c.Message.ContentText
		}
		if len(c.Message.ContentParts) > 0 {
			var b strings.Builder
			for _, p := range c.Message.ContentParts {
				if p.Text != "" {
					b.WriteString(p.Text)
				}
			}
			return b.String()
		}
	}
	return ""
}

// NewScorerByName is a registry used by the HTTP layer to instantiate a
// scorer from its name + a JSON config blob. Unknown names fall back to
// cosine to keep replay runs from failing hard.
func NewScorerByName(name string, cfg json.RawMessage) (Scorer, error) {
	switch name {
	case "regex":
		var c struct{ Patterns []string `json:"patterns"` }
		_ = json.Unmarshal(cfg, &c)
		return NewRegexScorer(c.Patterns)
	case "json_schema":
		s := &JSONSchemaScorer{}
		_ = json.Unmarshal(cfg, s)
		return s, nil
	case "cosine":
		s := &CosineScorer{}
		_ = json.Unmarshal(cfg, s)
		return s, nil
	case "llm_judge":
		// The Invoke function must be bound by the replay engine before use.
		s := &LLMJudge{}
		_ = json.Unmarshal(cfg, s)
		return s, nil
	default:
		return &CosineScorer{Threshold: 0.75}, nil
	}
}
