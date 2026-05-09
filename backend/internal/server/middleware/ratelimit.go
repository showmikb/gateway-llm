package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/cache"
	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/observability"
	"go.uber.org/zap"
)

// Management/admin-plane paths are cheap bookkeeping calls (list keys, read
// usage, etc.) and must not share the LLM-proxy RPM budget. A chatty
// dashboard or CI run would otherwise trip 60 rpm in seconds and lock the
// user out of their own settings.
func isManagementPath(p string) bool {
	return strings.HasPrefix(p, "/v1/management") ||
		p == "/v1/models" ||
		p == "/v1/routes" ||
		p == "/v1/deployments"
}

func RateLimit(c *cache.Cache, cfg config.RateLimitConfig, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := GetAPIKey(r.Context())
			if key == nil || IsMaster(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			if isManagementPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			window := cfg.Window
			if window == 0 {
				window = time.Minute
			}

			rpmLimit := cfg.DefaultRPM
			if key.RPMLimit != nil && *key.RPMLimit > 0 {
				rpmLimit = *key.RPMLimit
			}

			if rpmLimit > 0 {
				rpmKey := fmt.Sprintf("rpm:%s", key.ID.String())
				count, err := c.IncrementWindow(r.Context(), rpmKey, window)
				if err != nil {
					logger.Warn("rate limit check failed", zap.Error(err))
				} else if count > int64(rpmLimit) {
					observability.RecordRateLimitHit("rpm")
					w.Header().Set("Retry-After", "60")
					writeError(w, http.StatusTooManyRequests, "rate limit exceeded (RPM)")
					return
				}
			}

			// TPM is best-effort: token usage is only known after the
			// upstream response, so we pre-flight against the current
			// counter (incremented by handlers post-response via
			// ConsumeTokens). This may allow one over-quota request
			// through, which is the standard approach for token limits.
			tpmLimit := cfg.DefaultTPM
			if key.TPMLimit != nil && *key.TPMLimit > 0 {
				tpmLimit = *key.TPMLimit
			}
			if tpmLimit > 0 {
				tpmKey := fmt.Sprintf("tpm:%s", key.ID.String())
				used, err := c.GetCounter(r.Context(), tpmKey)
				if err != nil {
					logger.Warn("tpm check failed", zap.Error(err))
				} else if used >= int64(tpmLimit) {
					observability.RecordRateLimitHit("tpm")
					w.Header().Set("Retry-After", "60")
					writeError(w, http.StatusTooManyRequests, "rate limit exceeded (TPM)")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ConsumeTokens records token usage against the TPM window for a key.
// Called by request handlers after we learn actual token counts from the
// upstream response. Safe to call with cache=nil or tokens<=0 (no-op).
func ConsumeTokens(ctx context.Context, c *cache.Cache, keyID string, tokens int, window time.Duration) {
	if c == nil || tokens <= 0 || keyID == "" {
		return
	}
	if window == 0 {
		window = time.Minute
	}
	_, _ = c.IncrementByWindow(ctx, fmt.Sprintf("tpm:%s", keyID), int64(tokens), window)
}
