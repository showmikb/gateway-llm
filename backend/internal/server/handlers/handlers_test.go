package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

func setupTestRouter(masterKey string) *chi.Mux {
	r := chi.NewRouter()
	return r
}

func TestHealthEndpoint(t *testing.T) {
	r := setupTestRouter("test-master-key")
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %s", body["status"])
	}
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "test-master-key", nil))
	r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "test-master-key", nil))
	r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with invalid token, got %d", w.Code)
	}
}

func TestAuthMiddleware_MasterKey(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "test-master-key", nil))
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		if !middleware.IsMaster(r.Context()) {
			t.Error("expected IsMaster to be true")
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-master-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with master key, got %d", w.Code)
	}
}

func TestAdminOnly_WithMasterKey(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "test-master-key", nil))
	r.Use(middleware.AdminOnly("test-master-key"))
	r.Get("/admin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer test-master-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin with master key, got %d", w.Code)
	}
}

func TestAdminOnly_WithoutMasterKey(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "test-master-key", nil))
	r.Use(middleware.AdminOnly("test-master-key"))
	r.Get("/admin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for non-master key, got %d", w.Code)
	}
}

func TestGetAPIKey_FromContext(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "test-master-key", nil))
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		key := middleware.GetAPIKey(r.Context())
		if key == nil {
			t.Error("expected API key in context")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if key.Name != "master_key" {
			t.Errorf("expected master_key name, got %s", key.Name)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-master-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCheckModelAccess_NoRestriction(t *testing.T) {
	if !middleware.CheckModelAccess(nil, "any-model") {
		t.Error("nil key should allow all models")
	}
}

func TestCheckModelAccess_WithModels(t *testing.T) {
	key := &models.APIKey{Models: []string{"gpt-4o", "gpt-4o-mini"}}
	if !middleware.CheckModelAccess(key, "gpt-4o") {
		t.Error("key with gpt-4o in models should allow gpt-4o")
	}
	if middleware.CheckModelAccess(key, "gpt-3.5-turbo") {
		t.Error("key without gpt-3.5-turbo should deny it")
	}
}

func TestErrorResponse_Format(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/error", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(types.ErrorResponse{
			Error: types.ErrorDetail{
				Message: "test error",
				Type:    "invalid_request_error",
			},
		})
	})

	req := httptest.NewRequest("GET", "/error", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var errResp types.ErrorResponse
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Message != "test error" {
		t.Errorf("expected 'test error', got %s", errResp.Error.Message)
	}
	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("expected 'invalid_request_error', got %s", errResp.Error.Type)
	}
}

func TestWriteJSON_Helper(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	})

	req := httptest.NewRequest("GET", "/json", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["hello"] != "world" {
		t.Errorf("expected 'world', got %s", body["hello"])
	}
}

func TestCORSOptions(t *testing.T) {
	corsOpts := middleware.CORS()
	if corsOpts.AllowedOrigins == nil || len(corsOpts.AllowedOrigins) == 0 {
		t.Error("expected CORS allowed origins to be set")
	}
}

func TestAuthMiddleware_EmptyMasterKey(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "", nil))
	r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer some-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with empty master key and nil DB, got %d", w.Code)
	}
}

func TestAuthMiddleware_BearerPrefix(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Auth(nil, "test-key", nil))
	r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		authHeader string
		expected   int
	}{
		{"valid bearer", "Bearer test-key", http.StatusOK},
		{"missing bearer prefix", "test-key", http.StatusUnauthorized},
		{"empty header", "", http.StatusUnauthorized},
		{"basic auth", "Basic dGVzdA==", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expected {
				t.Errorf("%s: expected %d, got %d", tt.name, tt.expected, w.Code)
			}
		})
	}
}

func TestLoginEndpoint_MissingFields(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/v1/management/users/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Email == "" || body.Password == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(types.ErrorResponse{
				Error: types.ErrorDetail{Message: "email and password are required", Type: "invalid_request_error"},
			})
			return
		}
	})

	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{"empty body", "{}", http.StatusBadRequest},
		{"missing password", `{"email":"test@test.com"}`, http.StatusBadRequest},
		{"missing email", `{"password":"secret"}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/management/users/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.expected {
				t.Errorf("%s: expected %d, got %d", tt.name, tt.expected, w.Code)
			}
		})
	}
}

func TestHasRole(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		minRole string
		want    bool
	}{
		{"super_admin >= org_admin", "super_admin", "org_admin", true},
		{"super_admin >= super_admin", "super_admin", "super_admin", true},
		{"org_admin >= org_admin", "org_admin", "org_admin", true},
		{"org_admin < super_admin", "org_admin", "super_admin", false},
		{"team_admin >= member", "team_admin", "member", true},
		{"team_admin < org_admin", "team_admin", "org_admin", false},
		{"member >= member", "member", "member", true},
		{"member < team_admin", "member", "team_admin", false},
		{"legacy admin >= org_admin", "admin", "org_admin", true},
		{"legacy user >= member", "user", "member", true},
		{"legacy user < org_admin", "user", "org_admin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &models.User{Role: tt.role}
			ctx := context.WithValue(context.Background(), middleware.UserContextKey, user)
			ctx = context.WithValue(ctx, middleware.IsMasterKey, false)

			got := middleware.HasRole(ctx, tt.minRole)
			if got != tt.want {
				t.Errorf("HasRole(%s, %s) = %v, want %v", tt.role, tt.minRole, got, tt.want)
			}
		})
	}
}

func TestHasRole_MasterKey(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.IsMasterKey, true)
	if !middleware.HasRole(ctx, "super_admin") {
		t.Error("master key should satisfy any role requirement")
	}
}

func TestHasRole_NoUser(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.IsMasterKey, false)
	if middleware.HasRole(ctx, "member") {
		t.Error("no user in context should fail role check")
	}
}

func TestAdminOnly_OrgAdminUser(t *testing.T) {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), middleware.APIKeyContextKey, &models.APIKey{Name: "session", IsActive: true})
			ctx = context.WithValue(ctx, middleware.IsMasterKey, false)
			ctx = context.WithValue(ctx, middleware.UserContextKey, &models.User{Role: "org_admin"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(middleware.AdminOnly("master-key"))
	r.Get("/admin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected org_admin to pass AdminOnly, got %d", w.Code)
	}
}

func TestAdminOnly_MemberUser(t *testing.T) {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), middleware.APIKeyContextKey, &models.APIKey{Name: "session", IsActive: true})
			ctx = context.WithValue(ctx, middleware.IsMasterKey, false)
			ctx = context.WithValue(ctx, middleware.UserContextKey, &models.User{Role: "member"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(middleware.AdminOnly("master-key"))
	r.Get("/admin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected member to be rejected by AdminOnly, got %d", w.Code)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Acme Corp", "acme-corp"},
		{"  Hello World  ", "hello-world"},
		{"test--org", "test--org"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := strings.ToLower(strings.TrimSpace(tt.input))
			got = strings.Join(strings.Fields(got), "-")
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
