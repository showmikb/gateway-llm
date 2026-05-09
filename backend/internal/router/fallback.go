package router

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type RequestFunc func(ctx context.Context, dep *DeploymentInfo) (*http.Response, error)

// ExecuteWithFallback tries each deployment in order, retrying on failure.
// It returns the first successful response or the last error.
func (r *ModelRouter) ExecuteWithFallback(ctx context.Context, modelAlias string, fn RequestFunc) (*http.Response, *DeploymentInfo, error) {
	return r.ExecuteWithFallbackForOrg(ctx, nil, modelAlias, fn)
}

// ExecuteWithFallbackForOrg resolves deployments scoped to an org, then tries each in order.
func (r *ModelRouter) ExecuteWithFallbackForOrg(ctx context.Context, orgID *uuid.UUID, modelAlias string, fn RequestFunc) (*http.Response, *DeploymentInfo, error) {
	deps, err := r.ResolveForOrg(orgID, modelAlias)
	if err != nil {
		return nil, nil, err
	}

	var lastErr error
	for _, dep := range deps {
		if !dep.Breaker().Allow() {
			lastErr = fmt.Errorf("circuit breaker open for provider %s/%s", dep.Provider, dep.ProviderModel)
			r.logger.Debug("skipping deployment: circuit open",
				zap.String("provider", dep.Provider),
				zap.String("model", dep.ProviderModel),
			)
			if !r.cfg.FallbackEnabled {
				return nil, dep, lastErr
			}
			continue
		}

		resp, err := r.executeWithRetry(ctx, dep, fn)
		if err != nil {
			lastErr = err
			r.ReportFailure(dep)
			r.logger.Warn("deployment failed, trying next",
				zap.String("provider", dep.Provider),
				zap.String("model", dep.ProviderModel),
				zap.String("circuit", dep.Breaker().State().String()),
				zap.Error(err),
			)
			if !r.cfg.FallbackEnabled {
				return nil, dep, err
			}
			continue
		}

		if resp.StatusCode >= 500 {
			r.ReportFailure(dep)
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream returned %d", resp.StatusCode)
			r.logger.Warn("deployment returned server error",
				zap.String("provider", dep.Provider),
				zap.Int("status", resp.StatusCode),
				zap.String("circuit", dep.Breaker().State().String()),
			)
			if !r.cfg.FallbackEnabled {
				return nil, dep, lastErr
			}
			continue
		}

		r.ReportSuccess(dep)
		return resp, dep, nil
	}

	return nil, nil, fmt.Errorf("all deployments failed for model %q: %w", modelAlias, lastErr)
}

func (r *ModelRouter) executeWithRetry(ctx context.Context, dep *DeploymentInfo, fn RequestFunc) (*http.Response, error) {
	var lastErr error
	maxRetries := r.cfg.Retries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(r.cfg.RetryDelay):
			}
		}

		start := time.Now()
		resp, err := fn(ctx, dep)
		elapsed := time.Since(start)
		r.ReportLatency(dep, elapsed)

		if err != nil {
			lastErr = err
			r.logger.Debug("request attempt failed",
				zap.Int("attempt", attempt+1),
				zap.String("provider", dep.Provider),
				zap.Error(err),
			)
			continue
		}

		return resp, nil
	}

	return nil, lastErr
}
