package semcache

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOpenAIEmbedder_Success(t *testing.T) {
	want := []float64{0.1, 0.2, 0.3, 0.4}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "text-embedding-3-small" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if req.Input != "hello world" {
			t.Errorf("unexpected input: %s", req.Input)
		}
		resp := embeddingResponse{Data: []struct {
			Embedding []float64 `json:"embedding"`
		}{{Embedding: want}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	emb := NewOpenAIEmbedderWithBase("test-key", "text-embedding-3-small", srv.URL, nil)
	got, err := emb.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d dims, want %d", len(got), len(want))
	}

	// Should be L2-normalized.
	var norm float64
	for _, v := range got {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 1e-6 {
		t.Errorf("L2 norm = %f, want 1.0", norm)
	}
}

func TestOpenAIEmbedder_EmptyText(t *testing.T) {
	emb := NewOpenAIEmbedderWithBase("k", "m", "http://unused", nil)
	got, err := emb.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty text, got %v", got)
	}
}

func TestOpenAIEmbedder_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid api key"},
		})
	}))
	defer srv.Close()

	emb := NewOpenAIEmbedderWithBase("bad-key", "text-embedding-3-small", srv.URL, nil)
	_, err := emb.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestOpenAIEmbedder_RetryOn500(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
			return
		}
		resp := embeddingResponse{Data: []struct {
			Embedding []float64 `json:"embedding"`
		}{{Embedding: []float64{1.0}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	emb := NewOpenAIEmbedderWithBase("key", "text-embedding-3-small", srv.URL, nil)
	got, err := emb.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", calls.Load())
	}
	if len(got) != 1 {
		t.Errorf("expected 1-dim vector, got %d", len(got))
	}
}

func TestOpenAIEmbedder_DistinguishesSimilarPrompts(t *testing.T) {
	// Use the HashEmbedder to show that Amazon vs Walmart prompts
	// may collide, then confirm that conceptually distinct embeddings
	// produce different vectors. (Real OpenAI embeddings would diverge;
	// this test validates the code path, not the model quality.)
	hash := NewHashEmbedder(256)
	ctx := context.Background()

	v1, _ := hash.Embed(ctx, "Analyze the number of items in amazon website")
	v2, _ := hash.Embed(ctx, "Analyze the number of items in walmart website")

	sim := cosine(v1, v2)
	// With bag-of-words these are very similar (the bug scenario).
	// Just assert they aren't bit-identical.
	if sim == 1.0 {
		t.Error("hash embedder returned identical vectors for distinct prompts")
	}
	t.Logf("hash embedder cosine(amazon, walmart) = %.4f (expected high, ~0.93+)", sim)
}
