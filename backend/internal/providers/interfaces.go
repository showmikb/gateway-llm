package providers

import (
	"context"
	"io"
	"net/http"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

type Capability string

const (
	CapChat          Capability = "chat"
	CapResponses     Capability = "responses"
	CapEmbeddings    Capability = "embeddings"
	CapCompletions   Capability = "completions"
	CapImages        Capability = "images"
	CapAudioSpeech   Capability = "audio_speech"
	CapAudioTranscribe Capability = "audio_transcribe"
	CapModerations   Capability = "moderations"
)

type Provider interface {
	Name() string
	Capabilities() []Capability
}

type ChatProvider interface {
	Provider
	TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error)
	TransformChatResponse(resp *http.Response) (*types.ChatCompletionResponse, error)
	StreamChatResponse(ctx context.Context, resp *http.Response) (<-chan StreamEvent, error)
}

type EmbeddingsProvider interface {
	Provider
	TransformEmbeddingsRequest(ctx context.Context, req *types.EmbeddingRequest, apiKey string, apiBase string) (*http.Request, error)
	TransformEmbeddingsResponse(resp *http.Response) (*types.EmbeddingResponse, error)
}

type CompletionsProvider interface {
	Provider
	TransformCompletionsRequest(ctx context.Context, req *types.CompletionRequest, apiKey string, apiBase string) (*http.Request, error)
	TransformCompletionsResponse(resp *http.Response) (*types.CompletionResponse, error)
}

type ImageProvider interface {
	Provider
	TransformImageRequest(ctx context.Context, req *types.ImageRequest, apiKey string, apiBase string) (*http.Request, error)
	TransformImageResponse(resp *http.Response) (*types.ImageResponse, error)
}

type AudioProvider interface {
	Provider
	TransformSpeechRequest(ctx context.Context, req *types.SpeechRequest, apiKey string, apiBase string) (*http.Request, error)
	StreamSpeechResponse(resp *http.Response) (io.ReadCloser, string, error)
}

type ModerationProvider interface {
	Provider
	TransformModerationRequest(ctx context.Context, req *types.ModerationRequest, apiKey string, apiBase string) (*http.Request, error)
	TransformModerationResponse(resp *http.Response) (*types.ModerationResponse, error)
}

type ResponsesProvider interface {
	Provider
	TransformResponsesRequest(ctx context.Context, req *types.ResponsesRequest, apiKey string, apiBase string) (*http.Request, error)
	TransformResponsesResponse(resp *http.Response) (*types.ResponsesAPIResponse, error)
	StreamResponsesResponse(ctx context.Context, resp *http.Response) (<-chan StreamEvent, error)
}

type StreamEvent struct {
	Data  []byte
	Error error
	Done  bool
}

// PassthroughProvider forwards raw HTTP requests to an upstream
// OpenAI-compatible API without requiring typed request/response structs.
// Used by the OpenAI compatibility layer for endpoints that Gateway-LLM
// does not have typed handlers for (files, assistants, batches, etc.).
type PassthroughProvider interface {
	Provider
	// ForwardRequest builds an upstream HTTP request from the original
	// inbound request. The path is the sub-path under /v1 (e.g.
	// "/files", "/assistants/asst_123"). The provider rewrites the
	// host/scheme and injects auth but leaves everything else intact.
	ForwardRequest(ctx context.Context, method, path, rawQuery string, header http.Header, body io.Reader, apiKey string, apiBase string) (*http.Request, error)
}

// CapPassthrough indicates a provider supports generic HTTP forwarding.
const CapPassthrough Capability = "passthrough"

type DiscoveredModel struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	OwnedBy      string   `json:"owned_by"`
	Capabilities []string `json:"capabilities"`
}

type ModelDiscoveryProvider interface {
	Provider
	DiscoverModels(ctx context.Context, apiKey string, apiBase string) ([]DiscoveredModel, error)
}
