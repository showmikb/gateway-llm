// Package gemini is the wire-format adapter for Google's Generative
// Language / Gemini API. It parses the google-genai SDK's request shape
// from /v1beta/models/{model}:generateContent (or :streamGenerateContent)
// and emits Gemini's response shape so SDKs don't notice they're going
// through the gateway.
//
// Non-streaming: fully supported. Streaming: TranslateStream below
// consumes OpenAI SSE and emits Gemini's NDJSON-style stream (one JSON
// object per line prefixed with "data: " when SSE, or raw JSON for
// alt=json).
//
// Reference:
//   - https://ai.google.dev/api/rest/v1beta/models/generateContent
package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

// Name is the ingress tag stored on recordings.
const Name = "gemini"

// GenerateContentRequest is the subset of Gemini's request we parse.
type GenerateContentRequest struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *Content           `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
	Tools             []Tool             `json:"tools,omitempty"`
	ToolConfig        json.RawMessage    `json:"toolConfig,omitempty"`
	SafetySettings    []json.RawMessage  `json:"safetySettings,omitempty"`
}

type Content struct {
	Role  string `json:"role,omitempty"` // "user" | "model"
	Parts []Part `json:"parts"`
}

type Part struct {
	Text             string           `json:"text,omitempty"`
	InlineData       *InlineData      `json:"inlineData,omitempty"`
	FunctionCall     *FunctionCall    `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResp    `json:"functionResponse,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type FunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type FunctionResp struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type GenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	TopK            *int     `json:"topK,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
	CandidateCount  *int     `json:"candidateCount,omitempty"`
	Seed            *int     `json:"seed,omitempty"`
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
}

type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type FunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// GenerateContentResponse is the response shape we emit back.
type GenerateContentResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
}

type Candidate struct {
	Content       Content `json:"content"`
	FinishReason  string  `json:"finishReason,omitempty"`
	Index         int     `json:"index"`
	SafetyRatings []any   `json:"safetyRatings,omitempty"`
}

type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Parse converts a Gemini generateContent request into the canonical
// OpenAI-shaped request. The model path parameter supplies the model.
func Parse(body []byte, model string) (*types.ChatCompletionRequest, bool, error) {
	var r GenerateContentRequest
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, false, fmt.Errorf("gemini parse: %w", err)
	}
	if model == "" {
		return nil, false, fmt.Errorf("gemini parse: missing model in path")
	}

	out := &types.ChatCompletionRequest{Model: model}

	if r.GenerationConfig != nil {
		out.Temperature = r.GenerationConfig.Temperature
		out.TopP = r.GenerationConfig.TopP
		out.MaxTokens = r.GenerationConfig.MaxOutputTokens
		out.Seed = r.GenerationConfig.Seed
		if len(r.GenerationConfig.StopSequences) > 0 {
			stop, _ := json.Marshal(r.GenerationConfig.StopSequences)
			out.Stop = stop
		}
		if r.GenerationConfig.ResponseMimeType == "application/json" {
			if len(r.GenerationConfig.ResponseSchema) > 0 {
				rf, _ := json.Marshal(map[string]any{
					"type": "json_schema",
					"json_schema": map[string]any{
						"name":   "response",
						"schema": r.GenerationConfig.ResponseSchema,
					},
				})
				out.ResponseFormat = rf
			} else {
				out.ResponseFormat = json.RawMessage(`{"type":"json_object"}`)
			}
		}
	}

	if r.SystemInstruction != nil {
		sys := joinParts(r.SystemInstruction.Parts)
		if sys != "" {
			payload, _ := json.Marshal(sys)
			out.Messages = append(out.Messages, types.ChatMessage{Role: "system", Content: payload})
		}
	}

	for _, c := range r.Contents {
		msgs, err := contentToMessages(c)
		if err != nil {
			return nil, false, err
		}
		out.Messages = append(out.Messages, msgs...)
	}

	for _, t := range r.Tools {
		for _, fd := range t.FunctionDeclarations {
			out.Tools = append(out.Tools, types.Tool{
				Type: "function",
				Function: types.ToolFunction{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  fd.Parameters,
				},
			})
		}
	}

	return out, false, nil
}

