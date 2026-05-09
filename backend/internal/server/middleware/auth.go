package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/types"

	"go.uber.org/zap"
)

type contextKey string

const (
	APIKeyContextKey contextKey = "api_key"
	UserContextKey   contextKey = "user"
	IsMasterKey      contextKey = "is_master"
	OrgIDContextKey  contextKey = "org_id"
)

const (
	RoleSuperAdmin = "super_admin"
	RoleOrgAdmin   = "org_admin"
	RoleTeamAdmin  = "team_admin"
	RoleMember     = "member"
	RoleViewer     = "viewer"
	RoleAdmin      = "admin" // legacy
	RoleUser       = "user"  // legacy
)

func roleLevel(role string) int {
	switch role {
	case RoleSuperAdmin:
		return 4
	case RoleOrgAdmin, RoleAdmin:
		return 3
	case RoleTeamAdmin:
		return 2
	case RoleMember, RoleUser:
		return 1
	case RoleViewer:
		return 0
	default:
		return -1
	}
}

func Auth(database *db.DB, masterKey string, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				writeError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
				return
			}

			if masterKey != "" && token == masterKey {
				ctx := context.WithValue(r.Context(), APIKeyContextKey, &models.APIKey{
					Name:     "master_key",
					IsActive: true,
				})
				ctx = context.WithValue(ctx, IsMasterKey, true)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if database == nil {
				writeError(w, http.StatusUnauthorized, "invalid API key")
				return
			}
			hash := hashToken(token)
			key, err := database.GetAPIKeyByHash(r.Context(), hash)
			if err != nil {
				if logger != nil {
					logger.Debug("api key lookup failed", zap.Error(err))
				}
				writeError(w, http.StatusUnauthorized, "invalid API key")
				return
			}

			if !key.IsActive {
				writeError(w, http.StatusForbidden, "API key is deactivated")
				return
			}

			if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
				writeError(w, http.StatusForbidden, "API key has expired")
				return
			}

			if key.MaxBudget != nil && key.TotalSpend >= *key.MaxBudget {
				writeError(w, http.StatusForbidden, "API key has exceeded its budget")
				return
			}

			ctx := context.WithValue(r.Context(), APIKeyContextKey, key)
			ctx = context.WithValue(ctx, IsMasterKey, false)

			var user *models.User
			if key.UserID != nil && database != nil {
				u, err := database.GetUser(r.Context(), *key.UserID)
				if err == nil && u != nil {
					user = u
					ctx = context.WithValue(ctx, UserContextKey, user)
				}
			}

			// Team and org budget enforcement. We look these up on every
			// LLM-plane request; both are already in the Auth path so the
			// hit is one query per cache-miss. 402 Payment Required is the
			// standard status for budget rejections.
			if key.TeamID != nil && database != nil {
				team, err := database.GetTeam(r.Context(), *key.TeamID)
				if err == nil && team != nil {
					if team.MaxBudget != nil && team.TotalSpend >= *team.MaxBudget {
						writeError(w, http.StatusPaymentRequired, "team budget exhausted")
						return
					}
					if team.OrgID != nil {
						ctx = context.WithValue(ctx, OrgIDContextKey, *team.OrgID)
					}
				}
			}

			if _, ok := ctx.Value(OrgIDContextKey).(uuid.UUID); !ok && user != nil && user.OrgID != nil {
				ctx = context.WithValue(ctx, OrgIDContextKey, *user.OrgID)
			}

			if orgID, ok := ctx.Value(OrgIDContextKey).(uuid.UUID); ok && database != nil {
				org, err := database.GetOrganization(r.Context(), orgID)
				if err == nil && org != nil {
					if !org.IsActive {
						writeError(w, http.StatusForbidden, "organization is deactivated")
						return
					}
					if org.MaxBudget != nil && org.TotalSpend >= *org.MaxBudget {
						writeError(w, http.StatusPaymentRequired, "organization budget exhausted")
						return
					}
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MasterKeyOnly is the strictest available gate. Only requests
// authenticated with the master key (i.e. the gateway operator) pass
// through. Org admins, super admins, even API keys with admin role get
// 403. Used by endpoints that mutate the operator-signed price catalog
// or countersign customer discount declarations — the moat's signed-
// pricing guarantee depends on this being non-bypassable.
func MasterKeyOnly() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsMaster(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, "operator (master key) authentication required")
		})
	}
}

func AdminOnly(masterKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := GetAPIKey(r.Context())
			if key == nil {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			if IsMaster(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r.Context())
			if user != nil && roleLevel(user.Role) >= roleLevel(RoleOrgAdmin) {
				next.ServeHTTP(w, r)
				return
			}

			writeError(w, http.StatusForbidden, "admin access required")
		})
	}
}

func GetAPIKey(ctx context.Context) *models.APIKey {
	key, _ := ctx.Value(APIKeyContextKey).(*models.APIKey)
	return key
}

func GetUser(ctx context.Context) *models.User {
	user, _ := ctx.Value(UserContextKey).(*models.User)
	return user
}

func IsMaster(ctx context.Context) bool {
	v, _ := ctx.Value(IsMasterKey).(bool)
	return v
}

func GetOrgID(ctx context.Context) *uuid.UUID {
	v, ok := ctx.Value(OrgIDContextKey).(uuid.UUID)
	if !ok {
		return nil
	}
	return &v
}

// IsSuperAdmin returns true for callers who can see across tenants: the
// master key or any user with the super_admin role. Everyone else is
// tenant-scoped.
func IsSuperAdmin(ctx context.Context) bool {
	if IsMaster(ctx) {
		return true
	}
	user := GetUser(ctx)
	return user != nil && user.Role == RoleSuperAdmin
}

// CanAccessOrg is the single choke point for tenant isolation: super admins
// can touch any org, everyone else is limited to their own org. `target`
// may be nil (e.g. a legacy row that predates orgs) in which case only
// super admins are allowed.
func CanAccessOrg(ctx context.Context, target *uuid.UUID) bool {
	if IsSuperAdmin(ctx) {
		return true
	}
	caller := GetOrgID(ctx)
	if caller == nil || target == nil {
		return false
	}
	return *caller == *target
}

func RequireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsMaster(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			user := GetUser(r.Context())
			if user != nil && roleLevel(user.Role) >= roleLevel(minRole) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, minRole+" access or higher required")
		})
	}
}

func HasRole(ctx context.Context, minRole string) bool {
	if IsMaster(ctx) {
		return true
	}
	user := GetUser(ctx)
	if user == nil {
		return false
	}
	return roleLevel(user.Role) >= roleLevel(minRole)
}

func CheckModelAccess(key *models.APIKey, modelAlias string) bool {
	if key == nil || key.Models == nil || len(key.Models) == 0 {
		return true
	}
	for _, m := range key.Models {
		if m == "*" || m == modelAlias {
			return true
		}
	}
	return false
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(types.ErrorResponse{
		Error: types.ErrorDetail{
			Message: message,
			Type:    "authentication_error",
		},
	})
}
