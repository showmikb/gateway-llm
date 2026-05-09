package smartroute

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

// Features are the numeric inputs fed to both the rule-based scorer and
// the ML classifier. Keeping them identical makes the two
// implementations directly comparable during shadow evaluation.
type Features struct {
	Bias         float64
	LenRunes     float64
	LogTurns     float64
	HasCode      float64
	HasMath      float64
	HasReasoning float64
	HasTools     float64
	HasJSONSpec  float64
	SystemLen    float64
}

// Vector returns the features as a dense slice in a stable order so the
// ML classifier's weight vector matches up.
func (f Features) Vector() []float64 {
	return []float64{
		f.Bias,
		f.LenRunes,
		f.LogTurns,
		f.HasCode,
		f.HasMath,
		f.HasReasoning,
		f.HasTools,
		f.HasJSONSpec,
		f.SystemLen,
	}
}

// FeatureDim is the number of elements produced by Features.Vector().
const FeatureDim = 9

var (
	codeFenceRE2 = regexp.MustCompile("```")
	mathRE2      = regexp.MustCompile(`\\(frac|sum|int|lim|alpha|beta|theta)|\$\$|equation|integral|derivative`)
	reasoningRE  = regexp.MustCompile(`(?i)\b(reason|prove|derive|analyze|compare|explain why|step[- ]by[- ]step|chain of thought|think carefully)\b`)
)

// Extract runs once per request to cheaply derive Features. The length
// and system-prompt metrics are log-scaled so extreme outliers don't
// dominate a linear classifier.
func Extract(req *types.ChatCompletionRequest) Features {
	if req == nil {
		return Features{Bias: 1}
	}
	var total, sysLen int
	var userTextB strings.Builder
	const maxScan = 32 * 1024
	for _, m := range req.Messages {
		t, _ := extractText(m.Content)
		total += utf8.RuneCountInString(t)
		if m.Role == "system" {
			sysLen += utf8.RuneCountInString(t)
		}
		if userTextB.Len() < maxScan {
			userTextB.WriteString(t)
			userTextB.WriteByte('\n')
		}
	}
	text := userTextB.String()
	f := Features{Bias: 1}
	f.LenRunes = log1p(float64(total))
	f.LogTurns = log1p(float64(len(req.Messages)))
	if codeFenceRE2.MatchString(text) {
		f.HasCode = 1
	}
	if mathRE2.MatchString(text) {
		f.HasMath = 1
	}
	if reasoningRE.MatchString(text) {
		f.HasReasoning = 1
	}
	if len(req.Tools) > 0 {
		f.HasTools = 1
	}
	if len(req.ResponseFormat) > 0 && strings.Contains(string(req.ResponseFormat), "json_schema") {
		f.HasJSONSpec = 1
	}
	f.SystemLen = log1p(float64(sysLen))
	return f
}

func log1p(x float64) float64 {
	// lightweight, avoids math import at call sites where we just want
	// a non-negative growth curve.
	if x <= 0 {
		return 0
	}
	y := 0.0
	n := x
	for n > 1 {
		n /= 2
		y += 0.6931471805599453
	}
	return y + (n - 1) - (n-1)*(n-1)/2
}
