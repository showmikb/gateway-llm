package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type Cache struct {
	client    *redis.Client
	logger    *zap.Logger
	available bool
	local     *localCache
}

type localCache struct {
	mu    sync.RWMutex
	items map[string]*localItem
}

type localItem struct {
	value     int64
	expiresAt time.Time
}

func New(redisURL string, logger *zap.Logger) *Cache {
	c := &Cache{
		logger: logger,
		local: &localCache{
			items: make(map[string]*localItem),
		},
	}

	if redisURL == "" {
		logger.Info("redis URL not configured, using in-memory rate limiting")
		return c
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Warn("invalid redis URL, falling back to in-memory", zap.Error(err))
		return c
	}

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn("redis not reachable, falling back to in-memory", zap.Error(err))
		return c
	}

	c.client = client
	c.available = true
	logger.Info("connected to redis")
	return c
}

func (c *Cache) IncrementWindow(ctx context.Context, key string, window time.Duration) (int64, error) {
	return c.IncrementByWindow(ctx, key, 1, window)
}

// IncrementByWindow atomically increments the counter by n, sets the
// expiration to window on create, and returns the new value. Used by the
// rate limiter (n=1 for RPM, n=token-count for TPM).
func (c *Cache) IncrementByWindow(ctx context.Context, key string, n int64, window time.Duration) (int64, error) {
	if n <= 0 {
		n = 1
	}
	if c.available {
		return c.redisIncrementBy(ctx, key, n, window)
	}
	return c.localIncrementBy(key, n, window), nil
}

// GetCounter returns the current counter value without incrementing. Used
// by the TPM middleware for a pre-flight budget check before we know the
// actual token cost of the request.
func (c *Cache) GetCounter(ctx context.Context, key string) (int64, error) {
	if c.available {
		v, err := c.client.Get(ctx, key).Int64()
		if err == redis.Nil {
			return 0, nil
		}
		if err != nil {
			return c.localGet(key), nil
		}
		return v, nil
	}
	return c.localGet(key), nil
}

func (c *Cache) redisIncrementBy(ctx context.Context, key string, n int64, window time.Duration) (int64, error) {
	pipe := c.client.Pipeline()
	incr := pipe.IncrBy(ctx, key, n)
	pipe.Expire(ctx, key, window)
	_, err := pipe.Exec(ctx)
	if err != nil {
		c.logger.Warn("redis increment failed, falling back to local", zap.Error(err))
		return c.localIncrementBy(key, n, window), nil
	}
	return incr.Val(), nil
}

func (c *Cache) localIncrementBy(key string, n int64, window time.Duration) int64 {
	c.local.mu.Lock()
	defer c.local.mu.Unlock()

	now := time.Now()
	item, ok := c.local.items[key]
	if !ok || now.After(item.expiresAt) {
		c.local.items[key] = &localItem{value: n, expiresAt: now.Add(window)}
		return n
	}
	item.value += n
	return item.value
}

func (c *Cache) localGet(key string) int64 {
	c.local.mu.RLock()
	defer c.local.mu.RUnlock()
	item, ok := c.local.items[key]
	if !ok || time.Now().After(item.expiresAt) {
		return 0
	}
	return item.value
}

// RedisClient returns the underlying redis client (may be nil if not
// configured / reachable). Used by adapters that need richer semantics
// than IncrementByWindow (e.g. response caching Set/Get/EX).
func (c *Cache) RedisClient() *redis.Client {
	if c == nil {
		return nil
	}
	return c.client
}

func (c *Cache) HealthCheck(ctx context.Context) error {
	if !c.available {
		return fmt.Errorf("redis not available")
	}
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Close() {
	if c.client != nil {
		c.client.Close()
	}
}
