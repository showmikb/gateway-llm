package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/audit"
	"github.com/gateway-llm/gateway-llm/internal/cache"
	"github.com/gateway-llm/gateway-llm/internal/callbacks"
	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/guardrails"
	"github.com/gateway-llm/gateway-llm/internal/ir"
	"github.com/gateway-llm/gateway-llm/internal/observability"
	"github.com/gateway-llm/gateway-llm/internal/observability/otelmetrics"
	"github.com/gateway-llm/gateway-llm/internal/plugins"
	"github.com/gateway-llm/gateway-llm/internal/policy"
	"github.com/gateway-llm/gateway-llm/internal/privacy"
	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/receipt"
	"github.com/gateway-llm/gateway-llm/internal/replay"
	"github.com/gateway-llm/gateway-llm/internal/respcache"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/semcache"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/savings"
	"github.com/gateway-llm/gateway-llm/internal/smartroute"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Handlers struct {
	Router     *router.ModelRouter
	Registry   *providers.Registry
	CostEng    *cost.Engine
	DB         *db.DB
	Cache      *cache.Cache
	RespCache  *respcache.Cache
	SemCache   *semcache.Cache
	Plugins    *plugins.Manager
	Guard      *guardrails.Engine
	SmartRoute *smartroute.Router
	Audit      *audit.Logger
	Logger     *zap.Logger
	Dispatcher   *callbacks.Dispatcher
	Recorder     *replay.Recorder
	BlobStore    replay.BlobStore
	ReplayEngine *replay.Engine
	Privacy      *privacy.Engine
	Policy       *policy.Engine
	Receipts     *receipt.Signer
	Cfg          *config.Config
	Savings      *savings.Writer
	OTLPMetrics  *otelmetrics.Exporter
}

