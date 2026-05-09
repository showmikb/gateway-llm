package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	TeamID    *uuid.UUID `json:"team_id"`
	UserID    *uuid.UUID `json:"user_id"`
	Models    []string   `json:"models"`
	RPMLimit  *int       `json:"rpm_limit"`
	TPMLimit  *int       `json:"tpm_limit"`
	MaxBudget *float64   `json:"max_budget"`
}

type createTeamRequest struct {
	Name      string     `json:"name"`
	OrgID     *uuid.UUID `json:"org_id"`
	Models    []string   `json:"models"`
	RPMLimit  *int       `json:"rpm_limit"`
	TPMLimit  *int       `json:"tpm_limit"`
	MaxBudget *float64   `json:"max_budget"`
}

type deploymentView struct {
	ModelAlias    string `json:"model_alias"`
	Provider      string `json:"provider"`
	ProviderModel string `json:"provider_model"`
	APIBase       string `json:"api_base,omitempty"`
	Priority      int    `json:"priority"`
}

func generateGatewayLLMKey() (raw string, hash string, err error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = "sk-gatewayllm-" + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}

func (h *Handlers) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in createAPIKeyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Non-super admins can only mint keys for teams in their own org.
	if in.TeamID != nil && !middleware.IsSuperAdmin(r.Context()) {
		team, terr := h.DB.GetTeam(r.Context(), *in.TeamID)
		if terr != nil {
			writeErrorResp(w, http.StatusBadRequest, "team not found")
			return
		}
		if !middleware.CanAccessOrg(r.Context(), team.OrgID) {
			writeErrorResp(w, http.StatusForbidden, "cannot create keys for teams outside your organization")
			return
		}
	}

	// If a non-super-admin leaves team_id AND user_id blank, bind the key to
	// the caller so it's visible in the org-scoped list view. Without this
	// the freshly-created key is effectively orphaned.
	if in.TeamID == nil && in.UserID == nil && !middleware.IsSuperAdmin(r.Context()) {
		caller := middleware.GetUser(r.Context())
		if caller != nil {
			id := caller.ID
			in.UserID = &id
		}
	}

	raw, tokenHash, err := generateGatewayLLMKey()
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to generate key")
		return
	}
	key := &models.APIKey{
		TokenHash: tokenHash,
		Name:      in.Name,
		TeamID:    in.TeamID,
		UserID:    in.UserID,
		Models:    in.Models,
		RPMLimit:  in.RPMLimit,
		TPMLimit:  in.TPMLimit,
		MaxBudget: in.MaxBudget,
		IsActive:  true,
	}
	if err := h.DB.CreateAPIKey(r.Context(), key); err != nil {
		h.Logger.Error("create API key", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to create API key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"key":     raw,
		"id":      key.ID,
		"name":    key.Name,
		"team_id": key.TeamID,
		"models":  key.Models,
	})
}

func (h *Handlers) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}

	if !middleware.HasRole(r.Context(), middleware.RoleOrgAdmin) {
		user := middleware.GetUser(r.Context())
		if user != nil {
			keys, err := h.DB.ListAPIKeysForUser(r.Context(), user.ID)
			if err != nil {
				writeErrorResp(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys})
			return
		}
	}

	keys, err := h.DB.ListAPIKeys(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Org admins see every key tied to their org - whether via the team the
	// key belongs to, or via the user who owns it. Keys with neither are
	// legacy/unassigned and stay hidden from non-super-admins.
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		scoped := make([]models.APIKey, 0, len(keys))
		teamOrgCache := map[uuid.UUID]*uuid.UUID{}
		userOrgCache := map[uuid.UUID]*uuid.UUID{}
		orgOf := func(teamID, userID *uuid.UUID) *uuid.UUID {
			if teamID != nil {
				if cached, ok := teamOrgCache[*teamID]; ok {
					if cached != nil {
						return cached
					}
				} else {
					team, err := h.DB.GetTeam(r.Context(), *teamID)
					if err == nil && team != nil {
						teamOrgCache[*teamID] = team.OrgID
						return team.OrgID
					}
					teamOrgCache[*teamID] = nil
				}
			}
			if userID != nil {
				if cached, ok := userOrgCache[*userID]; ok {
					return cached
				}
				user, err := h.DB.GetUser(r.Context(), *userID)
				if err == nil && user != nil {
					userOrgCache[*userID] = user.OrgID
					return user.OrgID
				}
				userOrgCache[*userID] = nil
			}
			return nil
		}
		for _, k := range keys {
			orgID := orgOf(k.TeamID, k.UserID)
			if callerOrg != nil && orgID != nil && *orgID == *callerOrg {
				scoped = append(scoped, k)
			}
		}
		keys = scoped
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys})
}

