package replay

import (
	"context"
	"testing"
)

// BenchmarkMemoryPut approximates the cost of enqueueing a recording's
// payload into the in-memory blob store. Includes gzip compression but
// not the DB insert (which is async).
func BenchmarkMemoryPut(b *testing.B) {
	store := NewMemoryStore()
	payload := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Put(context.Background(), "k", payload)
	}
}
