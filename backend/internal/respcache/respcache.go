// Package respcache provides exact-match response caching for LLM chat
// completions. The cache key is a SHA-256 of a canonical form of the
// request (model alias + messages + deterministic params). Only requests
// that are semantically deterministic (temperature==0 or unset) and
// non-streaming are eligible, to avoid returning stale or nondeterministic
// outputs.
//
// The cache is backed by Redis via the internal cache package, with an
// in-memory fallback when Redis is unavailable (inherited from
// cache.Cache). Cached values are stored as raw JSON byte slices tagged
// with the original cost so we can credit the key with "cache hit"
// savings in the spend log.
package respcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type CacheClient interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// Entry is the envelope stored in the cache. We keep Body as raw JSON so
// handlers can write it straight to the HTTP response without re-encoding.
type Entry struct {
	Body             json.RawMessage `json:"body"`
	Provider         string          `json:"provider"`
	ProviderModel    string          `json:"provider_model"`
	PromptTokens     int             `json:"prompt_tokens"`
	CompletionTokens int             `json:"completion_tokens"`
	CostUSD          float64         `json:"cost_usd"`
	StoredAt         time.Time       `json:"stored_at"`
}

// Cache is the public surface used by handlers.
type Cache struct {
	client CacheClient
	logger *zap.Logger
	ttl    time.Duration
}

func New(client CacheClient, ttl time.Duration, logger *zap.Logger) *Cache {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Cache{client: client, ttl: ttl, logger: logger}
}

// KeyParts is the canonical form of a chat request that determines cache
// identity. Requests with Temperature != 0 / nondeterministic params
// should skip caching entirely.
type KeyParts struct {
	ModelAlias string          `json:"m"`
	Messages   json.RawMessage `json:"msgs"`
	Tools      json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tc,omitempty"`
	Seed       *int            `json:"seed,omitempty"`
	MaxTokens  *int            `json:"max,omitempty"`
	OrgID      string          `json:"org,omitempty"`
}

// Key returns the canonical cache key for the given request components.
func Key(p KeyParts) string {
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	return "respcache:" + hex.EncodeToString(h[:])
}

func (c *Cache) Get(ctx context.Context, key string) (*Entry, bool) {
	if c == nil || c.client == nil {
		return nil, false
	}
	raw, ok, err := c.client.Get(ctx, key)
	if err != nil || !ok {
		return nil, false
	}
	var entry Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false
	}
	return &entry, true
}

func (c *Cache) Set(ctx context.Context, key string, entry *Entry) {
	if c == nil || c.client == nil || entry == nil {
		return
	}
	entry.StoredAt = time.Now()
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := c.client.Set(ctx, key, raw, c.ttl); err != nil && c.logger != nil {
		c.logger.Debug("respcache set failed", zap.Error(err))
	}
}

// RedisAdapter wraps a *redis.Client to satisfy CacheClient. A nil client
// means the cache is a no-op.
type RedisAdapter struct {
	Client *redis.Client
}

func (r *RedisAdapter) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if r == nil || r.Client == nil {
		return nil, false, nil
	}
	v, err := r.Client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

func (r *RedisAdapter) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if r == nil || r.Client == nil {
		return nil
	}
	return r.Client.Set(ctx, key, value, ttl).Err()
}