func (h *Handlers) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	// Org admin / master can delete any key in their scope. Team admins
	// can delete any key belonging to their team. Members can only
	// delete keys they personally own. Permission checks operate on the
	// key's actual owner / team, not on the caller's scope, so the
	// route's RoleTeamAdmin gate is just the lower bound.
	isOrgAdminOrMaster := middleware.HasRole(r.Context(), middleware.RoleOrgAdmin) ||
		middleware.IsMaster(r.Context())

	if !isOrgAdminOrMaster {
		user := middleware.GetUser(r.Context())
		if user == nil {
			writeErrorResp(w, http.StatusForbidden, "access denied")
			return
		}
		// Pull the caller's team scope (single membership in this app).
		var callerTeamID *uuid.UUID
		if user.TeamID != nil {
			callerTeamID = user.TeamID
		}

		// Look up the key. ListAPIKeysForUser returns keys owned by the
		// caller; supplement with team-level discovery for team_admins.
		ownedKeys, err := h.DB.ListAPIKeysForUser(r.Context(), user.ID)
		if err != nil {
			writeErrorResp(w, http.StatusInternalServerError, "failed to verify key ownership")
			return
		}
		allowed := false
		for _, k := range ownedKeys {
			if k.ID == id {
				allowed = true
				break
			}
		}
		if !allowed && middleware.HasRole(r.Context(), middleware.RoleTeamAdmin) && callerTeamID != nil {
			teamKeys, err := h.DB.ListAPIKeys(r.Context())
			if err != nil {
				writeErrorResp(w, http.StatusInternalServerError, "failed to verify key team")
				return
			}
			for _, k := range teamKeys {
				if k.ID == id && k.TeamID != nil && *k.TeamID == *callerTeamID {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			writeErrorResp(w, http.StatusNotFound, "key not found")
			return
		}
	}

	if err := h.DB.DeleteAPIKey(r.Context(), id); err != nil {
		if h.Logger != nil {
			h.Logger.Error("delete api key failed",
				zap.String("key_id", id.String()),
				zap.Error(err))
		}
		// Surface the underlying error so the UI can show something
		// actionable. FK constraints on api_keys are all ON DELETE SET
		// NULL today, so this normally only fires on transient DB
		// errors.
		writeErrorResp(w, http.StatusInternalServerError, "failed to delete key: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) CreateTeam(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in createTeamRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	orgID := in.OrgID
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		if callerOrg == nil {
			writeErrorResp(w, http.StatusForbidden, "cannot determine caller organization")
			return
		}
		if orgID != nil && *orgID != *callerOrg {
			writeErrorResp(w, http.StatusForbidden, "cannot create teams outside your organization")
			return
		}
		orgID = callerOrg
	}
	team := &models.Team{
		Name:      in.Name,
		OrgID:     orgID,
		Models:    in.Models,
		RPMLimit:  in.RPMLimit,
		TPMLimit:  in.TPMLimit,
		MaxBudget: in.MaxBudget,
	}
	if err := h.DB.CreateTeam(r.Context(), team); err != nil {
		h.Logger.Error("create team", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to create team")
		return
	}
	writeJSON(w, http.StatusCreated, team)
}

func (h *Handlers) ListTeams(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	teams, err := h.DB.ListTeams(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		scoped := make([]models.Team, 0, len(teams))
		for _, t := range teams {
			if callerOrg != nil && t.OrgID != nil && *t.OrgID == *callerOrg {
				scoped = append(scoped, t)
			}
		}
		teams = scoped
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": teams})
}

func (h *Handlers) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	team, err := h.DB.GetTeam(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "team not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), team.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "team not found")
		return
	}
	if err := h.DB.DeactivateTeamKeys(r.Context(), id); err != nil {
		h.Logger.Warn("cascade deactivate team keys", zap.Error(err))
	}
	if err := h.DB.DeleteTeam(r.Context(), id); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) GetUsage(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 10000 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	if !middleware.HasRole(r.Context(), middleware.RoleOrgAdmin) {
		user := middleware.GetUser(r.Context())
		if user != nil {
			logs, err := h.DB.GetSpendLogsForUser(r.Context(), user.ID, limit, offset)
			if err != nil {
				writeErrorResp(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs})
			return
		}
	}

	// Org admins see spend for their org; super admins see everything.
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		if callerOrg == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": []interface{}{}})
			return
		}
		logs, err := h.DB.GetSpendLogsForOrg(r.Context(), *callerOrg, limit, offset)
		if err != nil {
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs})
		return
	}

	logs, err := h.DB.GetSpendLogs(r.Context(), limit, offset)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs})
}

