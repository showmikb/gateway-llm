package types

import "encoding/json"

type ChatCompletionRequest struct {
	Model               string          `json:"model"`
	Messages            []ChatMessage   `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	N                   *int            `json:"n,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat      json.RawMessage `json:"response_format,omitempty"`
	Seed                *int            `json:"seed,omitempty"`
	User                string          `json:"user,omitempty"`
	StreamOptions       *StreamOptions  `json:"stream_options,omitempty"`

	// --- LiteLLM / gateway-llm passthrough (not sent upstream) ---
	// Accepted so LiteLLM users can paste existing code verbatim. These
	// are stripped before the request is translated to a provider format;
	// they flow to Recordings, SpendLogs, and callbacks instead.
	Metadata        map[string]any `json:"metadata,omitempty"`
	Tags            []string       `json:"tags,omitempty"`
	TeamID          string         `json:"team_id,omitempty"`
	TraceID         string         `json:"trace_id,omitempty"`
	GenerationName  string         `json:"generation_name,omitempty"`
	Caching         *bool          `json:"caching,omitempty"`
	TTL             *int           `json:"ttl,omitempty"`
	NoLog           bool           `json:"no-log,omitempty"`
	MockResponse    string         `json:"mock_response,omitempty"`
	// GatewayLLM-specific routing hints
	RouterHint      string         `json:"x_gateway_llm_route,omitempty"` // "smart" | "cheap" | "fast" | "quality"
	QualityFloor    *float64       `json:"x_gateway_llm_quality_floor,omitempty"`
	ReplayRecord    *bool          `json:"x_gateway_llm_record,omitempty"`
	Replay          *ReplayOptions `json:"x_gateway_llm_replay,omitempty"`
}

// ReplayOptions lets a caller request this specific request be recorded and/or
// replayed. Present in the request body so it survives client SDK passthrough.
type ReplayOptions struct {
	Dataset string `json:"dataset,omitempty"`
	Tag     string `json:"tag,omitempty"`
}

// Passthrough captures the fields that are meaningful for spend/recording but
// must not reach providers. It's what TransformChatRequest and the bridge
// copy off so they can scrub the request body before forwarding upstream.
type Passthrough struct {
	Metadata       map[string]any
	Tags           []string
	TeamID         string
	TraceID        string
	GenerationName string
	Caching        *bool
	TTL            *int
	NoLog          bool
	MockResponse   string
	RouterHint     string
	QualityFloor   *float64
	ReplayRecord   *bool
	Replay         *ReplayOptions
}

// ExtractPassthrough copies the non-OpenAI fields off and clears them from the
// request, so what the provider sees is a pristine OpenAI payload.
func (r *ChatCompletionRequest) ExtractPassthrough() Passthrough {
	p := Passthrough{
		Metadata:       r.Metadata,
		Tags:           r.Tags,
		TeamID:         r.TeamID,
		TraceID:        r.TraceID,
		GenerationName: r.GenerationName,
		Caching:        r.Caching,
		TTL:            r.TTL,
		NoLog:          r.NoLog,
		MockResponse:   r.MockResponse,
		RouterHint:     r.RouterHint,
		QualityFloor:   r.QualityFloor,
		ReplayRecord:   r.ReplayRecord,
		Replay:         r.Replay,
	}
	r.Metadata = nil
	r.Tags = nil
	r.TeamID = ""
	r.TraceID = ""
	r.GenerationName = ""
	r.Caching = nil
	r.TTL = nil
	r.NoLog = false
	r.MockResponse = ""
	r.RouterHint = ""
	r.QualityFloor = nil
	r.ReplayRecord = nil
	r.Replay = nil
	return p
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

type Choice struct {
	Index        int          `json:"index"`
	Message      *ChatMessage `json:"message,omitempty"`
	Delta        *ChatMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type ChatCompletionChunk struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}
