package ir

import (
	"encoding/json"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

// FromOpenAI converts a types.ChatCompletionRequest (OpenAI wire format) into
// our canonical IR. It is lossless for everything the IR tracks explicitly
// and carries the rest through via Extra if callers choose to populate it.
//
// The OpenAI format is considered the "reference" ingress; all other
// ingresses translate via this function indirectly.
func FromOpenAI(req *types.ChatCompletionRequest) *ChatRequest {
	if req == nil {
		return nil
	}
	ir := &ChatRequest{
		Alias:               req.Model,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		N:                   req.N,
		Stream:              req.Stream,
		Stop:                req.Stop,
		MaxTokens:           req.MaxTokens,
		MaxCompletionTokens: req.MaxCompletionTokens,
		PresencePenalty:     req.PresencePenalty,
		FrequencyPenalty:    req.FrequencyPenalty,
		Seed:                req.Seed,
		User:                req.User,
		ResponseFormat:      req.ResponseFormat,
		ToolChoice:          req.ToolChoice,
		Tags:                req.Tags,
		Metadata:            req.Metadata,
		TeamID:              req.TeamID,
		TraceID:             req.TraceID,
		GenerationName:      req.GenerationName,
		Caching:             req.Caching,
		TTL:                 req.TTL,
		NoLog:               req.NoLog,
		MockResponse:        req.MockResponse,
		RouterHint:          req.RouterHint,
		QualityFloor:        req.QualityFloor,
		Ingress:             "openai",
	}
	if req.StreamOptions != nil {
		ir.StreamOptions = &StreamOptions{IncludeUsage: req.StreamOptions.IncludeUsage}
	}
	for _, t := range req.Tools {
		ir.Tools = append(ir.Tools, Tool{
			Type: t.Type,
			Function: ToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}
	for _, m := range req.Messages {
		ir.Messages = append(ir.Messages, messageFromOpenAI(m))
	}
	return ir
}

// messageFromOpenAI normalizes the content union: a JSON string becomes
// ContentText; a JSON array becomes ContentParts. Everything else is kept
// in ContentParts[0].Extra as opaque JSON.
func messageFromOpenAI(m types.ChatMessage) Message {
	out := Message{
		Role:       m.Role,
		Name:       m.Name,
		ToolCallID: m.ToolCallID,
	}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return out
	}
	// Try string first; most messages are simple text.
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		out.ContentText = s
		return out
	}
	// Then try array-of-parts.
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		for _, p := range parts {
			out.ContentParts = append(out.ContentParts, contentPartFromJSON(p))
		}
		return out
	}
	// Unknown shape: preserve as an opaque part so replay is lossless.
	out.ContentParts = append(out.ContentParts, ContentPart{Type: "unknown", Extra: m.Content})
	return out
}

func contentPartFromJSON(p map[string]json.RawMessage) ContentPart {
	var cp ContentPart
	if raw, ok := p["type"]; ok {
		_ = json.Unmarshal(raw, &cp.Type)
	}
	switch cp.Type {
	case "text":
		if raw, ok := p["text"]; ok {
			_ = json.Unmarshal(raw, &cp.Text)
		}
	case "image_url":
		if raw, ok := p["image_url"]; ok {
			cp.ImageURL = &ImageURLPart{}
			_ = json.Unmarshal(raw, cp.ImageURL)
		}
	case "input_audio":
		if raw, ok := p["input_audio"]; ok {
			cp.Audio = &AudioPart{}
			_ = json.Unmarshal(raw, cp.Audio)
		}
	default:
		// Collapse all fields back to Extra for lossless passthrough.
		extra, _ := json.Marshal(p)
		cp.Extra = extra
	}
	return cp
}

// ToOpenAI converts the IR back to an OpenAI wire request. Inverse of
// FromOpenAI; round-trip safe for fields the IR tracks.
func ToOpenAI(req *ChatRequest) *types.ChatCompletionRequest {
	if req == nil {
		return nil
	}
	out := &types.ChatCompletionRequest{
		Model:               req.Alias,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		N:                   req.N,
		Stream:              req.Stream,
		Stop:                req.Stop,
		MaxTokens:           req.MaxTokens,
		MaxCompletionTokens: req.MaxCompletionTokens,
		PresencePenalty:     req.PresencePenalty,
		FrequencyPenalty:    req.FrequencyPenalty,
		Seed:                req.Seed,
		User:                req.User,
		ResponseFormat:      req.ResponseFormat,
		ToolChoice:          req.ToolChoice,
	}
	if req.StreamOptions != nil {
		out.StreamOptions = &types.StreamOptions{IncludeUsage: req.StreamOptions.IncludeUsage}
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, types.Tool{
			Type: t.Type,
			Function: types.ToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, messageToOpenAI(m))
	}
	return out
}

func messageToOpenAI(m Message) types.ChatMessage {
	out := types.ChatMessage{Role: m.Role, Name: m.Name, ToolCallID: m.ToolCallID}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, types.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: types.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	if m.ContentText != "" {
		raw, _ := json.Marshal(m.ContentText)
		out.Content = raw
	} else if len(m.ContentParts) > 0 {
		parts := make([]map[string]any, 0, len(m.ContentParts))
		for _, p := range m.ContentParts {
			obj := map[string]any{"type": p.Type}
			switch p.Type {
			case "text":
				obj["text"] = p.Text
			case "image_url":
				if p.ImageURL != nil {
					obj["image_url"] = p.ImageURL
				}
			case "input_audio":
				if p.Audio != nil {
					obj["input_audio"] = p.Audio
				}
			default:
				if len(p.Extra) > 0 {
					var fallback map[string]any
					if err := json.Unmarshal(p.Extra, &fallback); err == nil {
						obj = fallback
					}
				}
			}
			parts = append(parts, obj)
		}
		raw, _ := json.Marshal(parts)
		out.Content = raw
	} else {
		out.Content = []byte("null")
	}
	return out
}
