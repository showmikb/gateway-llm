package smartroute

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

func TestTrainAndScore(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	samples := make([]TrainingSample, 0, 400)
	for i := 0; i < 400; i++ {
		f := Features{Bias: 1}
		f.LenRunes = rng.Float64() * 10
		f.HasCode = float64(rng.Intn(2))
		f.HasMath = float64(rng.Intn(2))
		// Label: "complex" if code OR math OR very long prompt.
		label := 0.0
		if f.HasCode == 1 || f.HasMath == 1 || f.LenRunes > 7 {
			label = 1
		}
		samples = append(samples, TrainingSample{Features: f, Label: label})
	}
	w, err := Train(samples, TrainingOptions{Epochs: 400, LearningRate: 0.1, L2: 0.0005})
	if err != nil {
		t.Fatalf("train: %v", err)
	}
	cls := NewMLClassifier()
	if err := cls.SetWeights(w, time.Now(), len(samples)); err != nil {
		t.Fatal(err)
	}

	simple := &types.ChatCompletionRequest{Messages: []types.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	complex_ := &types.ChatCompletionRequest{Messages: []types.ChatMessage{{Role: "user", Content: json.RawMessage("\"```go\\nfunc main(){}\\n```\"")}}}
	s1 := cls.Score(simple)
	s2 := cls.Score(complex_)
	if s2 <= s1 {
		t.Fatalf("expected complex > simple, got %v vs %v", s2, s1)
	}
}

func TestMLClassifierFallsBackWithoutWeights(t *testing.T) {
	cls := NewMLClassifier()
	req := &types.ChatCompletionRequest{Messages: []types.ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}}}
	if s := cls.Score(req); s < 0 || s > 1 {
		t.Fatalf("unexpected fallback score %v", s)
	}
}

func TestShadowRunnerAcquireReleases(t *testing.T) {
	r := NewShadowRunner(2, nil)
	rel1 := r.Acquire()
	rel2 := r.Acquire()
	if rel1 == nil || rel2 == nil {
		t.Fatal("first two acquires should succeed")
	}
	if r.Acquire() != nil {
		t.Fatal("third acquire should be capped")
	}
	rel1()
	if r.Acquire() == nil {
		t.Fatal("after release, acquire should succeed")
	}
	rel2()
}
