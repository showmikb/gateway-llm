package smartroute

import (
	"fmt"
	"math"
)

// TrainingSample is a single (features, label) pair used to fit the ML
// classifier. Label should be in [0,1] -- typically 1 if the frontier
// model was required to satisfy the feedback metric, 0 if a cheaper
// tier's response was accepted.
type TrainingSample struct {
	Features Features
	Label    float64
}

// TrainingOptions controls mini-batch logistic-regression training.
type TrainingOptions struct {
	Epochs       int
	LearningRate float64
	L2           float64
}

func defaultTrainingOptions() TrainingOptions {
	return TrainingOptions{Epochs: 200, LearningRate: 0.05, L2: 0.001}
}

// Train fits a logistic-regression model in-process. It deliberately
// depends on nothing outside the stdlib so Gateway-LLM can re-train on
// its own schedule without pulling in a heavy ML library.
func Train(samples []TrainingSample, opts TrainingOptions) ([]float64, error) {
	if len(samples) < 8 {
		return nil, fmt.Errorf("need at least 8 samples, got %d", len(samples))
	}
	if opts.Epochs <= 0 {
		opts = defaultTrainingOptions()
	}
	w := make([]float64, FeatureDim)
	n := float64(len(samples))
	for epoch := 0; epoch < opts.Epochs; epoch++ {
		grad := make([]float64, FeatureDim)
		for _, s := range samples {
			x := s.Features.Vector()
			var z float64
			for i := range x {
				z += w[i] * x[i]
			}
			p := sigmoid(z)
			err := p - s.Label
			for i := range x {
				grad[i] += err * x[i]
			}
		}
		for i := range w {
			w[i] -= opts.LearningRate * (grad[i]/n + opts.L2*w[i])
		}
	}
	if hasNaN(w) {
		return nil, fmt.Errorf("training diverged (NaN weights)")
	}
	return w, nil
}

func hasNaN(v []float64) bool {
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return true
		}
	}
	return false
}
