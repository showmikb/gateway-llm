package semcache

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// EmbedderFunc adapts an ordinary func to the Embedder interface.
type EmbedderFunc func(ctx context.Context, text string) ([]float64, error)

func (f EmbedderFunc) Embed(ctx context.Context, text string) ([]float64, error) {
	return f(ctx, text)
}

// HashEmbedder implements Embedder using hashed-token bag-of-words with
// L2 normalization. It is deterministic, zero-dependency, and good
// enough for smoke tests and demos where calling a real embedding model
// per request would be too costly or unavailable. A production
// deployment should replace it with a provider-backed embedder.
type HashEmbedder struct {
	Dim int
}

func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = 256
	}
	return &HashEmbedder{Dim: dim}
}

func (h *HashEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	vec := make([]float64, h.Dim)
	if text == "" {
		return vec, nil
	}
	for _, tok := range tokenize(text) {
		sum := fnv.New64a()
		_, _ = sum.Write([]byte(tok))
		sign := 1.0
		if sum.Sum64()&1 == 1 {
			sign = -1.0
		}
		vec[int(sum.Sum64()>>1)%h.Dim] += sign
	}
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec, nil
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if len(f) >= 2 {
			out = append(out, f)
		}
	}
	return out
}
