package replay

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	url, err := s.Put(ctx, "key", []byte(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.Get(ctx, url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != `{"hello":"world"}` {
		t.Errorf("want roundtrip payload, got %q", got)
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(os.TempDir(), "gw-recordings-test")
	defer os.RemoveAll(dir)
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	payload := []byte(`{"trace":"abc","v":1}`)
	url, err := s.Put(ctx, "key", payload)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.Get(ctx, url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload mismatch: %q vs %q", got, payload)
	}
}

func TestContentAddressedDedup(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	url1, _ := s.Put(ctx, "a", []byte("same"))
	url2, _ := s.Put(ctx, "b", []byte("same"))
	if url1 != url2 {
		t.Errorf("identical payloads should dedup: %s vs %s", url1, url2)
	}
}