func (h *Handlers) GetDailyUsage(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}

	if !middleware.HasRole(r.Context(), middleware.RoleOrgAdmin) {
		user := middleware.GetUser(r.Context())
		if user != nil {
			spends, err := h.DB.GetDailySpendForUser(r.Context(), user.ID, days)
			if err != nil {
				writeErrorResp(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": spends})
			return
		}
	}

	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		if callerOrg == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": []interface{}{}})
			return
		}
		spends, err := h.DB.GetDailySpendForOrg(r.Context(), *callerOrg, days)
		if err != nil {
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": spends})
		return
	}

	spends, err := h.DB.GetDailySpend(r.Context(), days)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": spends})
}

// canSeeKey returns true when the caller is allowed to view usage for
// the given key. Org admins and master see all keys in their org;
// team admins see keys in their team; everyone else only their own.
func (h *Handlers) canSeeKey(r *http.Request, key *models.APIKey) bool {
	if middleware.IsMaster(r.Context()) || middleware.IsSuperAdmin(r.Context()) {
		return true
	}
	user := middleware.GetUser(r.Context())
	if user == nil {
		return false
	}
	if middleware.HasRole(r.Context(), middleware.RoleOrgAdmin) {
		// Org admins are scoped to their org. Until api_keys grow an
		// explicit org_id column, fall back to team-or-owner scoping.
		if key.UserID != nil && *key.UserID == user.ID {
			return true
		}
		if user.TeamID != nil && key.TeamID != nil && *key.TeamID == *user.TeamID {
			return true
		}
		// Allow org admins to see un-teamed, un-owned keys (legacy).
		if key.UserID == nil && key.TeamID == nil {
			return true
		}
		return false
	}
	if middleware.HasRole(r.Context(), middleware.RoleTeamAdmin) &&
		user.TeamID != nil && key.TeamID != nil && *key.TeamID == *user.TeamID {
		return true
	}
	return key.UserID != nil && *key.UserID == user.ID
}

// loadKeyForUsage fetches a key by id and authorizes the caller to
// view its usage. Returns 404 to avoid leaking key existence to
// unauthorized callers.
func (h *Handlers) loadKeyForUsage(w http.ResponseWriter, r *http.Request) (*models.APIKey, bool) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return nil, false
	}
	keys, err := h.DB.ListAPIKeys(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	for i := range keys {
		if keys[i].ID == id {
			if !h.canSeeKey(r, &keys[i]) {
				writeErrorResp(w, http.StatusNotFound, "key not found")
				return nil, false
			}
			return &keys[i], true
		}
	}
	writeErrorResp(w, http.StatusNotFound, "key not found")
	return nil, false
}

// GetKeyUsage returns recent spend log rows for a single API key,
// paginated. Drives the per-key drill-down page (/keys/{id}).
func (h *Handlers) GetKeyUsage(w http.ResponseWriter, r *http.Request) {
	key, ok := h.loadKeyForUsage(w, r)
	if !ok {
		return
	}
	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	logs, err := h.DB.GetSpendLogsForKey(r.Context(), key.ID, limit, offset)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": logs,
		"key":  key,
	})
}

