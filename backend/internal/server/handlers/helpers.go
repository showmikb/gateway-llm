package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/callbacks"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/ir"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/observability"
	"github.com/gateway-llm/gateway-llm/internal/observability/otelmetrics"
	"github.com/gateway-llm/gateway-llm/internal/replay"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"go.uber.org/zap"
)

// sha256Sum256 is a tiny indirection so hashToken can stay inline above
// without the `crypto/sha256` import reaching into callers that don't
// need it. It's identical to sha256.Sum256.
func sha256Sum256(b []byte) [32]byte { return sha256.Sum256(b) }

// estimateSavings returns a best-effort $ estimate for rerouting from
// `fromAlias` to `toAlias`. Input tokens are estimated as chars/4 (the
// OpenAI rule of thumb) and output tokens are assumed equal to the
// caller's max_completion_tokens or 256 when unspecified.
//
// Missing pricing or missing deployments collapse to 0 so the header is
// not emitted — we'd rather omit a savings claim than publish a lie.
func estimateSavings(eng *cost.Engine, fromAlias, toAlias string, req *types.ChatCompletionRequest) float64 {
	if eng == nil || fromAlias == "" || toAlias == "" {
		return 0
	}
	var chars int
	for _, m := range req.Messages {
		chars += len(m.Content)
	}
	promptTokens := chars / 4
	if promptTokens < 1 {
		promptTokens = 1
	}
	outTokens := 256
	if req.MaxCompletionTokens != nil && *req.MaxCompletionTokens > 0 {
		outTokens = *req.MaxCompletionTokens
	} else if req.MaxTokens != nil && *req.MaxTokens > 0 {
		outTokens = *req.MaxTokens
	}

	fromCost := aliasExpectedCost(eng, fromAlias, promptTokens, outTokens)
	toCost := aliasExpectedCost(eng, toAlias, promptTokens, outTokens)
	if fromCost <= 0 || toCost <= 0 || toCost >= fromCost {
		return 0
	}
	return fromCost - toCost
}

// aliasExpectedCost looks up any pricing entry for the first known
// (provider, model) under `alias` and returns in+out cost. The catalog
// is keyed by provider/model, not alias, so we ask the cost engine for
// its full map and match heuristically. If the alias name itself is a
// valid pricing key (e.g. the alias equals the provider model), that
// key wins.
func aliasExpectedCost(eng *cost.Engine, alias string, promptTokens, outTokens int) float64 {
	all := eng.GetAllPricing()
	// Direct match by alias (provider/model-style).
	if p := all[alias]; p != nil {
		return float64(promptTokens)*p.InputCostPerToken + float64(outTokens)*p.OutputCostPerToken
	}
	// Fall back to any pricing row whose model suffix == alias. This
	// keeps the header truthful for the common case where alias names
	// mirror the upstream model id (e.g. "gpt-4o-mini").
	needle := alias
	if idx := strings.LastIndex(alias, "/"); idx >= 0 {
		needle = alias[idx+1:]
	}
	for k, p := range all {
		if p == nil {
			continue
		}
		if strings.HasSuffix(k, "/"+needle) {
			return float64(promptTokens)*p.InputCostPerToken + float64(outTokens)*p.OutputCostPerToken
		}
	}
	return 0
}

