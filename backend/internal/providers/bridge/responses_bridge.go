package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

// ResponsesBridge adapts any ChatProvider to serve the Responses API
// by converting input -> messages and output -> response items.
type ResponsesBridge struct {
	chat providers.ChatProvider
}

func NewResponsesBridge(chat providers.ChatProvider) *ResponsesBridge {
	return &ResponsesBridge{chat: chat}
}

func (b *ResponsesBridge) Name() string {
	return b.chat.Name()
}

func (b *ResponsesBridge) Capabilities() []providers.Capability {
	caps := b.chat.Capabilities()
	return append(caps, providers.CapResponses)
}

func (b *ResponsesBridge) TransformResponsesRequest(ctx context.Context, req *types.ResponsesRequest, apiKey string, apiBase string) (*http.Request, error) {
	chatReq := b.convertToChatRequest(req)
	return b.chat.TransformChatRequest(ctx, chatReq, apiKey, apiBase)
}

func (b *ResponsesBridge) TransformResponsesResponse(resp *http.Response) (*types.ResponsesAPIResponse, error) {
	chatResp, err := b.chat.TransformChatResponse(resp)
	if err != nil {
		return nil, err
	}
	return b.convertFromChatResponse(chatResp), nil
}

func (b *ResponsesBridge) StreamResponsesResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	chatCh, err := b.chat.StreamChatResponse(ctx, resp)
	if err != nil {
		return nil, err
	}

	out := make(chan providers.StreamEvent, 64)
	go func() {
		defer close(out)

		respID := "resp_" + uuid.New().String()[:12]

		// Send response.created event
		createdEvent, _ := json.Marshal(map[string]interface{}{
			"type": "response.created",
			"response": map[string]interface{}{
				"id":     respID,
				"status": "in_progress",
			},
		})
		out <- providers.StreamEvent{Data: createdEvent}

		for evt := range chatCh {
			if evt.Error != nil {
				out <- evt
				return
			}
			if evt.Done {
				// Send response.completed event
				doneEvent, _ := json.Marshal(map[string]interface{}{
					"type": "response.completed",
					"response": map[string]interface{}{
						"id":     respID,
						"status": "completed",
					},
				})
				out <- providers.StreamEvent{Data: doneEvent}
				out <- providers.StreamEvent{Done: true}
				return
			}

			// Convert chat chunk to responses text delta
			var chunk types.ChatCompletionChunk
			if err := json.Unmarshal(evt.Data, &chunk); err != nil {
				continue
			}

			for _, choice := range chunk.Choices {
				if choice.Delta != nil && choice.Delta.Content != nil {
					var text string
					json.Unmarshal(choice.Delta.Content, &text)
					if text != "" {
						deltaEvent, _ := json.Marshal(map[string]interface{}{
							"type":  "response.output_text.delta",
							"delta": text,
						})
						out <- providers.StreamEvent{Data: deltaEvent}
					}
				}
			}
		}
	}()

	return out, nil
}

func (b *ResponsesBridge) convertToChatRequest(req *types.ResponsesRequest) *types.ChatCompletionRequest {
	chatReq := &types.ChatCompletionRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Tools:       req.Tools,
		ToolChoice:  req.ToolChoice,
		MaxCompletionTokens: req.MaxOutputTokens,
		User:        req.User,
	}

	var messages []types.ChatMessage

	if req.Instructions != "" {
		sysContent, _ := json.Marshal(req.Instructions)
		messages = append(messages, types.ChatMessage{
			Role:    "system",
			Content: sysContent,
		})
	}

	// Parse input: can be a string or an array of structured items
	var inputStr string
	if err := json.Unmarshal(req.Input, &inputStr); err == nil {
		content, _ := json.Marshal(inputStr)
		messages = append(messages, types.ChatMessage{
			Role:    "user",
			Content: content,
		})
	} else {
		var inputItems []map[string]interface{}
		if err := json.Unmarshal(req.Input, &inputItems); err == nil {
			for _, item := range inputItems {
				role, _ := item["role"].(string)
				if role == "" {
					role = "user"
				}
				content, _ := json.Marshal(item["content"])
				messages = append(messages, types.ChatMessage{
					Role:    role,
					Content: content,
				})
			}
		} else {
			content, _ := json.Marshal(string(req.Input))
			messages = append(messages, types.ChatMessage{
				Role:    "user",
				Content: content,
			})
		}
	}

	chatReq.Messages = messages
	return chatReq
}

func (b *ResponsesBridge) convertFromChatResponse(chatResp *types.ChatCompletionResponse) *types.ResponsesAPIResponse {
	resp := &types.ResponsesAPIResponse{
		ID:        fmt.Sprintf("resp_%s", chatResp.ID),
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Status:    "completed",
		Model:     chatResp.Model,
	}

	if chatResp.Usage != nil {
		resp.Usage = &types.ResponsesUsage{
			InputTokens:  chatResp.Usage.PromptTokens,
			OutputTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:  chatResp.Usage.TotalTokens,
		}
	}

	for _, choice := range chatResp.Choices {
		if choice.Message != nil {
			item := types.ResponseOutputItem{
				Type:   "message",
				ID:     fmt.Sprintf("msg_%s", uuid.New().String()[:8]),
				Status: "completed",
				Role:   choice.Message.Role,
			}
			if choice.Message.Content != nil {
				var text string
				json.Unmarshal(choice.Message.Content, &text)
				if text != "" {
					item.Content = []types.OutputContent{
						{Type: "output_text", Text: text},
					}
				}
			}
			resp.Output = append(resp.Output, item)
		}
	}

	return resp
}
