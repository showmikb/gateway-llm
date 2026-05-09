// Package anthropic is the wire-format adapter for Anthropic's Messages API.
// It accepts the Anthropic SDK's request shape on /anthropic/v1/messages,
// translates it to our canonical OpenAI-shaped types.ChatCompletionRequest
// (which is itself a thin wrapper around ir.ChatRequest), and translates
// the resulting OpenAI response back into Anthropic's Messages response
// shape so the caller's SDK doesn't notice.
//
// Non-streaming is fully supported. Streaming translation is implemented
// by StreamTranslator: it consumes OpenAI-format SSE chunks and emits
// Anthropic's event-typed SSE stream (`message_start`, `content_block_delta`,
// `message_delta`, `message_stop`).
//
// Reference:
//   - https://docs.anthropic.com/en/api/messages
package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

// Name is the ingress tag stored on recordings.
const Name = "anthropic"

// Request is the minimal subset of Anthropic's Messages request we parse.
// Fields we don't support yet are preserved in Extra so recordings round-trip.
type Request struct {
	Model         string            `json:"model"`
	Messages      []Message         `json:"messages"`
	System        json.RawMessage   `json:"system,omitempty"` // string OR [{type,text}]
	MaxTokens     int               `json:"max_tokens"`
	Temperature   *float64          `json:"temperature,omitempty"`
	TopP          *float64          `json:"top_p,omitempty"`
	TopK          *int              `json:"top_k,omitempty"`
	Stream        bool              `json:"stream,omitempty"`
	StopSequences []string          `json:"stop_sequences,omitempty"`
	Tools         []Tool            `json:"tools,omitempty"`
	ToolChoice    json.RawMessage   `json:"tool_choice,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
}

type Message struct {
	Role    string          `json:"role"`    // "user" | "assistant"
	Content json.RawMessage `json:"content"` // string OR ContentBlock[]
}

type ContentBlock struct {
	Type  string          `json:"type"` // "text" | "image" | "tool_use" | "tool_result"
	Text  string          `json:"text,omitempty"`
	Source *ImageSource   `json:"source,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type ImageSource struct {
	Type      string `json:"type"` // "base64" | "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// Response is the Anthropic Messages response shape we emit back.
type Response struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"` // "message"
	Role         string           `json:"role"` // "assistant"
	Model        string           `json:"model"`
	Content      []ResponseBlock  `json:"content"`
	StopReason   string           `json:"stop_reason,omitempty"`
	StopSequence *string          `json:"stop_sequence"`
	Usage        ResponseUsage    `json:"usage"`
}

type ResponseBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type ResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Parse converts an Anthropic Messages request body into the canonical
// OpenAI-shaped request the rest of the gateway operates on. It returns
// a `Stream` flag separately because the caller needs to know whether
// to set up SSE.
func Parse(body []byte) (*types.ChatCompletionRequest, bool, error) {
	var r Request
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, false, fmt.Errorf("anthropic parse: %w", err)
	}
	if r.Model == "" {
		return nil, false, fmt.Errorf("anthropic parse: missing model")
	}
	if len(r.Messages) == 0 {
		return nil, false, fmt.Errorf("anthropic parse: messages required")
	}

	out := &types.ChatCompletionRequest{
		Model:  r.Model,
		Stream: r.Stream,
	}
	if r.MaxTokens > 0 {
		mt := r.MaxTokens
		out.MaxTokens = &mt
	}
	if r.Temperature != nil {
		out.Temperature = r.Temperature
	}
	if r.TopP != nil {
		out.TopP = r.TopP
	}

	// System → OpenAI "system" message.
	if sys := decodeSystem(r.System); sys != "" {
		payload, _ := json.Marshal(sys)
		out.Messages = append(out.Messages, types.ChatMessage{
			Role:    "system",
			Content: payload,
		})
	}

	for _, m := range r.Messages {
		msg, err := decodeMessage(m)
		if err != nil {
			return nil, false, fmt.Errorf("anthropic parse message: %w", err)
		}
		out.Messages = append(out.Messages, msg...)
	}

	for _, t := range r.Tools {
		out.Tools = append(out.Tools, types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	if len(r.ToolChoice) > 0 {
		out.ToolChoice = r.ToolChoice
	}
	if len(r.StopSequences) > 0 {
		stop, _ := json.Marshal(r.StopSequences)
		out.Stop = stop
	}
	if len(r.Metadata) > 0 {
		out.Metadata = r.Metadata
	}

	return out, r.Stream, nil
}

// Format converts an OpenAI-shaped completion back into an Anthropic
// Messages response body.
func Format(resp *types.ChatCompletionResponse) ([]byte, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("anthropic format: empty response")
	}
	ch := resp.Choices[0]
	out := Response{
		ID:    resp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: resp.Model,
	}
	if ch.Message != nil {
		txt := decodeContent(ch.Message.Content)
		if txt != "" {
			out.Content = append(out.Content, ResponseBlock{Type: "text", Text: txt})
		}
		for _, tc := range ch.Message.ToolCalls {
			out.Content = append(out.Content, ResponseBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage(tc.Function.Arguments),
			})
		}
	}
	if ch.FinishReason != nil {
		out.StopReason = mapFinishReason(*ch.FinishReason)
	}
	if resp.Usage != nil {
		out.Usage = ResponseUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}
	return json.Marshal(out)
}

// FormatError renders a 4xx/5xx body in Anthropic's canonical error shape.
func FormatError(status int, msg string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]any{"type": classifyErr(status), "message": msg},
	})
	return b
}

func classifyErr(status int) string {
	switch {
	case status == 401 || status == 403:
		return "authentication_error"
	case status == 404:
		return "not_found_error"
	case status == 429:
		return "rate_limit_error"
	case status >= 500:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}

func decodeSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" {
				b.WriteString(blk.Text)
				b.WriteByte('\n')
			}
		}
		return strings.TrimRight(b.String(), "\n")
	}
	return ""
}

func decodeMessage(m Message) ([]types.ChatMessage, error) {
	var out []types.ChatMessage

	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		payload, _ := json.Marshal(text)
		out = append(out, types.ChatMessage{Role: m.Role, Content: payload})
		return out, nil
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return nil, fmt.Errorf("anthropic: content must be string or blocks: %w", err)
	}

	// We collect text/image blocks into a single OpenAI message, and
	// split tool_use/tool_result into their own messages to preserve
	// Anthropic's richer semantics on the wire.
	var parts []map[string]any
	var toolCalls []types.ToolCall
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": blk.Text})
		case "image":
			if blk.Source != nil {
				url := blk.Source.URL
				if url == "" && blk.Source.Data != "" && blk.Source.MediaType != "" {
					url = "data:" + blk.Source.MediaType + ";base64," + blk.Source.Data
				}
				if url != "" {
					parts = append(parts, map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": url},
					})
				}
			}
		case "tool_use":
			toolCalls = append(toolCalls, types.ToolCall{
				ID:   blk.ID,
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      blk.Name,
					Arguments: string(blk.Input),
				},
			})
		case "tool_result":
			// Emit a separate "tool" role message so OpenAI models can
			// read it as a function-call result.
			payload, _ := json.Marshal(string(blk.Content))
			out = append(out, types.ChatMessage{
				Role:       "tool",
				Content:    payload,
				ToolCallID: blk.ToolUseID,
			})
		}
	}
	if len(parts) > 0 {
		payload, _ := json.Marshal(parts)
		msg := types.ChatMessage{Role: m.Role, Content: payload}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		out = append([]types.ChatMessage{msg}, out...)
	} else if len(toolCalls) > 0 {
		out = append([]types.ChatMessage{{Role: m.Role, ToolCalls: toolCalls}}, out...)
	}
	if len(out) == 0 {
		out = append(out, types.ChatMessage{Role: m.Role, Content: json.RawMessage(`""`)})
	}
	return out, nil
}

