package handlers

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/ir"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/receipt"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"go.uber.org/zap"
)

// issueReceipt builds, signs, and persists the Ed25519 billing receipt
// for a completed chat call. It never blocks the hot path: the DB write
// is fire-and-forget in a goroutine.
func (h *Handlers) issueReceipt(
	ctx context.Context,
	w http.ResponseWriter,
	irReq *ir.ChatRequest,
	resp *types.ChatCompletionResponse,
	dep *router.DeploymentInfo,
	alias string,
	traceID string,
	key *models.APIKey,
	usage cost.UsageInfo,
	cr *cost.CostResult,
) {
	if h.Receipts == nil || dep == nil {
		return
	}
	reqHash := ""
	if irReq != nil {
		if b, err := json.Marshal(irReq); err == nil {
			reqHash = receipt.HashBytes(b)
		}
	}
	respHash := ""
	if resp != nil {
		if b, err := json.Marshal(resp); err == nil {
			respHash = receipt.HashBytes(b)
		}
	}

	rc := &receipt.Receipt{
		TraceID:       traceID,
		Alias:         alias,
		Provider:      dep.Provider,
		ProviderModel: dep.ProviderModel,
		ReqHash:       reqHash,
		RespHash:      respHash,
		PromptTokens:  usage.PromptTokens,
		OutputTokens:  usage.CompletionTokens,
		TotalTokens:   usage.PromptTokens + usage.CompletionTokens,
	}
	if cr != nil {
		rc.CostUSD = cr.TotalCost
	}
	if oid := middleware.GetOrgID(ctx); oid != nil {
		rc.OrgID = oid.String()
	}
	if key != nil {
		rc.APIKeyID = key.ID.String()
	}

	signed, err := h.Receipts.Sign(rc)
	if err != nil {
		h.Logger.Warn("sign receipt failed", zap.Error(err))
		return
	}
	w.Header().Set("X-Gateway-LLM-Receipt-Id", signed.ID)
	w.Header().Set("X-Gateway-LLM-Receipt-Key-Id", signed.PublicKeyID)

	// Persist asynchronously so DB hiccups never affect the caller.
	go func(r *receipt.Receipt) {
		if h.DB == nil {
			return
		}
		bctx, cancel := context.WithTimeout(context.Background(), 5_000_000_000)
		defer cancel()
		if err := h.DB.InsertReceipt(bctx, r); err != nil {
			h.Logger.Debug("insert receipt failed", zap.Error(err))
		}
	}(signed)
}

// GetReceipt is GET /v1/receipts/{id}. Returns the signed receipt so
// an auditor can verify it against the published public key.
func (h *Handlers) GetReceipt(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.DB == nil {
		writeErrorResp(w, http.StatusNotImplemented, "receipt store unavailable")
		return
	}
	rc, err := h.DB.GetReceipt(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "receipt not found")
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

// WellKnownReceiptKey serves /.well-known/gateway-llm-receipts.json with
// the Ed25519 public key in both JWK and raw-base64 form so both CLI
// verifiers and in-browser @noble/ed25519 can consume it.
func (h *Handlers) WellKnownReceiptKey(w http.ResponseWriter, r *http.Request) {
	if h.Receipts == nil {
		writeErrorResp(w, http.StatusNotFound, "receipts not configured")
		return
	}
	pub := h.Receipts.PublicKey()
	b64 := base64.StdEncoding.EncodeToString(pub)
	jwk := map[string]any{
		"kty": "OKP",
		"crv": "Ed25519",
		"x":   base64.RawURLEncoding.EncodeToString(pub),
		"use": "sig",
		"alg": "EdDSA",
		"kid": h.Receipts.KeyID(),
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key_id":      h.Receipts.KeyID(),
		"algorithm":   "Ed25519",
		"public_key":  b64,
		"public_size": ed25519.PublicKeySize,
		"jwk":         jwk,
	})
}
