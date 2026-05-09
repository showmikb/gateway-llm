package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

const (
	defaultAnthropicBase = "https://api.anthropic.com"
	anthropicAPIVersion  = "2023-06-01"
	defaultMaxTokens     = 4096
)

// AnthropicProvider implements ChatProvider and CompletionsProvider for Anthropic Messages API.
type AnthropicProvider struct {
	httpClient *http.Client
}

// New returns an Anthropic provider with a default HTTP client.
func New() *AnthropicProvider {
	return &AnthropicProvider{
		httpClient: &http.Client{Timeout: 0},
	}
}

// Name returns the provider identifier.
func (p *AnthropicProvider) Name() string { return "anthropic" }

// Capabilities reports supported features.
func (p *AnthropicProvider) Capabilities() []providers.Capability {
	return []providers.Capability{providers.CapChat, providers.CapCompletions}
}

// TransformChatRequest builds a POST request to Anthropic Messages API from an OpenAI chat completion request.
func (p *AnthropicProvider) TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key")
	}

	body, err := buildAnthropicRequestBody(req)
	if err != nil {
		return nil, err
	}

	endpoint, err := messagesURL(apiBase)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	return httpReq, nil
}

// TransformChatResponse maps an Anthropic Messages response to OpenAI ChatCompletionResponse.
func (p *AnthropicProvider) TransformChatResponse(resp *http.Response) (*types.ChatCompletionResponse, error) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic: API error %d: %s", resp.StatusCode, truncateBody(body))
	}

	var ar AnthropicResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}

	msg, finish := anthropicBlocksToOpenAIMessage(ar.Content, ar.StopReason)

	out := &types.ChatCompletionResponse{
		ID:      ar.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ar.Model,
		Choices: []types.Choice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: ptrString(finish),
			},
		},
		Usage: &types.Usage{
			PromptTokens:     ar.Usage.InputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}

	return out, nil
}

// StreamChatResponse parses Anthropic SSE and yields OpenAI-shaped chat.completion.chunk JSON payloads.
func (p *AnthropicProvider) StreamChatResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	return parseAnthropicSSE(ctx, resp)
}

// TransformCompletionsRequest maps a legacy completion request to Anthropic via chat conversion.
func (p *AnthropicProvider) TransformCompletionsRequest(ctx context.Context, req *types.CompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	promptText, err := parseCompletionPrompt(req.Prompt)
	if err != nil {
		return nil, err
	}

	content, err := json.Marshal(promptText)
	if err != nil {
		return nil, err
	}

	chat := &types.ChatCompletionRequest{
		Model:            req.Model,
		Messages:         []types.ChatMessage{{Role: "user", Content: content}},
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		N:                req.N,
		Stream:           req.Stream,
		Stop:             req.Stop,
		MaxTokens:        req.MaxTokens,
		PresencePenalty:  req.PresencePenalty,
		FrequencyPenalty: req.FrequencyPenalty,
		User:             req.User,
	}

	return p.TransformChatRequest(ctx, chat, apiKey, apiBase)
}

// TransformCompletionsResponse converts a Messages API response into OpenAI completion format.
func (p *AnthropicProvider) TransformCompletionsResponse(resp *http.Response) (*types.CompletionResponse, error) {
	chat, err := p.TransformChatResponse(resp)
	if err != nil {
		return nil, err
	}
	if len(chat.Choices) == 0 {
		return &types.CompletionResponse{
			ID:      chat.ID,
			Object:  "text_completion",
			Created: chat.Created,
			Model:   chat.Model,
			Choices: []types.CompletionChoice{},
			Usage:   chat.Usage,
		}, nil
	}

	text := messageTextFromOpenAI(chat.Choices[0].Message)
	finish := ""
	if chat.Choices[0].FinishReason != nil {
		finish = *chat.Choices[0].FinishReason
	}

	out := &types.CompletionResponse{
		ID:      chat.ID,
		Object:  "text_completion",
		Created: chat.Created,
		Model:   chat.Model,
		Choices: []types.CompletionChoice{
			{
				Text:         text,
				Index:        0,
				FinishReason: ptrStringOrNil(finish),
			},
		},
		Usage: chat.Usage,
	}
	return out, nil
}