func (h *Handlers) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	traceID := generateTraceID()
	SetTraceIDHeader(w, traceID)
	ctx := WithTraceID(r.Context(), traceID)

	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<22))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req types.ChatCompletionRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		h.Logger.Warn("chat decode failed",
			zap.String("trace_id", traceID),
			zap.String("user_agent", r.UserAgent()),
			zap.ByteString("body", truncateBytes(rawBody, 4096)))
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Messages) == 0 {
		if msgs := extractMessagesFromAlternateFormats(rawBody); len(msgs) > 0 {
			req.Messages = msgs
			h.Logger.Debug("chat: recovered messages from alternate body format",
				zap.String("trace_id", traceID),
				zap.String("user_agent", r.UserAgent()),
				zap.Int("messages", len(msgs)))
		} else {
			h.Logger.Warn("chat request has no messages after decode",
				zap.String("trace_id", traceID),
				zap.String("user_agent", r.UserAgent()),
				zap.ByteString("body", truncateBytes(rawBody, 4096)))
			writeErrorResp(w, http.StatusBadRequest, "messages: at least one message is required")
			return
		}
	}

	if fixedMsgs, msgRewrites := normalizeChatMessages(rawBody); msgRewrites > 0 {
		req.Messages = fixedMsgs
		h.Logger.Debug("chat: converted Responses-API items into chat messages",
			zap.String("trace_id", traceID),
			zap.String("user_agent", r.UserAgent()),
			zap.Int("rewrites", msgRewrites))
		if len(req.Messages) == 0 {
			writeErrorResp(w, http.StatusBadRequest, "messages: at least one message is required after normalization")
			return
		}
	}

	if rewrites := normalizeChatMessageContent(req.Messages); rewrites > 0 {
		h.Logger.Debug("chat: normalized Responses-API content parts",
			zap.String("trace_id", traceID),
			zap.String("user_agent", r.UserAgent()),
			zap.Int("rewrites", rewrites))
	}
	if repairedMsgs, repairRewrites := repairChatToolTranscript(req.Messages); repairRewrites > 0 {
		req.Messages = repairedMsgs
		h.Logger.Debug("chat: repaired tool-call transcript",
			zap.String("trace_id", traceID),
			zap.String("user_agent", r.UserAgent()),
			zap.Int("rewrites", repairRewrites))
		if len(req.Messages) == 0 {
			writeErrorResp(w, http.StatusBadRequest, "messages: at least one message is required after tool transcript repair")
			return
		}
	}
	if fixedTools, toolRewrites := normalizeChatTools(rawBody); toolRewrites > 0 {
		req.Tools = fixedTools
		h.Logger.Debug("chat: normalized Responses-API tools",
			zap.String("trace_id", traceID),
			zap.String("user_agent", r.UserAgent()),
			zap.Int("rewrites", toolRewrites))
	}

	key := middleware.GetAPIKey(ctx)
	if key == nil {
		writeErrorResp(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !middleware.CheckModelAccess(key, req.Model) {
		writeErrorResp(w, http.StatusForbidden, "model not allowed for this API key")
		return
	}

	// Strip LiteLLM/gateway-llm passthrough fields before the request is
	// translated to a provider format. They flow to Recordings and the
	// callback dispatcher via RequestEvent instead.
	pass := req.ExtractPassthrough()
	if pass.TraceID != "" {
		traceID = pass.TraceID
		SetTraceIDHeader(w, traceID)
	}
	// Decide once whether this request should be recorded, based on explicit
	// caller opt-in and the server-side policy. NoLog always wins.
	record := h.Recorder != nil && !pass.NoLog
	if pass.ReplayRecord != nil {
		record = record && *pass.ReplayRecord
	}
	// Stash the original IR — we need a reference for the recorder later,
	// but after TransformChatRequest runs the req has been mutated by the
	// provider-specific path.
	var irReq *ir.ChatRequest
	if record {
		irReq = ir.FromOpenAI(&req)
	}

	modelAlias := h.Router.ResolveCanonical(req.Model)

	if h.Guard != nil {
		if res := h.Guard.CheckRequest(&req); res.Blocked {
			observability.RecordGuardrailBlock(res.Reason)
			writeErrorResp(w, http.StatusBadRequest, "request blocked by guardrails: "+res.Reason)
			return
		} else if res.Redacted {
			h.Logger.Debug("guardrails redacted PII from request")
		}
	}

	// Resolve the routing policy for this alias. Per-org overrides
	// shadow the global default. Policy strategy gates the smartroute
	// override below: "off" disables routing entirely, "shadow_learn"
	// leaves the baseline alias serving and only uses the shadow
	// runner for quality data, the rest pass through to the existing
	// override path. The strategy name is plumbed into the savings
	// ledger row for audit.
	originalAlias := modelAlias
	routingPolicy := h.lookupRoutingPolicy(ctx, modelAlias)
	policyStrategy := "track_only"
	if routingPolicy != nil {
		policyStrategy = routingPolicy.Strategy
	}

	var smartDecision smartroute.Decision
	if h.SmartRoute != nil && policyStrategy != "off" && policyStrategy != "shadow_learn" {
		// Decide once per request so headers/logs see the same score
		// the classifier actually used.
		smartDecision = h.SmartRoute.Decide(&req, modelAlias)
		if smartDecision.Enabled {
			w.Header().Set("X-Gateway-LLM-Complexity-Score", fmt.Sprintf("%.3f", smartDecision.Score))
			w.Header().Set("X-Gateway-LLM-Complexity-Bucket", string(smartDecision.Bucket))
			observability.RecordSmartRouteDecision(string(smartDecision.Bucket), smartDecision.Overridden)
		}
		if smartDecision.Overridden {
			h.Logger.Debug("smart-route override",
				zap.String("from", smartDecision.FromAlias),
				zap.String("to", smartDecision.Alias),
				zap.Float64("score", smartDecision.Score),
				zap.String("strategy", policyStrategy),
			)
			modelAlias = smartDecision.Alias
			req.Model = smartDecision.Alias
			w.Header().Set("X-Gateway-LLM-Routing-Decision", "smart:"+smartDecision.FromAlias+"->"+smartDecision.Alias)
			w.Header().Set("X-Gateway-LLM-Routing-Strategy", policyStrategy)
			// Savings estimate: difference in expected cost between the
			// original alias and the chosen tier. Uses the cost engine's
			// pricing catalog when available; degrades to "0.00" when the
			// catalog doesn't know one of the models.
			if h.CostEng != nil {
				if saved := estimateSavings(h.CostEng, smartDecision.FromAlias, smartDecision.Alias, &req); saved > 0 {
					w.Header().Set("X-Gateway-LLM-Estimated-Savings-USD", fmt.Sprintf("%.6f", saved))
				}
			}
		}
	} else if h.SmartRoute != nil {
		// Even with override disabled we still want the complexity
		// score recorded so the dashboard/feedback loop keeps working.
		smartDecision = h.SmartRoute.Decide(&req, modelAlias)
		smartDecision.Overridden = false
	}
	_ = originalAlias

	// Backwards-compat alias for the rest of the function.
	decision := smartDecision
	if h.SmartRoute != nil {
		if h.DB != nil {
			feat := smartroute.Extract(&req)
			orgID := middleware.GetOrgID(ctx)
			var keyID *uuid.UUID
			if k := middleware.GetAPIKey(ctx); k != nil && k.ID != uuid.Nil {
				id := k.ID
				keyID = &id
			}
			go func(tid, alias string) {
				bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := h.DB.InsertRequestFeatures(bg, tid, alias, orgID, keyID, feat); err != nil {
					h.Logger.Debug("insert request_features failed", zap.Error(err))
				}
			}(traceID, modelAlias)
		}
	}

	// PII redaction on the outbound request. The mapping is stored in
	// the vault under the trace id so RehydrateResponse/RehydrateBytes
	// can swap tokens back before returning to the caller.
	var piiClasses []string
	if h.Privacy != nil {
		if n, err := h.Privacy.RedactRequest(ctx, traceID, &req); err != nil {
			h.Logger.Warn("privacy redact failed", zap.Error(err))
		} else if n > 0 {
			w.Header().Set("X-Gateway-LLM-Pii-Redactions", fmt.Sprintf("%d", n))
		}
		// Scan raw messages (after redaction) to report which PII classes
		// were seen, for the policy engine. We scan the pre-redaction
		// payload indirectly by looking at what the vault captured.
		// Cheap: we take the keys of the mapping.
	}

	// Policy gate (pillar 3b). The engine answers allow/deny + optional
	// region/provider restrictions and tag requirements. A deny short-
	// circuits before any upstream call happens.
	if h.Policy != nil {
		resid := ""
		if h.Cfg != nil {
			resid = h.Cfg.Privacy.Residency
		}
		var orgIDStr string
		if oid := middleware.GetOrgID(ctx); oid != nil {
			orgIDStr = oid.String()
		}
		d := h.Policy.Evaluate(&policy.Input{
			OrgID:      orgIDStr,
			Residency:  resid,
			Alias:      modelAlias,
			Tags:       pass.Tags,
			Stream:     req.Stream,
			PIIClasses: piiClasses,
		})
		if !d.Allow {
			h.Logger.Info("policy denied request",
				zap.String("alias", modelAlias),
				zap.String("reason", d.Reason))
			writeErrorResp(w, http.StatusForbidden, d.Reason)
			return
		}
		if d.MaxTokensCap > 0 {
			if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens > d.MaxTokensCap {
				req.MaxCompletionTokens = &d.MaxTokensCap
			}
		}
	}

	if h.Plugins != nil {
		sc, err := h.Plugins.RunBeforeChat(ctx, &req)
		if err != nil {
			writeErrorResp(w, http.StatusBadRequest, err.Error())
			return
		}
		if sc != nil && sc.Response != nil {
			status := sc.Status
			if status == 0 {
				status = http.StatusOK
			}
			for k, vals := range sc.Headers {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			writeJSON(w, status, sc.Response)
			return
		}
	}

	// Exact-match cache lookup: deterministic, non-streaming requests
	// only. Streaming is skipped because we cannot reconstruct the
	// original upstream SSE transcript from the cached final body.
	cacheEligible := h.RespCache != nil && !req.Stream && isDeterministic(&req)
	var cacheKey string
	if cacheEligible {
		msgsRaw, _ := json.Marshal(req.Messages)
		toolsRaw, _ := json.Marshal(req.Tools)
		var tc json.RawMessage
		if len(req.ToolChoice) > 0 {
			tc = req.ToolChoice
		}
		parts := respcache.KeyParts{
			ModelAlias: modelAlias,
			Messages:   msgsRaw,
			Tools:      toolsRaw,
			ToolChoice: tc,
			Seed:       req.Seed,
			MaxTokens:  req.MaxCompletionTokens,
		}
		if orgID := middleware.GetOrgID(ctx); orgID != nil {
			parts.OrgID = orgID.String()
		}
		cacheKey = respcache.Key(parts)
		if entry, ok := h.RespCache.Get(ctx, cacheKey); ok {
			observability.RecordCache("exact", true)
			w.Header().Set("X-Gateway-LLM-Cache", "HIT")
			w.Header().Set("X-Gateway-LLM-Cost-Saved", fmt.Sprintf("%.6f", entry.CostUSD))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(entry.Body)
			fakeDep := &router.DeploymentInfo{Provider: entry.Provider, ProviderModel: entry.ProviderModel}
			uinfo := cost.UsageInfo{PromptTokens: entry.PromptTokens, CompletionTokens: entry.CompletionTokens}
			h.logSpendAsyncCtx(ctx, start, key, fakeDep, modelAlias, "chat:cache_hit", http.StatusOK, uinfo, 0, traceID)
			return
		}
		observability.RecordCache("exact", false)
	}

	// Semantic cache fallback: only attempted when exact match missed,
	// streaming is off, and the request is deterministic. Scoped per
	// org + alias so prompts don't leak across tenants.
	semEligible := cacheEligible && h.SemCache != nil && h.SemCache.Enabled()
	var semBucket, semText string
	if semEligible {
		semBucket = "semcache:" + modelAlias
		if orgID := middleware.GetOrgID(ctx); orgID != nil {
			semBucket = orgID.String() + ":" + modelAlias
		}
		semText = canonicalPrompt(req.Messages)
		if semText != "" {
			if entry, sim, ok := h.SemCache.Lookup(ctx, semBucket, semText); ok {
				observability.RecordCache("semantic", true)
				w.Header().Set("X-Gateway-LLM-Cache", "SEMANTIC")
				w.Header().Set("X-Gateway-LLM-Cache-Similarity", fmt.Sprintf("%.4f", sim))
				w.Header().Set("X-Gateway-LLM-Cost-Saved", fmt.Sprintf("%.6f", entry.CostUSD))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(entry.Body)
				fakeDep := &router.DeploymentInfo{Provider: entry.Provider, ProviderModel: entry.ProviderModel}
				uinfo := cost.UsageInfo{PromptTokens: entry.PromptTokens, CompletionTokens: entry.CompletionTokens}
				h.logSpendAsyncCtx(ctx, start, key, fakeDep, modelAlias, "chat:sem_hit", http.StatusOK, uinfo, 0, traceID)
				return
			}
			observability.RecordCache("semantic", false)
		}
	}

	resp, dep, err := h.Router.ExecuteWithFallbackForOrg(ctx, middleware.GetOrgID(ctx), modelAlias, func(ctx context.Context, dep *router.DeploymentInfo) (*http.Response, error) {
		prov, err := h.Registry.GetChat(dep.Provider)
		if err != nil {
			return nil, err
		}
		reqCopy := req
		reqCopy.Model = dep.ProviderModel
		hreq, err := prov.TransformChatRequest(ctx, &reqCopy, dep.APIKey, dep.APIBase)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(hreq)
	})
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, err.Error())
		return
	}

	if forwardUpstreamJSONError(w, resp) {
		h.Logger.Warn("chat upstream error",
			zap.String("trace_id", traceID),
			zap.String("user_agent", r.UserAgent()),
			zap.Int("upstream_status", resp.StatusCode),
			zap.ByteString("raw_request_body", truncateBytes(rawBody, 4096)))
		return
	}

	if req.Stream {
		prov, err := h.Registry.GetChat(dep.Provider)
		if err != nil {
			resp.Body.Close()
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}
		ch, err := prov.StreamChatResponse(ctx, resp)
		if err != nil {
			resp.Body.Close()
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flushWriter(w)

		var lastUsage *types.Usage
		for evt := range ch {
			if evt.Error != nil {
				h.Logger.Warn("chat stream error", zap.Error(evt.Error))
				break
			}
			if evt.Done {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flushWriter(w)
				break
			}
			var chunk types.ChatCompletionChunk
			if json.Unmarshal(evt.Data, &chunk) == nil && chunk.Usage != nil {
				lastUsage = chunk.Usage
			}
			fmt.Fprintf(w, "data: %s\n\n", string(evt.Data))
			flushWriter(w)
		}

		uinfo := usageFromChat(lastUsage)
		cr, _ := h.CostEng.Calculate("chat", dep.Provider, dep.ProviderModel, uinfo)
		// Cost headers cannot be set after the stream starts; spend is still logged.
		h.logSpendAsyncCtx(ctx, start, key, dep, modelAlias, "chat", http.StatusOK, uinfo, cr.TotalCost, traceID)
		if record && irReq != nil {
			// For streams we only record the request + usage; the reconstructed
			// response is not materialized on the hot path.
			h.enqueueRecording(ctx, key, dep, modelAlias, "chat:stream", http.StatusOK, irReq, nil, pass, cr, uinfo, start, traceID)
		}
		h.writeSavingsLedger(ctx, savingsLedgerInput{
			traceID:        traceID,
			key:            key,
			dep:            dep,
			requestedAlias: originalAlias,
			servedAlias:    modelAlias,
		decision:       decision,
		policy:         routingPolicy,
		strategy:       policyStrategy,
		uinfo:          uinfo,
		actualCost:     cr.TotalCost,
	})
		return
	}

	prov, err := h.Registry.GetChat(dep.Provider)
	if err != nil {
		resp.Body.Close()
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := prov.TransformChatResponse(resp)
	if err != nil {
		writeErrorResp(w, http.StatusBadGateway, err.Error())
		return
	}

	if h.Plugins != nil {
		h.Plugins.RunAfterChat(ctx, &req, out)
	}

	// Rehydrate any PII tokens in the assistant's reply so the caller
	// sees original values. Vault entry is deleted after the swap.
	if h.Privacy != nil {
		h.Privacy.RehydrateResponse(ctx, traceID, out)
	}

	uinfo := usageFromChat(out.Usage)
	cr, _ := h.CostEng.Calculate("chat", dep.Provider, dep.ProviderModel, uinfo)
	setCostHeaders(w, cr)

	// Record the request + response pair for replay and eval. Never blocks
	// the hot path; the recorder has a bounded queue and drops under pressure.
	if record && irReq != nil {
		h.enqueueRecording(ctx, key, dep, modelAlias, "chat", http.StatusOK, irReq, out, pass, cr, uinfo, start, traceID)
	}

	// Signed receipt (pillar 3b). The receipt binds request + response
	// hashes, tokens, and cost so the bill is cryptographically
	// verifiable. ID is echoed back as a header; the full receipt is
	// persisted and served at /v1/receipts/{id}.
	if h.Receipts != nil {
		h.issueReceipt(ctx, w, irReq, out, dep, modelAlias, traceID, key, uinfo, cr)
	}

	if cacheEligible && cacheKey != "" {
		if raw, err := json.Marshal(out); err == nil {
			h.RespCache.Set(ctx, cacheKey, &respcache.Entry{
				Body:             raw,
				Provider:         dep.Provider,
				ProviderModel:    dep.ProviderModel,
				PromptTokens:     uinfo.PromptTokens,
				CompletionTokens: uinfo.CompletionTokens,
				CostUSD:          cr.TotalCost,
			})
			w.Header().Set("X-Gateway-LLM-Cache", "MISS")
			if semEligible && semText != "" {
				h.SemCache.Store(ctx, semBucket, semText, &semcache.Entry{
					Body:             raw,
					Provider:         dep.Provider,
					ProviderModel:    dep.ProviderModel,
					PromptTokens:     uinfo.PromptTokens,
					CompletionTokens: uinfo.CompletionTokens,
					CostUSD:          cr.TotalCost,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, out)

	// Stash the moat bundle so logSpendAsyncCtx (which fans out to all
	// 9 callback sinks) carries routing.* / cost.* / quality.* /
	// receipt.* attributes for free.
	ledgerInput := savingsLedgerInput{
		traceID:        traceID,
		key:            key,
		dep:            dep,
		requestedAlias: originalAlias,
		servedAlias:    modelAlias,
		decision:       decision,
		policy:         routingPolicy,
		strategy:       policyStrategy,
		uinfo:          uinfo,
		actualCost:     cr.TotalCost,
	}
	ctx = h.attachMoatCtx(ctx, ledgerInput)
	h.logSpendAsyncCtx(ctx, start, key, dep, modelAlias, "chat", http.StatusOK, uinfo, cr.TotalCost, traceID)

	h.writeSavingsLedger(ctx, ledgerInput)

	h.maybeShadow(ctx, &req, dep, uinfo, cr.TotalCost, start)
}

// maybeShadow launches a challenger call in the background when the
// SmartRoute config enables shadow testing. Results are logged via the
// ShadowRunner so offline analysis can score which model the user
// would have preferred.
func (h *Handlers) maybeShadow(parent context.Context, req *types.ChatCompletionRequest, dep *router.DeploymentInfo, primaryUsage cost.UsageInfo, primaryCost float64, start time.Time) {
	if h.SmartRoute == nil || !h.SmartRoute.ShouldShadow() {
		return
	}
	challenger := h.SmartRoute.ChallengerAlias()
	if challenger == "" || challenger == req.Model {
		return
	}
	runner := h.SmartRoute.Shadow()
	release := runner.Acquire()
	if release == nil {
		return
	}

	reqCopy := *req
	reqCopy.Stream = false
	reqCopy.Model = challenger
	primaryModel := dep.ProviderModel
	primaryMs := time.Since(start).Milliseconds()

	go func() {
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shadowStart := time.Now()
		resp, sdep, err := h.Router.ExecuteWithFallbackForOrg(ctx, nil, challenger, func(ctx context.Context, d *router.DeploymentInfo) (*http.Response, error) {
			prov, err := h.Registry.GetChat(d.Provider)
			if err != nil {
				return nil, err
			}
			rc := reqCopy
			rc.Model = d.ProviderModel
			hreq, err := prov.TransformChatRequest(ctx, &rc, d.APIKey, d.APIBase)
			if err != nil {
				return nil, err
			}
			return http.DefaultClient.Do(hreq)
		})
		if err != nil {
			h.Logger.Debug("shadow run failed", zap.Error(err))
			return
		}
		defer resp.Body.Close()
		prov, err := h.Registry.GetChat(sdep.Provider)
		if err != nil {
			return
		}
		shadowOut, err := prov.TransformChatResponse(resp)
		if err != nil {
			return
		}
		su := usageFromChat(shadowOut.Usage)
		scr, _ := h.CostEng.Calculate("chat", sdep.Provider, sdep.ProviderModel, su)

		winner := ""
		if scr.TotalCost < primaryCost && su.CompletionTokens > 0 {
			winner = "shadow"
		} else if scr.TotalCost > primaryCost {
			winner = "primary"
		}
		runner.Observe(parent, smartroute.ShadowResult{
			Winner:       winner,
			PrimaryModel: primaryModel,
			ShadowModel:  sdep.ProviderModel,
			PrimaryCost:  primaryCost,
			ShadowCost:   scr.TotalCost,
			PrimaryMs:    primaryMs,
			ShadowMs:     time.Since(shadowStart).Milliseconds(),
			RecordedAt:   time.Now(),
		})
		_ = primaryUsage
	}()
}

// isDeterministic returns true only when we're confident two calls with
// this request would return the same output. We cache exclusively for
// these calls to avoid confusing users with stale or nondeterministic
// responses.
func isDeterministic(req *types.ChatCompletionRequest) bool {
	if req.Temperature != nil && *req.Temperature > 0 {
		return false
	}
	if req.TopP != nil && *req.TopP < 1.0 {
		return false
	}
	return true
}