// enqueueRecording hands a fully-assembled Recording off to the async
// recorder. The caller must have already decided that this request is
// recordable (record flag). irResp may be nil for streamed responses.
func (h *Handlers) enqueueRecording(
	ctx context.Context,
	key *models.APIKey,
	dep *router.DeploymentInfo,
	alias string,
	endpoint string,
	status int,
	irReq *ir.ChatRequest,
	typedResp *types.ChatCompletionResponse,
	pass types.Passthrough,
	cr *cost.CostResult,
	usage cost.UsageInfo,
	start time.Time,
	traceID string,
) {
	if h.Recorder == nil {
		return
	}
	rec := &models.Recording{
		TraceID:          traceID,
		ModelAlias:       alias,
		Provider:         dep.Provider,
		ProviderModel:    dep.ProviderModel,
		Endpoint:         endpoint,
		Ingress:          "openai",
		StatusCode:       status,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.PromptTokens + usage.CompletionTokens,
		CostUSD:          cr.TotalCost,
		LatencyMS:        int(time.Since(start).Milliseconds()),
		Tags:             pass.Tags,
	}
	if key != nil && key.ID != uuid.Nil {
		id := key.ID
		rec.APIKeyID = &id
		rec.UserID = key.UserID
		rec.TeamID = key.TeamID
	}
	if oid := middleware.GetOrgID(ctx); oid != nil {
		rec.OrgID = oid
	}
	if len(pass.Metadata) > 0 {
		if meta, err := json.Marshal(pass.Metadata); err == nil {
			rec.Metadata = meta
		}
	}

	var irResp *ir.ChatResponse
	if typedResp != nil {
		irResp = chatResponseToIR(typedResp, dep)
	}
	h.Recorder.Enqueue(&replay.PendingRecording{
		Recording:  rec,
		RequestIR:  irReq,
		ResponseIR: irResp,
	})
}

func chatResponseToIR(resp *types.ChatCompletionResponse, dep *router.DeploymentInfo) *ir.ChatResponse {
	out := &ir.ChatResponse{
		ID:                resp.ID,
		Provider:          dep.Provider,
		ProviderModel:     dep.ProviderModel,
		Alias:             resp.Model,
		Created:           time.Unix(resp.Created, 0).UTC(),
		SystemFingerprint: resp.SystemFingerprint,
	}
	if resp.Usage != nil {
		out.Usage = &ir.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	for _, c := range resp.Choices {
		irc := ir.Choice{Index: c.Index}
		if c.FinishReason != nil {
			irc.FinishReason = *c.FinishReason
		}
		if c.Message != nil {
			// Reuse the ingress conversion for messages.
			m := messageFromChatMessage(*c.Message)
			irc.Message = &m
		}
		out.Choices = append(out.Choices, irc)
	}
	return out
}

// messageFromChatMessage mirrors ir.FromOpenAI but for a single message
// (so we don't materialize a whole request just to reuse that code path).
func messageFromChatMessage(m types.ChatMessage) ir.Message {
	out := ir.Message{Role: m.Role, Name: m.Name, ToolCallID: m.ToolCallID}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ir.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: ir.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	if len(m.Content) == 0 {
		return out
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		out.ContentText = s
	}
	return out
}

// canonicalPrompt returns a role-tagged flattening of the request
// messages suitable for semantic embedding. The semantic cache's
// HashEmbedder is bag-of-words so we concatenate everything with role
// prefixes to keep user vs system prompts distinguishable.
func canonicalPrompt(msgs []types.ChatMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Role)
		b.WriteByte(':')
		if len(m.Content) > 0 {
			var s string
			if err := json.Unmarshal(m.Content, &s); err == nil {
				b.WriteString(s)
			} else {
				b.Write(m.Content)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrorResp(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, types.ErrorResponse{Error: types.ErrorDetail{Message: msg, Type: "invalid_request_error"}})
}

func providerBaseURL(apiBase string) string {
	if apiBase != "" {
		return strings.TrimRight(apiBase, "/")
	}
	return "https://api.openai.com"
}

func setCostHeaders(w http.ResponseWriter, cr *cost.CostResult) {
	if cr == nil {
		return
	}
	w.Header().Set("X-Gateway-LLM-Cost", fmt.Sprintf("%.6f", cr.TotalCost))
	w.Header().Set("X-Gateway-LLM-Tokens-Input", fmt.Sprintf("%d", cr.PromptTokens))
	w.Header().Set("X-Gateway-LLM-Tokens-Output", fmt.Sprintf("%d", cr.CompletionTokens))
}

func usageFromChat(u *types.Usage) cost.UsageInfo {
	if u == nil {
		return cost.UsageInfo{}
	}
	info := cost.UsageInfo{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
	}
	if u.PromptTokensDetails != nil {
		info.CachedTokens = u.PromptTokensDetails.CachedTokens
	}
	return info
}

func usageFromEmbedding(u *types.EmbeddingUsage) cost.UsageInfo {
	if u == nil {
		return cost.UsageInfo{}
	}
	return cost.UsageInfo{PromptTokens: u.PromptTokens}
}

func usageFromResponses(u *types.ResponsesUsage) cost.UsageInfo {
	if u == nil {
		return cost.UsageInfo{}
	}
	return cost.UsageInfo{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
	}
}

func generateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// hashToken mirrors middleware/auth.hashToken for handlers that need to
// look up keys by their SHA-256 hash without importing the middleware
// package (import cycle).
func hashToken(token string) string {
	sum := sha256Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func generateSpanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// traceIDContextKey lets handlers share the trace ID they emit on the
// response header with the async spend-log writer, so feedback attached
// to that trace_id matches what we persisted.
type traceIDCtxKey struct{}

// WithTraceID returns a copy of ctx carrying traceID. Used when a handler
// wants the async spend logger to persist the same id it exposed on the
// response header.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDCtxKey{}, traceID)
}

// TraceIDFromContext returns the trace id stashed by WithTraceID, or "".
func TraceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceIDCtxKey{}).(string)
	return v
}

