package wasmplugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestNewHostClosesCleanly(t *testing.T) {
	ctx := context.Background()
	host := NewHost(ctx, zap.NewNop())
	if host == nil {
		t.Fatal("nil host")
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestLoadMissingPath(t *testing.T) {
	ctx := context.Background()
	host := NewHost(ctx, zap.NewNop())
	defer host.Close(ctx)
	if _, err := host.Load(ctx, Config{Name: "bad", Path: "/no/such/file.wasm"}); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestLoadEmptyModule verifies we surface a sensible error when the
// file is not a valid wasm binary rather than panicking deep in the
// runtime.
func TestLoadInvalidModule(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bogus.wasm")
	if err := os.WriteFile(p, []byte("not a wasm module"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	host := NewHost(ctx, zap.NewNop())
	defer host.Close(ctx)
	if _, err := host.Load(ctx, Config{Name: "bogus", Path: p}); err == nil {
		t.Fatal("expected compile error on invalid wasm")
	}
}

// TestLoadMinimalValidModule feeds the smallest legal wasm binary
// (magic + version, no sections) through the compiler and checks we
// don't crash. The module has no exports so it should have an empty
// hooks list.
func TestLoadMinimalValidModule(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "min.wasm")
	// \0asm + version 1
	bin := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(p, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	host := NewHost(ctx, zap.NewNop())
	defer host.Close(ctx)
	pl, err := host.Load(ctx, Config{Name: "min", Path: p})
	if err != nil {
		t.Fatalf("load minimal: %v", err)
	}
	if pl.Name() != "min" {
		t.Fatalf("name = %q", pl.Name())
	}
	if len(pl.exports) != 0 {
		t.Fatalf("unexpected exports: %v", pl.exports)
	}
}
