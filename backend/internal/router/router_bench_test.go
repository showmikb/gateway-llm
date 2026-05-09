package router

import (
	"fmt"
	"testing"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// buildBenchRouter constructs an in-memory ModelRouter with `numDeps` deployments
// under a single alias. When `withOrg` is true, an additional set of
// org-scoped deployments is added so ResolveForOrg exercises both the
// per-org filter and the global fallback path.
func buildBenchRouter(strategy string, numDeps int, withOrg bool) (*ModelRouter, *uuid.UUID) {
	r := New(nil, config.RoutingConfig{Strategy: strategy}, nil, zap.NewNop())

	deps := make([]*DeploymentInfo, 0, numDeps*2)
	for i := 0; i < numDeps; i++ {
		deps = append(deps, &DeploymentInfo{
			Provider:      "openai",
			ProviderModel: fmt.Sprintf("gpt-%d", i),
			APIKey:        "sk-test",
		})
	}

	var orgID *uuid.UUID
	if withOrg {
		id := uuid.New()
		orgID = &id
		for i := 0; i < numDeps; i++ {
			deps = append(deps, &DeploymentInfo{
				Provider:      "openai",
				ProviderModel: fmt.Sprintf("gpt-org-%d", i),
				APIKey:        "sk-test",
				OrgID:         orgID,
			})
		}
	}

	r.Reload(map[string][]*DeploymentInfo{"bench-model": deps})
	return r, orgID
}

func benchResolve(b *testing.B, strategy string, n int) {
	r, _ := buildBenchRouter(strategy, n, false)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		deps, err := r.Resolve("bench-model")
		if err != nil || len(deps) == 0 {
			b.Fatalf("resolve failed: %v", err)
		}
	}
}

func benchResolveForOrg(b *testing.B, strategy string, n int) {
	r, orgID := buildBenchRouter(strategy, n, true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		deps, err := r.ResolveForOrg(orgID, "bench-model")
		if err != nil || len(deps) == 0 {
			b.Fatalf("resolve-for-org failed: %v", err)
		}
	}
}

func BenchmarkResolve_RoundRobin_1(b *testing.B)  { benchResolve(b, "round-robin", 1) }
func BenchmarkResolve_RoundRobin_5(b *testing.B)  { benchResolve(b, "round-robin", 5) }
func BenchmarkResolve_RoundRobin_20(b *testing.B) { benchResolve(b, "round-robin", 20) }

func BenchmarkResolve_LeastLatency_1(b *testing.B)  { benchResolve(b, "least-latency", 1) }
func BenchmarkResolve_LeastLatency_5(b *testing.B)  { benchResolve(b, "least-latency", 5) }
func BenchmarkResolve_LeastLatency_20(b *testing.B) { benchResolve(b, "least-latency", 20) }

func BenchmarkResolveForOrg_RoundRobin_5(b *testing.B)  { benchResolveForOrg(b, "round-robin", 5) }
func BenchmarkResolveForOrg_RoundRobin_20(b *testing.B) { benchResolveForOrg(b, "round-robin", 20) }

// TestResolveSub10Microseconds asserts that the average routing-decision
// latency is well below the 10us promise from the README. It exists as a
// regular unit test (not a benchmark) so CI catches regressions without
// needing to parse `go test -bench` output.
//
// Run just this test with:  go test -run Sub10 ./internal/router
func TestResolveSub10Microseconds(t *testing.T) {
	r, _ := buildBenchRouter("round-robin", 20, false)

	// Warm up caches.
	for i := 0; i < 1000; i++ {
		if _, err := r.Resolve("bench-model"); err != nil {
			t.Fatalf("warmup resolve failed: %v", err)
		}
	}

	const iters = 200_000
	start := time.Now()
	for i := 0; i < iters; i++ {
		if _, err := r.Resolve("bench-model"); err != nil {
			t.Fatalf("resolve failed: %v", err)
		}
	}
	avg := time.Since(start) / iters

	const target = 10 * time.Microsecond
	if avg >= target {
		t.Fatalf("Resolve averaged %v/op, above the 10us target", avg)
	}
	t.Logf("Resolve averaged %v/op across %d iterations (target <%v)", avg, iters, target)
}

func TestResolveForOrgSub10Microseconds(t *testing.T) {
	r, orgID := buildBenchRouter("round-robin", 20, true)

	for i := 0; i < 1000; i++ {
		if _, err := r.ResolveForOrg(orgID, "bench-model"); err != nil {
			t.Fatalf("warmup resolve-for-org failed: %v", err)
		}
	}

	const iters = 200_000
	start := time.Now()
	for i := 0; i < iters; i++ {
		if _, err := r.ResolveForOrg(orgID, "bench-model"); err != nil {
			t.Fatalf("resolve-for-org failed: %v", err)
		}
	}
	avg := time.Since(start) / iters

	const target = 10 * time.Microsecond
	if avg >= target {
		t.Fatalf("ResolveForOrg averaged %v/op, above the 10us target", avg)
	}
	t.Logf("ResolveForOrg averaged %v/op across %d iterations (target <%v)", avg, iters, target)
}