// Format renders an OpenAI-shaped response as Gemini's
// GenerateContentResponse shape.
func Format(resp *types.ChatCompletionResponse) ([]byte, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("gemini format: empty response")
	}
	out := GenerateContentResponse{ModelVersion: resp.Model}
	for _, ch := range resp.Choices {
		cand := Candidate{Index: ch.Index}
		if ch.Message != nil {
			cand.Content = Content{Role: "model", Parts: messageToParts(ch.Message)}
		}
		if ch.FinishReason != nil {
			cand.FinishReason = mapFinishReason(*ch.FinishReason)
		}
		out.Candidates = append(out.Candidates, cand)
	}
	if resp.Usage != nil {
		out.UsageMetadata = &UsageMetadata{
			PromptTokenCount:     resp.Usage.PromptTokens,
			CandidatesTokenCount: resp.Usage.CompletionTokens,
			TotalTokenCount:      resp.Usage.TotalTokens,
		}
	}
	return json.Marshal(out)
}

// FormatError is Gemini's standard error envelope.
func FormatError(status int, msg string) []byte {
	b, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": msg,
			"status":  statusText(status),
		},
	})
	return b
}

func statusText(code int) string {
	switch {
	case code == 400:
		return "INVALID_ARGUMENT"
	case code == 401:
		return "UNAUTHENTICATED"
	case code == 403:
		return "PERMISSION_DENIED"
	case code == 404:
		return "NOT_FOUND"
	case code == 429:
		return "RESOURCE_EXHAUSTED"
	case code >= 500:
		return "INTERNAL"
	default:
		return "UNKNOWN"
	}
}

func mapFinishReason(r string) string {
	switch r {
	case "stop":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	case "tool_calls":
		return "STOP"
	default:
		return "FINISH_REASON_UNSPECIFIED"
	}
}

func contentToMessages(c Content) ([]types.ChatMessage, error) {
	role := "user"
	switch c.Role {
	case "model":
		role = "assistant"
	case "function":
		role = "tool"
	case "user", "":
		role = "user"
	}

	var parts []map[string]any
	var toolCalls []types.ToolCall
	var toolMsgs []types.ChatMessage
	for _, p := range c.Parts {
		switch {
		case p.Text != "":
			parts = append(parts, map[string]any{"type": "text", "text": p.Text})
		case p.InlineData != nil:
			url := "data:" + p.InlineData.MimeType + ";base64," + p.InlineData.Data
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
		case p.FunctionCall != nil:
			toolCalls = append(toolCalls, types.ToolCall{
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      p.FunctionCall.Name,
					Arguments: string(p.FunctionCall.Args),
				},
			})
		case p.FunctionResponse != nil:
			payload, _ := json.Marshal(string(p.FunctionResponse.Response))
			toolMsgs = append(toolMsgs, types.ChatMessage{Role: "tool", Content: payload, Name: p.FunctionResponse.Name})
		}
	}

	var out []types.ChatMessage
	if len(parts) > 0 || len(toolCalls) > 0 {
		msg := types.ChatMessage{Role: role}
		if len(parts) == 1 {
			if t, _ := parts[0]["text"].(string); t != "" {
				msg.Content, _ = json.Marshal(t)
			}
		}
		if len(msg.Content) == 0 && len(parts) > 0 {
			msg.Content, _ = json.Marshal(parts)
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		out = append(out, msg)
	}
	out = append(out, toolMsgs...)
	if len(out) == 0 {
		out = append(out, types.ChatMessage{Role: role, Content: json.RawMessage(`""`)})
	}
	return out, nil
}

func messageToParts(m *types.ChatMessage) []Part {
	var parts []Part
	txt := extractText(m.Content)
	if txt != "" {
		parts = append(parts, Part{Text: txt})
	}
	for _, tc := range m.ToolCalls {
		parts = append(parts, Part{FunctionCall: &FunctionCall{Name: tc.Function.Name, Args: json.RawMessage(tc.Function.Arguments)}})
	}
	if len(parts) == 0 {
		parts = []Part{{Text: ""}}
	}
	return parts
}

func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if t, _ := p["text"].(string); t != "" {
				b.WriteString(t)
			}
		}
		return b.String()
	}
	return ""
}

func joinParts(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Text != "" {
			b.WriteString(p.Text)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
