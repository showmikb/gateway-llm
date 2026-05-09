package privacy

import (
	"context"
	"sync"
)

// Vault persists the {token → original} mapping produced by the
// Redactor for one request so the response rehydrator can reverse it.
// Separate interface so tests (and future Postgres-backed
// implementations) can swap without touching the hot path.
//
// Entries are short-lived by contract — keyed by request/trace id and
// evicted after Rehydrate completes (or after a TTL on failure).
type Vault interface {
	Put(ctx context.Context, requestID string, m Mapping) error
	Get(ctx context.Context, requestID string) (Mapping, bool)
	Delete(ctx context.Context, requestID string)
}

// MemoryVault is an in-process vault suitable for single-binary
// deployments and tests. Production deployments can swap to a
// Postgres- or Vault-backed store.
type MemoryVault struct {
	mu sync.RWMutex
	m  map[string]Mapping
}

func NewMemoryVault() *MemoryVault {
	return &MemoryVault{m: make(map[string]Mapping)}
}

func (v *MemoryVault) Put(_ context.Context, id string, m Mapping) error {
	v.mu.Lock()
	v.m[id] = m
	v.mu.Unlock()
	return nil
}

func (v *MemoryVault) Get(_ context.Context, id string) (Mapping, bool) {
	v.mu.RLock()
	m, ok := v.m[id]
	v.mu.RUnlock()
	return m, ok
}

func (v *MemoryVault) Delete(_ context.Context, id string) {
	v.mu.Lock()
	delete(v.m, id)
	v.mu.Unlock()
}

// Size returns the current entry count. Useful for telemetry.
func (v *MemoryVault) Size() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.m)
}
