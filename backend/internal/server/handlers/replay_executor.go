package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gateway-llm/gateway-llm/internal/ir"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

// ChatExecutorForReplay returns a replay.ChatExecutor bound to this
// Handlers instance. Replay runs call it to re-play a stored IR request
// against any alias, using the same router + providers as live traffic.
//
// The function does NOT record its replays (that would cause a storm of
// replays-of-replays) and forces non-streaming so the whole response can
// be materialized for scoring.
func (h *Handlers) ChatExecutorForReplay() func(ctx context.Context, alias string, req *ir.ChatRequest) (*ir.ChatResponse, error) {
	return func(ctx context.Context, alias string, req *ir.ChatRequest) (*ir.ChatResponse, error) {
		req.Stream = false
		req.Alias = alias
		typedReq := ir.ToOpenAI(req)
		resp, dep, err := h.Router.ExecuteWithFallbackForOrg(ctx, nil, alias, func(ctx context.Context, dep *router.DeploymentInfo) (*http.Response, error) {
			prov, err := h.Registry.GetChat(dep.Provider)
			if err != nil {
				return nil, err
			}
			reqCopy := *typedReq
			reqCopy.Model = dep.ProviderModel
			hreq, err := prov.TransformChatRequest(ctx, &reqCopy, dep.APIKey, dep.APIBase)
			if err != nil {
				return nil, err
			}
			return http.DefaultClient.Do(hreq)
		})
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("replay upstream %d: %s", resp.StatusCode, string(body))
		}
		prov, err := h.Registry.GetChat(dep.Provider)
		if err != nil {
			return nil, err
		}
		out, err := prov.TransformChatResponse(resp)
		if err != nil {
			return nil, err
		}
		return chatResponseToIR(out, dep), nil
	}
}

// JudgeExecutor returns a function suitable for wiring into LLMJudge.Invoke:
// it sends a single-turn chat request through the gateway and returns the
// first choice's text.
func (h *Handlers) JudgeExecutor() func(ctx context.Context, model, prompt string) (string, error) {
	return func(ctx context.Context, model, prompt string) (string, error) {
		req := &types.ChatCompletionRequest{
			Model:    model,
			Messages: []types.ChatMessage{{Role: "user", Content: quote(prompt)}},
		}
		exec := h.ChatExecutorForReplay()
		irReq := ir.FromOpenAI(req)
		resp, err := exec(ctx, model, irReq)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
			return "", fmt.Errorf("judge returned no content")
		}
		return resp.Choices[0].Message.ContentText, nil
	}
}

func quote(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
