// Package replay implements request/response recording and replay.
//
// The design splits metadata from payload:
//   - Metadata (who, when, which model, tokens, cost, tags) lives in Postgres.
//   - Payload (the canonical IR request + response) lives in a blob store.
//
// The blob store is an interface with a local-disk default and an S3-compatible
// backend for production. Nothing else in the code references a concrete
// blob backend, so adding GCS/R2/Azure is a one-file change.
package replay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// BlobStore is the minimum a recording backend must provide.
// Put writes a blob and returns a URL; Get reads a blob by URL.
type BlobStore interface {
	Put(ctx context.Context, key string, data []byte) (url string, err error)
	Get(ctx context.Context, url string) ([]byte, error)
	Scheme() string
}

// NewBlobStoreFromURL picks a blob backend based on a URL scheme.
//   file:///var/lib/gateway-llm/recordings
//   s3://my-bucket/prefix
//   memory://
//
// Unknown schemes fall back to an in-memory store so tests and dev work
// without configuration.
func NewBlobStoreFromURL(raw string) (BlobStore, error) {
	if raw == "" {
		return NewMemoryStore(), nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse blob store url: %w", err)
	}
	switch u.Scheme {
	case "file", "":
		return NewFileStore(u.Path)
	case "memory":
		return NewMemoryStore(), nil
	case "s3":
		return NewS3Store(u.Host, strings.TrimPrefix(u.Path, "/"))
	default:
		return nil, fmt.Errorf("unknown blob store scheme: %q", u.Scheme)
	}
}

// hashKey returns a deterministic, fanned-out path for a payload so even
// 10M recordings don't end up in a single directory.
func hashKey(data []byte) (string, [32]byte) {
	sum := sha256.Sum256(data)
	hex := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s/%s/%s.json.gz", hex[:2], hex[2:4], hex), sum
}

// gzipBytes is a tiny helper so the blob payload on disk is ~10x smaller
// than raw JSON without needing a separate codec layer.
func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// --- File store -----------------------------------------------------------

type fileStore struct {
	root string
}

func NewFileStore(root string) (BlobStore, error) {
	if root == "" {
		root = filepath.Join(os.TempDir(), "gateway-llm-recordings")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &fileStore{root: root}, nil
}

func (s *fileStore) Scheme() string { return "file" }

func (s *fileStore) Put(_ context.Context, _ string, data []byte) (string, error) {
	gz, err := gzipBytes(data)
	if err != nil {
		return "", err
	}
	key, _ := hashKey(data)
	full := filepath.Join(s.root, key)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	// If this blob already exists (content-addressed), skip the write.
	if _, err := os.Stat(full); err == nil {
		return "file://" + full, nil
	}
	if err := os.WriteFile(full, gz, 0o644); err != nil {
		return "", err
	}
	return "file://" + full, nil
}

func (s *fileStore) Get(_ context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(u.Path)
	if err != nil {
		return nil, err
	}
	return gunzipBytes(data)
}

// --- Memory store (tests + dev) ------------------------------------------

type memStore struct {
	blobs map[string][]byte
}

func NewMemoryStore() BlobStore {
	return &memStore{blobs: make(map[string][]byte)}
}

func (s *memStore) Scheme() string { return "memory" }

func (s *memStore) Put(_ context.Context, _ string, data []byte) (string, error) {
	gz, err := gzipBytes(data)
	if err != nil {
		return "", err
	}
	key, _ := hashKey(data)
	s.blobs[key] = gz
	return "memory://" + key, nil
}

func (s *memStore) Get(_ context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	key := u.Host + u.Path
	key = strings.TrimPrefix(key, "/")
	gz, ok := s.blobs[key]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", raw)
	}
	return gunzipBytes(gz)
}