func buildAnthropicRequestBody(req *types.ChatCompletionRequest) ([]byte, error) {
	system, msgs, err := openAIToAnthropicMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	maxTok := defaultMaxTokens
	if req.MaxCompletionTokens != nil {
		maxTok = *req.MaxCompletionTokens
	} else if req.MaxTokens != nil {
		maxTok = *req.MaxTokens
	}

	ar := AnthropicRequest{
		Model:       req.Model,
		Messages:    msgs,
		System:      system,
		MaxTokens:   maxTok,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if len(req.Tools) > 0 {
		ar.Tools = make([]AnthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			if t.Type != "" && t.Type != "function" {
				continue
			}
			at := AnthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			}
			if len(at.InputSchema) == 0 {
				at.InputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			ar.Tools = append(ar.Tools, at)
		}
	}

	if stops, err := parseStopSequences(req.Stop); err == nil && len(stops) > 0 {
		ar.StopSequences = stops
	}

	return json.Marshal(ar)
}

func openAIToAnthropicMessages(messages []types.ChatMessage) (system string, out []AnthropicMessage, err error) {
	var sysParts []string
	rest := make([]types.ChatMessage, 0, len(messages))

	for _, m := range messages {
		switch m.Role {
		case "system", "developer":
			s, e := rawMessageToPlainText(m.Content)
			if e != nil {
				return "", nil, e
			}
			if strings.TrimSpace(s) != "" {
				sysParts = append(sysParts, s)
			}
		default:
			rest = append(rest, m)
		}
	}

	system = strings.Join(sysParts, "\n\n")

	merged, err := mergeConversationTurns(rest)
	if err != nil {
		return "", nil, err
	}

	for _, m := range merged {
		am, err := convertOpenAIMessage(m)
		if err != nil {
			return "", nil, err
		}
		out = append(out, am)
	}

	return system, out, nil
}

func mergeConversationTurns(messages []types.ChatMessage) ([]types.ChatMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	out := make([]types.ChatMessage, 0, len(messages))
	var cur *types.ChatMessage

	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}

	for _, m := range messages {
		switch m.Role {
		case "tool":
			flush()
			out = append(out, m)
		case "user", "assistant":
			if cur != nil && cur.Role == m.Role && canMergeAdjacentMessages(*cur, m) {
				combined, err := mergeSameRoleMessages(*cur, m)
				if err != nil {
					return nil, err
				}
				cur = &combined
				continue
			}
			flush()
			mm := m
			cur = &mm
		default:
			flush()
			out = append(out, m)
		}
	}
	flush()
	return out, nil
}

func canMergeAdjacentMessages(a, b types.ChatMessage) bool {
	if len(a.ToolCalls) > 0 || len(b.ToolCalls) > 0 {
		return false
	}
	return true
}

func mergeSameRoleMessages(a, b types.ChatMessage) (types.ChatMessage, error) {
	ta, err := messageContentToParts(a.Content)
	if err != nil {
		return types.ChatMessage{}, err
	}
	tb, err := messageContentToParts(b.Content)
	if err != nil {
		return types.ChatMessage{}, err
	}
	combined := append(append([]string{}, ta...), tb...)
	raw, err := json.Marshal(strings.Join(combined, "\n\n"))
	if err != nil {
		return types.ChatMessage{}, err
	}
	return types.ChatMessage{Role: a.Role, Content: raw}, nil
}

func messageContentToParts(content json.RawMessage) ([]string, error) {
	if len(content) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return []string{s}, nil
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(content, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if typ, _ := p["type"].(string); typ == "text" {
				if t, ok := p["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		if len(texts) > 0 {
			return texts, nil
		}
	}
	// Fallback: stringify
	return []string{string(content)}, nil
}

func convertOpenAIMessage(m types.ChatMessage) (AnthropicMessage, error) {
	switch m.Role {
	case "user":
		c, err := openAIUserContent(m.Content)
		if err != nil {
			return AnthropicMessage{}, err
		}
		return AnthropicMessage{Role: "user", Content: c}, nil
	case "assistant":
		blocks, err := openAIAssistantBlocks(m)
		if err != nil {
			return AnthropicMessage{}, err
		}
		if len(blocks) == 1 {
			if tb, ok := blocks[0].(string); ok {
				return AnthropicMessage{Role: "assistant", Content: tb}, nil
			}
		}
		bl, err := normalizeContentBlocks(blocks)
		if err != nil {
			return AnthropicMessage{}, err
		}
		return AnthropicMessage{Role: "assistant", Content: bl}, nil
	case "tool":
		toolUseID := m.ToolCallID
		if toolUseID == "" {
			return AnthropicMessage{}, fmt.Errorf("anthropic: tool message missing tool_call_id")
		}
		body, err := rawMessageToPlainText(m.Content)
		if err != nil {
			return AnthropicMessage{}, err
		}
		tb := map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": toolUseID,
			"content":     body,
		}
		return AnthropicMessage{Role: "user", Content: []interface{}{tb}}, nil
	default:
		return AnthropicMessage{}, fmt.Errorf("anthropic: unsupported message role %q", m.Role)
	}
}