func decodeContent(raw json.RawMessage) string {
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

func mapFinishReason(r string) string {
	switch r {
	case "stop", "stop_sequence":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

// ---- SSE translation --------------------------------------------------

// TranslateStream takes the raw OpenAI-format SSE bytes produced by a
// provider and returns the equivalent Anthropic-format SSE bytes. The
// stream is buffered in full first; for the hot path we run this on the
// response body after provider translation is complete.
//
// For true streaming translation the handler wires StreamTranslator over
// the upstream body; this helper is kept for tests and small payloads.
func TranslateStream(openaiSSE []byte, model string) []byte {
	var out bytes.Buffer
	ts := NewStreamTranslator(model)
	for _, line := range bytes.Split(openaiSSE, []byte("\n")) {
		evts, _ := ts.Feed(line)
		out.Write(evts)
	}
	return out.Bytes()
}

// StreamTranslator is a stateful SSE translator. Feed it OpenAI SSE
// lines as they arrive upstream and it emits the corresponding
// Anthropic event-typed SSE lines.
type StreamTranslator struct {
	model       string
	messageID   string
	started     bool
	inputTokens int
	outputText  strings.Builder
	done        bool
}

func NewStreamTranslator(model string) *StreamTranslator {
	return &StreamTranslator{model: model, messageID: "msg_" + fmt.Sprintf("%d", time.Now().UnixNano())}
}

// Feed consumes one upstream SSE line and returns any Anthropic-shaped
// SSE bytes to emit. Non-data lines are ignored.
func (s *StreamTranslator) Feed(line []byte) ([]byte, error) {
	trim := bytes.TrimSpace(line)
	if len(trim) == 0 || !bytes.HasPrefix(trim, []byte("data:")) {
		return nil, nil
	}
	payload := bytes.TrimSpace(trim[len("data:"):])
	if bytes.Equal(payload, []byte("[DONE]")) {
		return s.finish(), nil
	}

	var chunk types.ChatCompletionChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil, nil // ignore malformed
	}

	var out bytes.Buffer
	if !s.started {
		s.started = true
		msgStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    s.messageID,
				"type":  "message",
				"role":  "assistant",
				"model": s.model,
				"content": []any{},
				"stop_reason": nil,
				"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
			},
		}
		writeEvent(&out, "message_start", msgStart)
		cbStart := map[string]any{
			"type": "content_block_start",
			"index": 0,
			"content_block": map[string]any{"type": "text", "text": ""},
		}
		writeEvent(&out, "content_block_start", cbStart)
	}
	if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
		if dt := decodeContent(chunk.Choices[0].Delta.Content); dt != "" {
			s.outputText.WriteString(dt)
			delta := map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": dt},
			}
			writeEvent(&out, "content_block_delta", delta)
		}
	}
	if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != nil {
		cbStop := map[string]any{"type": "content_block_stop", "index": 0}
		writeEvent(&out, "content_block_stop", cbStop)
		msgDelta := map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   mapFinishReason(*chunk.Choices[0].FinishReason),
				"stop_sequence": nil,
			},
			"usage": map[string]int{
				"output_tokens": 0,
			},
		}
		if chunk.Usage != nil {
			msgDelta["usage"] = map[string]int{"output_tokens": chunk.Usage.CompletionTokens}
		}
		writeEvent(&out, "message_delta", msgDelta)
		s.done = true
	}
	return out.Bytes(), nil
}

func (s *StreamTranslator) finish() []byte {
	if s.done {
		var out bytes.Buffer
		writeEvent(&out, "message_stop", map[string]any{"type": "message_stop"})
		return out.Bytes()
	}
	return nil
}

func writeEvent(w *bytes.Buffer, name string, payload any) {
	data, _ := json.Marshal(payload)
	w.WriteString("event: ")
	w.WriteString(name)
	w.WriteByte('\n')
	w.WriteString("data: ")
	w.Write(data)
	w.WriteString("\n\n")
}
