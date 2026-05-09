package types

import "encoding/json"

type ResponsesRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input"`
	Instructions       string          `json:"instructions,omitempty"`
	MaxOutputTokens    *int            `json:"max_output_tokens,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	TopP               *float64        `json:"top_p,omitempty"`
	Tools              []Tool          `json:"tools,omitempty"`
	ToolChoice         json.RawMessage `json:"tool_choice,omitempty"`
	Stream             bool            `json:"stream,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Store              *bool           `json:"store,omitempty"`
	User               string          `json:"user,omitempty"`
}

type ResponsesAPIResponse struct {
	ID           string               `json:"id"`
	Object       string               `json:"object"`
	CreatedAt    int64                `json:"created_at"`
	Status       string               `json:"status"`
	Model        string               `json:"model"`
	Output       []ResponseOutputItem `json:"output"`
	Usage        *ResponsesUsage      `json:"usage,omitempty"`
	Instructions string               `json:"instructions,omitempty"`
}

type ResponseOutputItem struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Status  string          `json:"status,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content []OutputContent `json:"content,omitempty"`
}

type OutputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type ResponsesStreamEvent struct {
	Type     string          `json:"type"`
	Response json.RawMessage `json:"response,omitempty"`
	Delta    string          `json:"delta,omitempty"`
	ItemID   string          `json:"item_id,omitempty"`
}