func normalizeContentBlocks(blocks []interface{}) ([]ContentBlock, error) {
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch v := b.(type) {
		case string:
			out = append(out, ContentBlock{Type: "text", Text: v})
		case ContentBlock:
			out = append(out, v)
		default:
			return nil, fmt.Errorf("anthropic: unexpected assistant content block type %T", b)
		}
	}
	return out, nil
}

func openAIUserContent(content json.RawMessage) (interface{}, error) {
	if len(content) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s, nil
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(content, &parts); err != nil {
		return nil, fmt.Errorf("anthropic: user content: %w", err)
	}
	blocks := make([]ContentBlock, 0, len(parts))
	for _, p := range parts {
		typ, _ := p["type"].(string)
		switch typ {
		case "text":
			if t, ok := p["text"].(string); ok {
				blocks = append(blocks, ContentBlock{Type: "text", Text: t})
			}
		case "image_url":
			// Anthropic image blocks need source; skip unsupported for now
			continue
		default:
			continue
		}
	}
	if len(blocks) == 0 {
		return "", nil
	}
	if len(blocks) == 1 {
		return blocks[0].Text, nil
	}
	return blocks, nil
}

func openAIAssistantBlocks(m types.ChatMessage) ([]interface{}, error) {
	var blocks []interface{}

	text, err := rawMessageToPlainText(m.Content)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, text)
	}

	for _, tc := range m.ToolCalls {
		if tc.Type != "" && tc.Type != "function" {
			continue
		}
		var input interface{}
		if strings.TrimSpace(tc.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				input = map[string]interface{}{}
			}
		} else {
			input = map[string]interface{}{}
		}
		blocks = append(blocks, ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	if len(blocks) == 0 {
		blocks = append(blocks, "")
	}

	return blocks, nil
}

func rawMessageToPlainText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("anthropic: content: %w", err)
	}
	var b strings.Builder
	for _, p := range parts {
		if typ, _ := p["type"].(string); typ == "text" {
			if t, ok := p["text"].(string); ok {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(t)
			}
		}
	}
	return b.String(), nil
}

func anthropicBlocksToOpenAIMessage(blocks []ContentBlock, stopReason string) (*types.ChatMessage, string) {
	var textParts []string
	var toolCalls []types.ToolCall

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				textParts = append(textParts, b.Text)
			}
		case "tool_use":
			args := ""
			if b.Input != nil {
				if raw, err := json.Marshal(b.Input); err == nil {
					args = string(raw)
				}
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:   b.ID,
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      b.Name,
					Arguments: args,
				},
			})
		}
	}

	finish := mapStopReason(stopReason, len(toolCalls) > 0)

	msg := &types.ChatMessage{Role: "assistant"}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	if len(textParts) > 0 {
		raw, _ := json.Marshal(strings.Join(textParts, ""))
		msg.Content = raw
	} else if len(toolCalls) > 0 {
		msg.Content = json.RawMessage(`null`)
	} else {
		msg.Content = json.RawMessage(`""`)
	}

	return msg, finish
}

func mapStopReason(stopReason string, hasTools bool) string {
	switch stopReason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		if hasTools {
			return "tool_calls"
		}
		return "stop"
	}
}

func parseStopSequences(stop json.RawMessage) ([]string, error) {
	if len(stop) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(stop, &s); err == nil {
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	}
	var arr []string
	if err := json.Unmarshal(stop, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func messagesURL(apiBase string) (string, error) {
	base := strings.TrimSpace(apiBase)
	if base == "" {
		base = defaultAnthropicBase
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("anthropic: invalid api base: %w", err)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/messages"
	return u.String(), nil
}

func parseCompletionPrompt(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return strings.Join(arr, ""), nil
	}
	var anyArr []interface{}
	if err := json.Unmarshal(raw, &anyArr); err == nil {
		var b strings.Builder
		for _, v := range anyArr {
			switch t := v.(type) {
			case string:
				b.WriteString(t)
			case float64:
				b.WriteString(fmt.Sprintf("%v", t))
			default:
				b.WriteString(fmt.Sprint(t))
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("anthropic: unsupported completion prompt format")
}

func messageTextFromOpenAI(msg *types.ChatMessage) string {
	if msg == nil {
		return ""
	}
	s, err := rawMessageToPlainText(msg.Content)
	if err != nil {
		return ""
	}
	return s
}

func ptrString(s string) *string { return &s }

func ptrStringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncateBody(b []byte) string {
	const max = 2048
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
