// Package semcache is Gateway-LLM's semantic response cache. Where
// `respcache` matches requests byte-for-byte, `semcache` embeds the
// prompt into a vector and returns a cached response when a previously
// seen prompt is close enough under cosine similarity.
//
// Phase-1 storage is an in-memory ring buffer scoped by organization
// plus model alias. This keeps the cache warm within a single gateway
// process without a hard dependency on pgvector / Qdrant / Redis
// vector search, at the cost of losing entries on restart and not
// sharing across replicas. The Embedder and Store interfaces are
// deliberately small so a persistent index can be swapped in later.
package semcache

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Embedder turns a canonical prompt string into a dense vector. Any
// EmbeddingsProvider-backed implementation satisfies this interface.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// Entry is the cached payload returned on a semantic hit. The caller
// decides what Body is; semcache treats it as an opaque blob.
type Entry struct {
	Body             []byte
	Provider         string
	ProviderModel    string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	StoredAt         time.Time
}

// Config tunes the cache at construction time. SimilarityMin is the
// cosine-similarity threshold in [0,1]; requests scoring below it are
// treated as misses. MaxEntries per (org+alias) bucket bounds memory.
type Config struct {
	Enabled       bool
	SimilarityMin float64
	MaxEntries    int
	TTL           time.Duration
}

// Cache is the public façade used from handlers.
type Cache struct {
	cfg      Config
	embedder Embedder
	logger   *zap.Logger

	mu      sync.RWMutex
	buckets map[string]*ring

	hits   uint64
	misses uint64
}

type ring struct {
	items []*record
	next  int
}

type record struct {
	vec     []float64
	entry   *Entry
	expires time.Time
}

// New constructs a semantic cache. A nil Embedder makes every call a
// miss, which is useful in tests and when the feature is disabled.
func New(cfg Config, emb Embedder, logger *zap.Logger) *Cache {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 256
	}
	if cfg.SimilarityMin <= 0 {
		cfg.SimilarityMin = 0.92
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Minute
	}
	return &Cache{cfg: cfg, embedder: emb, logger: logger, buckets: map[string]*ring{}}
}

// Enabled reports whether lookups should be attempted.
func (c *Cache) Enabled() bool {
	return c != nil && c.cfg.Enabled && c.embedder != nil
}

// Lookup returns the best entry above SimilarityMin, or (nil,false). The
// caller is responsible for composing a bucket string (usually
// "<org>:<model-alias>") that partitions the cache.
func (c *Cache) Lookup(ctx context.Context, bucket, text string) (*Entry, float64, bool) {
	if !c.Enabled() || text == "" {
		return nil, 0, false
	}
	vec, err := c.embedder.Embed(ctx, text)
	if err != nil || len(vec) == 0 {
		if c.logger != nil {
			c.logger.Debug("semcache embed failed", zap.Error(err))
		}
		return nil, 0, false
	}
	c.mu.RLock()
	r := c.buckets[bucket]
	c.mu.RUnlock()
	if r == nil {
		c.misses++
		return nil, 0, false
	}
	now := time.Now()
	var best *record
	var bestSim float64
	for _, rec := range r.items {
		if rec == nil {
			continue
		}
		if !rec.expires.IsZero() && rec.expires.Before(now) {
			continue
		}
		sim := cosine(vec, rec.vec)
		if sim > bestSim {
			bestSim = sim
			best = rec
		}
	}
	if best == nil || bestSim < c.cfg.SimilarityMin {
		c.misses++
		return nil, bestSim, false
	}
	c.hits++
	return best.entry, bestSim, true
}

// Store remembers an entry for the given bucket keyed on the embedding
// of `text`. Stores are best-effort: embedding failures are silently
// dropped because they are never user-visible.
func (c *Cache) Store(ctx context.Context, bucket, text string, entry *Entry) {
	if !c.Enabled() || entry == nil || text == "" {
		return
	}
	vec, err := c.embedder.Embed(ctx, text)
	if err != nil || len(vec) == 0 {
		return
	}
	entry.StoredAt = time.Now()
	rec := &record{vec: vec, entry: entry, expires: time.Now().Add(c.cfg.TTL)}
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.buckets[bucket]
	if r == nil {
		r = &ring{items: make([]*record, 0, c.cfg.MaxEntries)}
		c.buckets[bucket] = r
	}
	if len(r.items) < c.cfg.MaxEntries {
		r.items = append(r.items, rec)
		return
	}
	r.items[r.next] = rec
	r.next = (r.next + 1) % c.cfg.MaxEntries
}

// Stats returns monotonic hit/miss counters for admin telemetry.
func (c *Cache) Stats() (hits, misses uint64) {
	if c == nil {
		return 0, 0
	}
	return c.hits, c.misses
}

// cosine returns the cosine similarity of two equally-sized vectors in
// [-1,1]. Mismatched dimensions yield 0 so callers treat them as misses.
func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
