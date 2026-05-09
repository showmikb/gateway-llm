package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

type createOrgRequest struct {
	Name      string   `json:"name"`
	Slug      string   `json:"slug,omitempty"`
	MaxBudget *float64 `json:"max_budget,omitempty"`
}

type updateOrgRequest struct {
	Name      string   `json:"name,omitempty"`
	Slug      string   `json:"slug,omitempty"`
	MaxBudget *float64 `json:"max_budget,omitempty"`
	IsActive  *bool    `json:"is_active,omitempty"`
}

var slugRegex = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRegex.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func (h *Handlers) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	if !middleware.IsSuperAdmin(r.Context()) {
		writeErrorResp(w, http.StatusForbidden, "only super admins can create organizations")
		return
	}
	var in createOrgRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Name == "" {
		writeErrorResp(w, http.StatusBadRequest, "name is required")
		return
	}
	slug := in.Slug
	if slug == "" {
		slug = slugify(in.Name)
	}

	org := &models.Organization{
		Name:      in.Name,
		Slug:      slug,
		MaxBudget: in.MaxBudget,
		IsActive:  true,
	}
	if err := h.DB.CreateOrganization(r.Context(), org); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (h *Handlers) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	orgs, err := h.DB.ListOrganizations(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		scoped := make([]models.Organization, 0, 1)
		if callerOrg != nil {
			for _, o := range orgs {
				if o.ID == *callerOrg {
					scoped = append(scoped, o)
					break
				}
			}
		}
		orgs = scoped
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": orgs})
}

func (h *Handlers) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	org, err := h.DB.GetOrganization(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "organization not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), &org.ID) {
		writeErrorResp(w, http.StatusNotFound, "organization not found")
		return
	}

	var in updateOrgRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if in.Name != "" {
		org.Name = in.Name
	}
	if in.Slug != "" {
		org.Slug = in.Slug
	}
	if in.MaxBudget != nil {
		org.MaxBudget = in.MaxBudget
	}
	if in.IsActive != nil {
		org.IsActive = *in.IsActive
	}

	if err := h.DB.UpdateOrganization(r.Context(), org); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (h *Handlers) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	if !middleware.IsSuperAdmin(r.Context()) {
		writeErrorResp(w, http.StatusForbidden, "only super admins can delete organizations")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.DB.DeleteOrganization(r.Context(), id); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