// SetTraceIDHeader attaches the trace id to the response so clients can
// later refer to this inference in POST /v1/feedback.
func SetTraceIDHeader(w http.ResponseWriter, traceID string) {
	if traceID == "" {
		return
	}
	w.Header().Set("X-Gateway-LLM-Trace-ID", traceID)
}

func (h *Handlers) logSpendAsync(
	start time.Time,
	key *models.APIKey,
	dep *router.DeploymentInfo,
	modelAlias, endpoint string,
	statusCode int,
	usage cost.UsageInfo,
	totalCost float64,
) {
	h.logSpendAsyncCtx(context.Background(), start, key, dep, modelAlias, endpoint, statusCode, usage, totalCost, "")
}

// logSpendAsyncCtx is the trace-aware variant used by handlers that have
// a trace id they exposed on the response header. When traceID is empty
// a fresh one is generated.
func (h *Handlers) logSpendAsyncCtx(
	parent context.Context,
	start time.Time,
	key *models.APIKey,
	dep *router.DeploymentInfo,
	modelAlias, endpoint string,
	statusCode int,
	usage cost.UsageInfo,
	totalCost float64,
	traceID string,
) {
	if h.DB == nil && h.Dispatcher == nil {
		return
	}
	if traceID == "" {
		traceID = generateTraceID()
	}
	// Pull the smart-routing moat bundle off the parent ctx now —
	// the goroutine below uses a fresh background ctx so we'd
	// otherwise drop these attributes from every dispatched event.
	moat := moatFromCtx(parent)

	// Record Prometheus samples synchronously - these are sub-microsecond
	// in-memory updates and we want them captured even if the goroutine
	// below is shed under shutdown.
	provider := ""
	if dep != nil {
		provider = dep.Provider
	}
	latencySec := time.Since(start).Seconds()
	observability.RecordRequest(
		modelAlias,
		provider,
		statusCode,
		latencySec,
		usage.PromptTokens,
		usage.CompletionTokens,
		totalCost,
	)
	if h.OTLPMetrics != nil {
		h.OTLPMetrics.RecordRequest(parent, modelAlias, provider,
			statusCode, latencySec*1000.0,
			usage.PromptTokens, usage.CompletionTokens, totalCost)
	}
	// Smart-routing moat counters: only emit when this request flowed
	// through the routing decision path (otherwise we'd inflate the
	// "passthrough" decision counter for every non-LLM hit).
	if moat != nil && moat.Routing != nil {
		sample := observability.MoatSample{
			Alias:       moat.Routing.RequestedAlias,
			ServedAlias: moat.Routing.ServedAlias,
			Strategy:    moat.Routing.Strategy,
			Retried:     moat.Routing.Retried,
			OrgID:       moat.OrgID,
			Overridden:  moat.Routing.Overridden,
		}
		if moat.Cost != nil {
			sample.BaselineUSD = moat.Cost.BaselineUSD
			sample.ActualUSD = moat.Cost.ActualUSD
			sample.SavingsUSD = moat.Cost.SavingsUSD
		}
		if moat.Quality != nil {
			sample.QualityScore = moat.Quality.Score
			sample.QualityPass = moat.Quality.Pass
		}
		observability.RecordMoat(sample)
		if h.OTLPMetrics != nil {
			h.OTLPMetrics.RecordMoat(parent, otelmetrics.MoatSample{
				Alias:        sample.Alias,
				ServedAlias:  sample.ServedAlias,
				Strategy:     sample.Strategy,
				Retried:      sample.Retried,
				OrgID:        sample.OrgID,
				BaselineUSD:  sample.BaselineUSD,
				ActualUSD:    sample.ActualUSD,
				SavingsUSD:   sample.SavingsUSD,
				QualityScore: sample.QualityScore,
				QualityPass:  sample.QualityPass,
				Overridden:   sample.Overridden,
			})
		}
	}

	go func() {
		ctx := context.Background()
		var keyID *uuid.UUID
		if key != nil && key.ID != uuid.Nil {
			keyID = &key.ID
		}
		latencyMS := int(time.Since(start).Milliseconds())
		totalTokens := usage.PromptTokens + usage.CompletionTokens

		if h.Cache != nil && keyID != nil && totalTokens > 0 {
			window := time.Minute
			if h.Cfg != nil && h.Cfg.RateLimiting.Window > 0 {
				window = h.Cfg.RateLimiting.Window
			}
			middleware.ConsumeTokens(ctx, h.Cache, keyID.String(), totalTokens, window)
		}
		var teamID *uuid.UUID
		if key != nil {
			teamID = key.TeamID
		}

		if h.DB != nil {
			log := &models.SpendLog{
				APIKeyID:         keyID,
				ModelAlias:       modelAlias,
				Provider:         dep.Provider,
				Endpoint:         endpoint,
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      totalTokens,
				CostUSD:          totalCost,
				LatencyMS:        latencyMS,
				StatusCode:       statusCode,
				TraceID:          traceID,
			}
			if err := h.DB.InsertSpendLog(ctx, log); err != nil {
				h.Logger.Warn("insert spend log", zap.Error(err))
			}
			if keyID != nil && totalCost > 0 {
				if err := h.DB.UpdateKeySpend(ctx, *keyID, totalCost); err != nil {
					h.Logger.Warn("update key spend", zap.Error(err))
				}
			}
			if keyID != nil || teamID != nil {
				if err := h.DB.UpsertDailySpend(ctx, keyID, teamID, totalTokens, totalCost); err != nil {
					h.Logger.Warn("upsert daily spend", zap.Error(err))
				}
			}
		}

		if h.Dispatcher != nil {
			var apiKeyStr, userIDStr, teamIDStr string
			if keyID != nil {
				apiKeyStr = keyID.String()
			}
			if key != nil && key.UserID != nil {
				userIDStr = key.UserID.String()
			}
			if teamID != nil {
				teamIDStr = teamID.String()
			}
			ev := callbacks.RequestEvent{
				TraceID:          traceID,
				SpanID:           generateSpanID(),
				Timestamp:        start,
				DurationMS:       int64(latencyMS),
				Method:           "POST",
				Endpoint:         endpoint,
				ModelAlias:       modelAlias,
				Provider:         dep.Provider,
				ProviderModel:    dep.ProviderModel,
				Status:           statusCode,
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      totalTokens,
				CostUSD:          totalCost,
				APIKeyID:         apiKeyStr,
				UserID:           userIDStr,
				TeamID:           teamIDStr,
			}
			if moat != nil {
				ev.OrgID = moat.OrgID
				ev.Routing = moat.Routing
				ev.Cost = moat.Cost
				ev.Quality = moat.Quality
				ev.Receipt = moat.Receipt
			}
			h.Dispatcher.Dispatch(ev)
		}
	}()
}

