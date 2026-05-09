package smartroute

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

// MLClassifier is a logistic-regression complexity scorer. It ships as
// the Phase-2 drop-in replacement for ruleClassifier. Weights are
// trained offline from historical feedback + request features (see
// trainer.go) and reloaded at startup.
//
// Scoring is O(FeatureDim) so it's cheap enough to keep on the hot
// path. When weights are uninitialized (cold start) the classifier
// falls back to the rule scorer so behavior degrades gracefully.
type MLClassifier struct {
	mu       sync.RWMutex
	weights  []float64
	trainedAt time.Time
	samples  int
	fallback Classifier
}

// ModelFile is the on-disk persistence format.
type ModelFile struct {
	Weights   []float64 `json:"weights"`
	TrainedAt time.Time `json:"trained_at"`
	Samples   int       `json:"samples"`
	Version   int       `json:"version"`
}

func NewMLClassifier() *MLClassifier {
	return &MLClassifier{fallback: &ruleClassifier{}}
}

// Score returns a probability in [0,1] that the request is complex.
func (m *MLClassifier) Score(req *types.ChatCompletionRequest) float64 {
	m.mu.RLock()
	w := m.weights
	m.mu.RUnlock()
	if len(w) != FeatureDim {
		return m.fallback.Score(req)
	}
	f := Extract(req).Vector()
	var z float64
	for i := range f {
		z += w[i] * f[i]
	}
	return sigmoid(z)
}

func sigmoid(x float64) float64 {
	if x > 30 {
		return 1
	}
	if x < -30 {
		return 0
	}
	return 1.0 / (1.0 + math.Exp(-x))
}

// SetWeights replaces the classifier's weights atomically. The caller
// is expected to have run LoadFile or Trainer.Fit() first.
func (m *MLClassifier) SetWeights(w []float64, trainedAt time.Time, samples int) error {
	if len(w) != FeatureDim {
		return fmt.Errorf("weights length = %d; want %d", len(w), FeatureDim)
	}
	m.mu.Lock()
	m.weights = append(m.weights[:0], w...)
	m.trainedAt = trainedAt
	m.samples = samples
	m.mu.Unlock()
	return nil
}

// Info returns observability metadata for /v1/metrics.
func (m *MLClassifier) Info() (hasModel bool, trainedAt time.Time, samples int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.weights) == FeatureDim, m.trainedAt, m.samples
}

// LoadFile reads a model file written by SaveFile, replacing the
// current weights on success.
func (m *MLClassifier) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var mf ModelFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return m.SetWeights(mf.Weights, mf.TrainedAt, mf.Samples)
}

// SaveFile persists the current weights for later reloading.
func (m *MLClassifier) SaveFile(path string) error {
	m.mu.RLock()
	mf := ModelFile{
		Weights:   append([]float64(nil), m.weights...),
		TrainedAt: m.trainedAt,
		Samples:   m.samples,
		Version:   1,
	}
	m.mu.RUnlock()
	if len(mf.Weights) != FeatureDim {
		return fmt.Errorf("no trained weights to save")
	}
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