// GetKeyDailyUsage returns aggregated daily spend for a single key
// over the last `days` days. Used for the per-key spend-over-time chart.
func (h *Handlers) GetKeyDailyUsage(w http.ResponseWriter, r *http.Request) {
	key, ok := h.loadKeyForUsage(w, r)
	if !ok {
		return
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	spends, err := h.DB.GetDailySpendForKey(r.Context(), key.ID, days)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": spends})
}

func (h *Handlers) GetDeployments(w http.ResponseWriter, r *http.Request) {
	if h.DB != nil {
		deps, err := h.DB.ListDeployments(r.Context())
		if err == nil && len(deps) > 0 {
			var filterOrgID *uuid.UUID
			if raw := r.URL.Query().Get("org_id"); raw != "" {
				id, err := uuid.Parse(raw)
				if err != nil {
					writeErrorResp(w, http.StatusBadRequest, "invalid org_id")
					return
				}
				filterOrgID = &id
			}
			// Non-super admins always get scoped to their own org regardless of the query param.
			if !middleware.IsSuperAdmin(r.Context()) {
				filterOrgID = middleware.GetOrgID(r.Context())
			}
			if filterOrgID != nil {
				filtered := make([]models.Deployment, 0)
				for _, d := range deps {
					if d.OrgID != nil && *d.OrgID == *filterOrgID {
						filtered = append(filtered, d)
					}
				}
				deps = filtered
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": deps})
			return
		}
	}

	aliases := h.Router.ListAliases()
	sort.Strings(aliases)
	var out []deploymentView
	for _, alias := range aliases {
		for _, d := range h.Router.GetDeployments(alias) {
			if d == nil {
				continue
			}
			out = append(out, deploymentView{
				ModelAlias:    alias,
				Provider:      d.Provider,
				ProviderModel: d.ProviderModel,
				APIBase:       d.APIBase,
				Priority:      d.Priority,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}

// --- User management ---

type createUserRequest struct {
	Email    string     `json:"email"`
	Password string     `json:"password"`
	Role     string     `json:"role"`
	TeamID   *uuid.UUID `json:"team_id"`
	OrgID    *uuid.UUID `json:"org_id"`
}

type updateUserRequest struct {
	Email    string     `json:"email,omitempty"`
	Role     string     `json:"role,omitempty"`
	TeamID   *uuid.UUID `json:"team_id"`
	OrgID    *uuid.UUID `json:"org_id,omitempty"`
	IsActive *bool      `json:"is_active,omitempty"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in createUserRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Email == "" || in.Password == "" {
		writeErrorResp(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if in.Role == "" {
		in.Role = "member"
	}
	validRoles := map[string]bool{
		"super_admin": true, "org_admin": true, "team_admin": true, "member": true,
		"viewer": true, "admin": true, "user": true,
	}
	if !validRoles[in.Role] {
		writeErrorResp(w, http.StatusBadRequest, "role must be super_admin, org_admin, team_admin, member, or viewer")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// Scope rules:
	//   - super admins may target any org
	//   - everyone else (org_admin / team_admin on this route) is forced into their own org
	orgID := in.OrgID
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		if callerOrg == nil {
			writeErrorResp(w, http.StatusForbidden, "cannot determine caller organization")
			return
		}
		if orgID != nil && *orgID != *callerOrg {
			writeErrorResp(w, http.StatusForbidden, "cannot create users outside your organization")
			return
		}
		orgID = callerOrg
		// Non-super admins cannot mint super admins or other org admins above themselves.
		if in.Role == "super_admin" {
			writeErrorResp(w, http.StatusForbidden, "only super admins can assign super_admin role")
			return
		}
	}

	// If a team was supplied, make sure it belongs to the target org.
	if in.TeamID != nil {
		team, terr := h.DB.GetTeam(r.Context(), *in.TeamID)
		if terr != nil {
			writeErrorResp(w, http.StatusBadRequest, "team not found")
			return
		}
		if orgID != nil && team.OrgID != nil && *team.OrgID != *orgID {
			writeErrorResp(w, http.StatusBadRequest, "team does not belong to the target organization")
			return
		}
	}

	user := &models.User{
		Email:        in.Email,
		Role:         in.Role,
		TeamID:       in.TeamID,
		OrgID:        orgID,
		PasswordHash: string(hash),
		IsActive:     true,
	}
	if err := h.DB.CreateUser(r.Context(), user); err != nil {
		h.Logger.Error("create user", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	users, err := h.DB.ListUsers(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		scoped := make([]models.User, 0, len(users))
		for _, u := range users {
			if callerOrg != nil && u.OrgID != nil && *u.OrgID == *callerOrg {
				scoped = append(scoped, u)
			}
		}
		users = scoped
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": users})
}

func (h *Handlers) GetUser(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctxUser := middleware.GetUser(r.Context())
	isSelf := ctxUser != nil && ctxUser.ID == id
	if !middleware.HasRole(r.Context(), middleware.RoleOrgAdmin) && !isSelf {
		writeErrorResp(w, http.StatusForbidden, "access denied")
		return
	}

	user, err := h.DB.GetUser(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "user not found")
		return
	}
	if !isSelf && !middleware.CanAccessOrg(r.Context(), user.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *Handlers) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctxUser := middleware.GetUser(r.Context())
	isAdmin := middleware.HasRole(r.Context(), middleware.RoleOrgAdmin)
	isSelf := ctxUser != nil && ctxUser.ID == id

	if !isAdmin && !isSelf {
		writeErrorResp(w, http.StatusForbidden, "access denied")
		return
	}

	var in updateUserRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	user, err := h.DB.GetUser(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "user not found")
		return
	}
	if !isSelf && !middleware.CanAccessOrg(r.Context(), user.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "user not found")
		return
	}

	if in.Email != "" {
		user.Email = in.Email
	}
	if in.Role != "" {
		if !isAdmin {
			writeErrorResp(w, http.StatusForbidden, "only admins can change roles")
			return
		}
		user.Role = in.Role
	}
	if in.TeamID != nil {
		user.TeamID = in.TeamID
	}
	if in.IsActive != nil {
		if !isAdmin {
			writeErrorResp(w, http.StatusForbidden, "only admins can change active status")
			return
		}
		user.IsActive = *in.IsActive
	}

	if err := h.DB.UpdateUser(r.Context(), user); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	target, err := h.DB.GetUser(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "user not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), target.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "user not found")
		return
	}
	if err := h.DB.DeactivateUserKeys(r.Context(), id); err != nil {
		h.Logger.Warn("cascade deactivate user keys", zap.Error(err))
	}
	if err := h.DB.DeleteUser(r.Context(), id); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) LoginUser(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in loginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Email == "" || in.Password == "" {
		writeErrorResp(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.DB.GetUserByEmail(r.Context(), in.Email)
	if err != nil {
		writeErrorResp(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if !user.IsActive {
		writeErrorResp(w, http.StatusForbidden, "account is deactivated")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)); err != nil {
		writeErrorResp(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	raw, tokenHash, err := generateGatewayLLMKey()
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to generate session token")
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	sessionKey := &models.APIKey{
		TokenHash: tokenHash,
		Name:      "session:" + user.Email,
		UserID:    &user.ID,
		IsActive:  true,
		ExpiresAt: &expires,
	}
	if err := h.DB.CreateAPIKey(r.Context(), sessionKey); err != nil {
		h.Logger.Error("create session key", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":      raw,
		"expires_at": expires.Format(time.RFC3339),
		"user":       user,
	})
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name,omitempty"`
}

// RegisterUser is a PUBLIC endpoint that allows a visitor to create their own
// account. It auto-provisions a personal organization + default team so the new
// user becomes the org_admin of their own isolated workspace. This lets anyone
// try the gateway with their own provider keys without giving them access to
// other tenants' data.
func (h *Handlers) RegisterUser(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}

	var in registerRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	email := strings.TrimSpace(strings.ToLower(in.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeErrorResp(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(in.Password) < 8 {
		writeErrorResp(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	if existing, err := h.DB.GetUserByEmail(r.Context(), email); err == nil && existing != nil {
		writeErrorResp(w, http.StatusConflict, "an account with this email already exists")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	orgName := in.Name
	if orgName == "" {
		if idx := strings.Index(email, "@"); idx > 0 {
			orgName = email[:idx]
		} else {
			orgName = email
		}
	}
	orgName = orgName + "'s Workspace"
	slug := slugifyRegister(email) + "-" + randomSuffix(6)

	org := &models.Organization{
		Name:     orgName,
		Slug:     slug,
		IsActive: true,
	}
	if err := h.DB.CreateOrganization(r.Context(), org); err != nil {
		h.Logger.Error("register: create organization", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to provision workspace")
		return
	}

	team := &models.Team{
		Name:  "Default",
		OrgID: &org.ID,
	}
	if err := h.DB.CreateTeam(r.Context(), team); err != nil {
		h.Logger.Error("register: create team", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to provision team")
		return
	}

	user := &models.User{
		Email:        email,
		Role:         "org_admin",
		TeamID:       &team.ID,
		OrgID:        &org.ID,
		PasswordHash: string(hash),
		IsActive:     true,
	}
	if err := h.DB.CreateUser(r.Context(), user); err != nil {
		h.Logger.Error("register: create user", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	raw, tokenHash, err := generateGatewayLLMKey()
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to generate session token")
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	sessionKey := &models.APIKey{
		TokenHash: tokenHash,
		Name:      "session:" + user.Email,
		UserID:    &user.ID,
		TeamID:    &team.ID,
		IsActive:  true,
		ExpiresAt: &expires,
	}
	if err := h.DB.CreateAPIKey(r.Context(), sessionKey); err != nil {
		h.Logger.Error("register: create session", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token":        raw,
		"expires_at":   expires.Format(time.RFC3339),
		"user":         user,
		"organization": org,
		"team":         team,
	})
}

func slugifyRegister(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func randomSuffix(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "xxxxxx"
	}
	return hex.EncodeToString(b)[:n]
}