func truncateBytes(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	return b[:max]
}

// extractRawMessageItems returns the request's message-or-item array
// from whichever top-level field the client used. Cursor's agent puts
// the conversation under "input" instead of "messages", so we accept
// both. The result is a slice of opaque JSON objects so callers can
// inspect Responses-API item shapes (function_call, function_call_output,
// reasoning, ...) before unmarshalling into the typed ChatMessage.
func extractRawMessageItems(rawBody []byte) []map[string]json.RawMessage {
	var probe struct {
		Messages []map[string]json.RawMessage `json:"messages"`
		Input    json.RawMessage              `json:"input"`
	}
	if json.Unmarshal(rawBody, &probe) != nil {
		return nil
	}
	if len(probe.Messages) > 0 {
		return probe.Messages
	}
	if len(probe.Input) > 0 && string(probe.Input) != "null" {
		var items []map[string]json.RawMessage
		if json.Unmarshal(probe.Input, &items) == nil && len(items) > 0 {
			return items
		}
	}
	return nil
}

// normalizeChatMessages converts Responses-API "item" entries that
// don't have a Chat-Completions-compatible role into proper role-based
// messages. Cursor's agent and other Responses-API-aware clients send
// messages such as:
//
//	{"type": "function_call", "name": "...", "arguments": "...", "call_id": "..."}
//	{"type": "function_call_output", "call_id": "...", "output": "..."}
//	{"type": "reasoning", "summary": [...]}
//
// Chat Completions only accepts {"role": "system|user|assistant|tool|...",
// ...}. We rewrite using the official mapping:
//
//	function_call          -> {role:"assistant", tool_calls:[{id:call_id, type:"function",
//	                            function:{name, arguments}}], content: null}
//	function_call_output   -> {role:"tool", tool_call_id:call_id, content:output}
//	reasoning              -> dropped (assistant-introspection only)
//
// Returns (rewritten messages, count of items rewritten or dropped).
// (nil, 0) when the body is missing, unparseable, or no rewrites apply.
func normalizeChatMessages(rawBody []byte) ([]types.ChatMessage, int) {
	rawItems := extractRawMessageItems(rawBody)
	if len(rawItems) == 0 {
		return nil, 0
	}

	rewrites := 0
	out := make([]types.ChatMessage, 0, len(rawItems))
	for _, raw := range rawItems {
		var role, msgType string
		if r, ok := raw["role"]; ok {
			_ = json.Unmarshal(r, &role)
		}
		if t, ok := raw["type"]; ok {
			_ = json.Unmarshal(t, &msgType)
		}

		if role != "" {
			if msg, ok := decodeRawChatMessage(raw); ok {
				out = append(out, msg)
			}
			continue
		}

		switch msgType {
		case "function_call":
			var name, args, callID string
			if v, ok := raw["name"]; ok {
				_ = json.Unmarshal(v, &name)
			}
			if v, ok := raw["arguments"]; ok {
				if json.Unmarshal(v, &args) != nil {
					args = string(v)
				}
			}
			if v, ok := raw["call_id"]; ok {
				_ = json.Unmarshal(v, &callID)
			}
			if name == "" {
				rewrites++
				continue
			}
			tc := types.ToolCall{
				ID:   callID,
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      name,
					Arguments: args,
				},
			}
			out = append(out, types.ChatMessage{
				Role:      "assistant",
				Content:   nil,
				ToolCalls: []types.ToolCall{tc},
			})
			rewrites++

		case "function_call_output":
			var callID string
			if v, ok := raw["call_id"]; ok {
				_ = json.Unmarshal(v, &callID)
			}
			var output string
			if v, ok := raw["output"]; ok {
				if json.Unmarshal(v, &output) != nil {
					output = string(v)
				}
			}
			content, _ := json.Marshal(output)
			out = append(out, types.ChatMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    content,
			})
			rewrites++

		case "reasoning":
			rewrites++
			continue

		default:
			if msg, ok := decodeRawChatMessage(raw); ok {
				out = append(out, msg)
			}
		}
	}

	if rewrites == 0 {
		return nil, 0
	}
	return out, rewrites
}

