package semcache

import (
	"context"
	"testing"
	"time"
)

func TestHashEmbedderCosineHit(t *testing.T) {
	c := New(Config{Enabled: true, SimilarityMin: 0.85, MaxEntries: 16, TTL: time.Minute},
		NewHashEmbedder(128), nil)
	ctx := context.Background()

	c.Store(ctx, "org:alias", "What is the capital of France?",
		&Entry{Body: []byte(`{"answer":"Paris"}`), Provider: "openai"})

	if e, sim, ok := c.Lookup(ctx, "org:alias", "what is the capital of france?"); !ok {
		t.Fatalf("expected hit, got miss sim=%f", sim)
	} else if string(e.Body) != `{"answer":"Paris"}` {
		t.Fatalf("unexpected body: %s", e.Body)
	}

	if _, _, ok := c.Lookup(ctx, "org:alias", "How do I bake sourdough bread?"); ok {
		t.Fatalf("expected miss for unrelated prompt")
	}
}

func TestDisabledIsNoop(t *testing.T) {
	c := New(Config{Enabled: false}, NewHashEmbedder(64), nil)
	c.Store(context.Background(), "b", "hi", &Entry{})
	if _, _, ok := c.Lookup(context.Background(), "b", "hi"); ok {
		t.Fatal("disabled cache must miss")
	}
}

func TestBucketIsolation(t *testing.T) {
	c := New(Config{Enabled: true, SimilarityMin: 0.5, MaxEntries: 8, TTL: time.Minute},
		NewHashEmbedder(64), nil)
	ctx := context.Background()
	c.Store(ctx, "org1", "hello world", &Entry{Body: []byte(`A`)})
	if _, _, ok := c.Lookup(ctx, "org2", "hello world"); ok {
		t.Fatal("must not cross buckets")
	}
}
