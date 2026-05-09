package replay

import (
	"context"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/ir"
)

func mkResp(text string) *ir.ChatResponse {
	return &ir.ChatResponse{
		Choices: []ir.Choice{{Message: &ir.Message{Role: "assistant", ContentText: text}}},
	}
}

func TestRegexScorer(t *testing.T) {
	s, err := NewRegexScorer([]string{`^hello`, `world$`})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	got, _ := s.Score(context.Background(), nil, mkResp("hello beautiful world"))
	if !got.Pass || got.Value != 1 {
		t.Errorf("expected full match, got %#v", got)
	}
	got, _ = s.Score(context.Background(), nil, mkResp("goodbye world"))
	if got.Pass {
		t.Errorf("expected partial fail, got %#v", got)
	}
	if got.Value != 0.5 {
		t.Errorf("expected 0.5, got %v", got.Value)
	}
}

func TestJSONSchemaScorer(t *testing.T) {
	s := &JSONSchemaScorer{RequiredKeys: []string{"city", "temp"}, TypeHints: map[string]string{"temp": "number"}}
	got, _ := s.Score(context.Background(), nil, mkResp(`{"city":"SF","temp":70}`))
	if !got.Pass {
		t.Errorf("expected pass, got %#v", got)
	}
	got, _ = s.Score(context.Background(), nil, mkResp(`{"city":"SF"}`))
	if got.Pass {
		t.Errorf("expected missing-key fail, got %#v", got)
	}
	got, _ = s.Score(context.Background(), nil, mkResp(`{"city":"SF","temp":"hot"}`))
	if got.Pass {
		t.Errorf("expected type-mismatch fail, got %#v", got)
	}
}

func TestCosineScorer(t *testing.T) {
	s := &CosineScorer{Threshold: 0.5}
	got, _ := s.Score(context.Background(), mkResp("the quick brown fox"), mkResp("the quick brown fox"))
	if got.Value < 0.99 {
		t.Errorf("identical should be ~1, got %v", got.Value)
	}
	if !got.Pass {
		t.Errorf("expected pass")
	}
	got, _ = s.Score(context.Background(), mkResp("quick fox"), mkResp("completely unrelated text about turtles"))
	if got.Value > 0.2 {
		t.Errorf("unrelated should be low, got %v", got.Value)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`Sure! {"score":8,"reason":"close"}`:         `{"score":8,"reason":"close"}`,
		`{"score":10}`:                                `{"score":10}`,
		`prelude {"a":1} suffix {"b":2}`:              `{"a":1} suffix {"b":2}`,
	}
	for in, want := range cases {
		if got := extractJSON(in); got != want {
			t.Errorf("extract(%q): got %q want %q", in, got, want)
		}
	}
}