// decodeRawChatMessage marshals the raw map back to JSON and decodes it
// into a ChatMessage struct. Returns (msg, true) on success.
func decodeRawChatMessage(raw map[string]json.RawMessage) (types.ChatMessage, bool) {
	bs, err := json.Marshal(raw)
	if err != nil {
		return types.ChatMessage{}, false
	}
	var msg types.ChatMessage
	if err := json.Unmarshal(bs, &msg); err != nil {
		return types.ChatMessage{}, false
	}
	return msg, true
}

// repairChatToolTranscript enforces the Chat Completions invariant that
// every assistant message with tool_calls is immediately followed by tool
// messages answering each tool_call_id. Responses-style histories can
// contain unresolved tool calls; those cannot be represented in Chat
// Completions, so we drop only the incomplete tool-call fragment.
func repairChatToolTranscript(messages []types.ChatMessage) ([]types.ChatMessage, int) {
	if len(messages) == 0 {
		return nil, 0
	}

	rewrites := 0
	repaired := make([]types.ChatMessage, 0, len(messages))
	for i := 0; i < len(messages); {
		msg := messages[i]

		if msg.Role == "tool" {
			rewrites++
			i++
			continue
		}

		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			repaired = append(repaired, msg)
			i++
			continue
		}

		required := make(map[string]struct{}, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" {
				required[tc.ID] = struct{}{}
			}
		}
		if len(required) == 0 {
			rewrites++
			i++
			continue
		}

		j := i + 1
		tools := make([]types.ChatMessage, 0, len(required))
		satisfied := make(map[string]struct{}, len(required))
		for j < len(messages) && messages[j].Role == "tool" {
			toolMsg := messages[j]
			if _, ok := required[toolMsg.ToolCallID]; ok && toolMsg.ToolCallID != "" {
				tools = append(tools, toolMsg)
				satisfied[toolMsg.ToolCallID] = struct{}{}
			} else {
				rewrites++
			}
			j++
		}

		if len(satisfied) == len(required) {
			repaired = append(repaired, msg)
			repaired = append(repaired, tools...)
		} else {
			rewrites += 1 + len(tools)
		}
		i = j
	}

	if rewrites == 0 {
		return nil, 0
	}
	return repaired, rewrites
}

