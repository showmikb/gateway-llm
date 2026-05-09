package router

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"go.uber.org/zap"
)

// newTestRouter spins up a ModelRouter with two aliases pointing at
// stub deployments. The RequestFunc is keyed by dep.Provider so tests
// can script success/failure per alias.
func newTestRouter(t *testing.T, cheapDelay, expensiveDelay time.Duration) *ModelRouter {
	t.Helper()
	r := New(
		[]config.ModelAlias{
			{ModelAlias: "cheap", Deployments: []config.Deployment{{Provider: "cheap", Model: "cheap-m", APIKeyEnv: "X"}}},
			{ModelAlias: "expensive", Deployments: []config.Deployment{{Provider: "expensive", Model: "ex-m", APIKeyEnv: "X"}}},
		},
		config.RoutingConfig{FallbackEnabled: true, Retries: 0},
		nil,
		zap.NewNop(),
	)
	return r
}

func TestSpeculative_CheapWinsUnderGrace(t *testing.T) {
	r := newTestRouter(t, 10*time.Millisecond, 500*time.Millisecond)
	var cheapHit, expHit atomic.Int32
	fn := func(ctx context.Context, dep *DeploymentInfo) (*http.Response, error) {
		if dep.Provider == "cheap" {
			cheapHit.Add(1)
			time.Sleep(10 * time.Millisecond)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("cheap"))}, nil
		}
		expHit.Add(1)
		time.Sleep(500 * time.Millisecond)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("expensive"))}, nil
	}
	res, err := r.ExecuteSpeculative(context.Background(), nil, "cheap", "expensive", 250*time.Millisecond, fn)
	if err != nil {
		t.Fatalf("spec failed: %v", err)
	}
	if res.Winner != "cheap" {
		t.Fatalf("expected cheap winner, got %s", res.Winner)
	}
	b, _ := io.ReadAll(res.Response.Body)
	if string(b) != "cheap" {
		t.Fatalf("expected cheap body, got %s", b)
	}
	// Expensive branch should have been cancelled before it fired since
	// cheap finished in 10ms << 250ms grace.
	if expHit.Load() != 0 {
		t.Fatalf("expected expensive branch not to fire, got %d hits", expHit.Load())
	}
}

func TestSpeculative_ExpensiveWinsOnCheapFailure(t *testing.T) {
	r := newTestRouter(t, 0, 0)
	fn := func(ctx context.Context, dep *DeploymentInfo) (*http.Response, error) {
		if dep.Provider == "cheap" {
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ex-ok"))}, nil
	}
	res, err := r.ExecuteSpeculative(context.Background(), nil, "cheap", "expensive", 10*time.Millisecond, fn)
	if err != nil {
		t.Fatalf("spec failed: %v", err)
	}
	if res.Winner != "expensive" {
		t.Fatalf("expected expensive winner, got %s", res.Winner)
	}
}

func TestSpeculative_BothFail(t *testing.T) {
	r := newTestRouter(t, 0, 0)
	fn := func(ctx context.Context, dep *DeploymentInfo) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("nope"))}, nil
	}
	_, err := r.ExecuteSpeculative(context.Background(), nil, "cheap", "expensive", 0, fn)
	if err == nil {
		t.Fatalf("expected error when both branches fail")
	}
}

func TestSpeculative_EmptyAliasFallsBackToNonEmpty(t *testing.T) {
	r := newTestRouter(t, 0, 0)
	fn := func(ctx context.Context, dep *DeploymentInfo) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}
	res, err := r.ExecuteSpeculative(context.Background(), nil, "", "expensive", 0, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Dep.Provider != "expensive" {
		t.Fatalf("expected expensive dep, got %+v", res.Dep)
	}
}
