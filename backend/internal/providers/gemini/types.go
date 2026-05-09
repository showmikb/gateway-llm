package gemini

import "encoding/json"

type GeminiRequest struct {
	Contents           []GeminiContent         `json:"contents"`
	SystemInstruction  *GeminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig   *GenerationConfig       `json:"generationConfig,omitempty"`
	Tools              []GeminiToolDeclaration `json:"tools,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

type GeminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type GeminiFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type GenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type GeminiToolDeclaration struct {
	FunctionDeclarations []GeminiFunctionDecl `json:"functionDeclarations,omitempty"`
}

type GeminiFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type GeminiResponse struct {
	Candidates    []GeminiCandidate `json:"candidates"`
	UsageMetadata *GeminiUsage      `json:"usageMetadata,omitempty"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
}

type GeminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Embeddings

type GeminiEmbedRequest struct {
	Model   string             `json:"model"`
	Content GeminiEmbedContent `json:"content"`
}

type GeminiEmbedContent struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiEmbedResponse struct {
	Embedding GeminiEmbedding `json:"embedding"`
}

type GeminiEmbedding struct {
	Values []float64 `json:"values"`
}

// Batch embeddings (models:batchEmbedContents)

type GeminiBatchEmbedRequest struct {
	Requests []GeminiBatchEmbedItem `json:"requests"`
}

type GeminiBatchEmbedItem struct {
	Content GeminiEmbedContent `json:"content"`
}

type GeminiBatchEmbedResponse struct {
	Embeddings []GeminiEmbedding `json:"embeddings"`
}