// normalizeChatTools repairs tool entries that arrived in the
// Responses-API "flat" function tool shape so they round-trip correctly
// to OpenAI Chat Completions.
//
// Cursor's agent (and other Responses-API-aware clients) sends:
//
//	{"type": "function", "name": "edit_file", "description": "...", "parameters": {...}}
//
// Chat Completions requires the nested form:
//
//	{"type": "function", "function": {"name": "edit_file", "description": "...", "parameters": {...}}}
//
// We detect the flat shape by walking the *raw* request body (the
// typed []types.Tool decode already stripped the top-level fields and
// left function.name == "" as the symptom). Returns the fully-rewritten
// tools slice and the number of entries lifted; on any failure or no
// rewrite needed, returns (nil, 0) so the caller leaves the original
// req.Tools alone.
func normalizeChatTools(rawBody []byte) ([]types.Tool, int) {
	var probe struct {
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(rawBody, &probe); err != nil || len(probe.Tools) == 0 {
		return nil, 0
	}

	rewrites := 0
	out := make([]types.Tool, 0, len(probe.Tools))
	for _, raw := range probe.Tools {
		tool := types.Tool{}
		if t, ok := raw["type"]; ok {
			_ = json.Unmarshal(t, &tool.Type)
		}
		if tool.Type == "" {
			tool.Type = "function"
		}

		// Prefer the nested function object when it is present and has
		// a non-empty name; this keeps already-correct payloads
		// untouched.
		nested := false
		if fn, ok := raw["function"]; ok && len(fn) > 0 && string(fn) != "null" {
			var fnObj types.ToolFunction
			if err := json.Unmarshal(fn, &fnObj); err == nil && fnObj.Name != "" {
				tool.Function = fnObj
				nested = true
			}
		}

		if !nested {
			if n, ok := raw["name"]; ok {
				_ = json.Unmarshal(n, &tool.Function.Name)
			}
			if d, ok := raw["description"]; ok {
				_ = json.Unmarshal(d, &tool.Function.Description)
			}
			if p, ok := raw["parameters"]; ok {
				tool.Function.Parameters = p
			}
			if tool.Function.Name != "" {
				rewrites++
			}
		}

		out = append(out, tool)
	}

	if rewrites == 0 {
		return nil, 0
	}
	return out, rewrites
}

// normalizeChatMessageContent rewrites Responses-API content-part type
// names into their Chat Completions equivalents so clients that send the
// newer "input_*" schema (notably Cursor's agent mode) don't get a 400
// from upstream OpenAI. Returns the number of parts rewritten.
//
//   input_text   -> text
//   input_image  -> image_url  (image_url string is wrapped into {"url": ...})
//
// Other types ("input_audio", "refusal", "audio", "file", "text",
// "image_url") are passed through unchanged. String content (the common
// case) is also untouched.
func normalizeChatMessageContent(msgs []types.ChatMessage) int {
	rewrites := 0
	for i, m := range msgs {
		if len(m.Content) == 0 {
			continue
		}
		// Only array-of-parts content needs rewriting; bare string
		// content is already valid for Chat Completions.
		trimmed := strings.TrimSpace(string(m.Content))
		if !strings.HasPrefix(trimmed, "[") {
			continue
		}
		var parts []map[string]interface{}
		if err := json.Unmarshal(m.Content, &parts); err != nil {
			continue
		}
		changed := false
		for j, p := range parts {
			t, _ := p["type"].(string)
			switch t {
			case "input_text":
				parts[j]["type"] = "text"
				changed = true
				rewrites++
			case "output_text":
				parts[j]["type"] = "text"
				changed = true
				rewrites++
			case "input_image":
				parts[j]["type"] = "image_url"
				if iu, ok := parts[j]["image_url"].(string); ok {
					parts[j]["image_url"] = map[string]interface{}{"url": iu}
				}
				changed = true
				rewrites++
			}
		}
		if changed {
			if newContent, err := json.Marshal(parts); err == nil {
				msgs[i].Content = newContent
			}
		}
	}
	return rewrites
}

// extractMessagesFromAlternateFormats attempts to recover a messages
// array from request bodies that use non-standard field names. Handles:
//   - "input" (string)  → single user message  (Responses API style)
//   - "input" (array of objects with role/content) → direct messages
//   - "prompt" (string) → single user message  (legacy completions)
func extractMessagesFromAlternateFormats(rawBody []byte) []types.ChatMessage {
	var probe struct {
		Input  json.RawMessage `json:"input"`
		Prompt json.RawMessage `json:"prompt"`
	}
	if json.Unmarshal(rawBody, &probe) != nil {
		return nil
	}

	if len(probe.Input) > 0 && string(probe.Input) != "null" {
		var s string
		if json.Unmarshal(probe.Input, &s) == nil && s != "" {
			content, _ := json.Marshal(s)
			return []types.ChatMessage{{Role: "user", Content: content}}
		}
		var msgs []types.ChatMessage
		if json.Unmarshal(probe.Input, &msgs) == nil && len(msgs) > 0 {
			return msgs
		}
	}

	if len(probe.Prompt) > 0 && string(probe.Prompt) != "null" {
		var s string
		if json.Unmarshal(probe.Prompt, &s) == nil && s != "" {
			content, _ := json.Marshal(s)
			return []types.ChatMessage{{Role: "user", Content: content}}
		}
	}

	return nil
}

func forwardUpstreamJSONError(w http.ResponseWriter, resp *http.Response) bool {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeErrorResp(w, resp.StatusCode, err.Error())
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
	return true
}

func flushWriter(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// --- Callback management handlers ---

func (h *Handlers) ListCallbacks(w http.ResponseWriter, r *http.Request) {
	if h.Dispatcher == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []interface{}{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": h.Dispatcher.ListCallbacks()})
}

func (h *Handlers) TestCallbacks(w http.ResponseWriter, r *http.Request) {
	if h.Dispatcher == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"results": map[string]string{}})
		return
	}
	results := h.Dispatcher.TestAll(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}
