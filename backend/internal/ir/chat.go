// Package ir holds the canonical intermediate representation that every
// ingress translates *into* and every provider translates *from*. It is
// deliberately small, minimal, and sheds wire-specific quirks.
//
// Invariants:
//   1. The IR is the single source of truth once a request has been parsed.
//      Ingresses produce it; providers consume it. Passthrough fields (LiteLLM
//      metadata, gateway-llm routing hints) live on the IR so they survive.
//   2. The IR is never sent on the wire. It's serialized only for recordings
//      and replay.
//   3. Anything the IR can't represent is carried in `Extra` as opaque JSON.
//      This keeps the IR stable while letting unusual provider features flow.
package ir

import (
	"encoding/json"
	"time"
)

// ChatRequest is the canonical shape every ingress normalizes to.
type ChatRequest struct {
	// Alias is the gateway-llm model alias or provider/model pair the
	// caller asked for. After routing resolution it is the canonical alias.
	Alias string `json:"alias"`

	// Messages in canonical order, earliest first.
	Messages []Message `json:"messages"`

	// Generation params (pointers so "unset" is distinguishable from 0).
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	N                   *int            `json:"n,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	Seed                *int            `json:"seed,omitempty"`
	User                string          `json:"user,omitempty"`

	// Tool-use.
	Tools      []Tool          `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`

	// Structured-output.
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`

	// Streaming options.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	// Passthrough. Metadata flows to recording + callbacks; it is NOT sent
	// to the provider.
	Metadata       map[string]any `json:"metadata,omitempty"`
	Tags           []string       `json:"tags,omitempty"`
	TeamID         string         `json:"team_id,omitempty"`
	TraceID        string         `json:"trace_id,omitempty"`
	GenerationName string         `json:"generation_name,omitempty"`
	Caching        *bool          `json:"caching,omitempty"`
	TTL            *int           `json:"ttl,omitempty"`
	NoLog          bool           `json:"no_log,omitempty"`
	MockResponse   string         `json:"mock_response,omitempty"`

	// Routing hints.
	RouterHint   string   `json:"router_hint,omitempty"`
	QualityFloor *float64 `json:"quality_floor,omitempty"`

	// Ingress tag: which wire format was this originally? Lets recordings
	// replay byte-exact against the original ingress if desired.
	Ingress string `json:"ingress,omitempty"`

	// Extra carries provider-specific fields that don't map cleanly onto
	// the canonical shape. Serialized as opaque JSON.
	Extra map[string]json.RawMessage `json:"extra,omitempty"`
}

// Message is role + content. Content is a union: it's either a string or
// a slice of content parts (text, image, audio). We model that with
// ContentParts when non-empty and ContentText otherwise.
type Message struct {
	Role         string         `json:"role"`
	ContentText  string         `json:"content_text,omitempty"`
	ContentParts []ContentPart  `json:"content_parts,omitempty"`
	Name         string         `json:"name,omitempty"`
	ToolCalls    []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
}

// ContentPart models one item of the content array (text, image_url,
// input_audio). Only fields relevant to the part's Type are set.
type ContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *ImageURLPart   `json:"image_url,omitempty"`
	Audio    *AudioPart      `json:"audio,omitempty"`
	Extra    json.RawMessage `json:"extra,omitempty"`
}

type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type AudioPart struct {
	Data   string `json:"data"`
	Format string `json:"format,omitempty"`
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

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatResponse is the canonical completed response. Streams are a series of
// deltas that reduce to this.
type ChatResponse struct {
	ID                string    `json:"id"`
	Alias             string    `json:"alias"`
	Provider          string    `json:"provider"`
	ProviderModel     string    `json:"provider_model"`
	Created           time.Time `json:"created"`
	Choices           []Choice  `json:"choices"`
	Usage             *Usage    `json:"usage,omitempty"`
	SystemFingerprint string    `json:"system_fingerprint,omitempty"`
}

type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Message `json:"delta,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
}
