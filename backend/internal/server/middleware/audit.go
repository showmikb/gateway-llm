package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/audit"
	"github.com/gateway-llm/gateway-llm/internal/models"
)

// AuditLLMRequests records an audit entry for every call that traverses
// the /v1/* API surface. The entry is created post-response so we can
// capture the final status code. Audit writes are non-blocking.
func AuditLLMRequests(a *audit.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a == nil || isManagementPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			ev := audit.From(r, "api_key")
			ev.Action = "llm." + strings.TrimPrefix(r.URL.Path, "/v1/")
			ev.Resource = r.URL.Path
			if key := GetAPIKey(r.Context()); key != nil {
				if key.ID != (models.APIKey{}).ID {
					id := key.ID
					ev.APIKeyID = &id
				}
				if key.TeamID != nil {
					ev.TeamID = key.TeamID
				}
			}
			if orgID := GetOrgID(r.Context()); orgID != nil {
				ev.OrgID = orgID
			}
			ev.Metadata = map[string]any{
				"status":     rw.status,
				"method":     r.Method,
				"latency_ms": time.Since(start).Milliseconds(),
			}
			a.Record(ev)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
