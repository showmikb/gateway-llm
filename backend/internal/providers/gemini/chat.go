package gemini

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

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

const defaultAPIBase = "https://generativelanguage.googleapis.com"

// GeminiProvider implements chat, streaming completions (via chat), embeddings, and legacy completions.
type GeminiProvider struct {
	httpClient *http.Client
}

func New() *GeminiProvider {
	return &GeminiProvider{httpClient: http.DefaultClient}
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Capabilities() []providers.Capability {
	return []providers.Capability{providers.CapChat, providers.CapCompletions, providers.CapEmbeddings}
}

func (p *GeminiProvider) TransformChatRequest(ctx context.Context, req *types.ChatCompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	body, err := buildGeminiRequest(req)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	base := normalizeAPIBase(apiBase)
	model := normalizeModelName(req.Model)
	u, err := url.Parse(fmt.Sprintf("%s/v1beta/models/%s", base, url.PathEscape(model)))
	if err != nil {
		return nil, err
	}
	if req.Stream {
		u.Path += ":streamGenerateContent"
		q := url.Values{}
		q.Set("alt", "sse")
		q.Set("key", apiKey)
		u.RawQuery = q.Encode()
	} else {
		u.Path += ":generateContent"
		q := url.Values{}
		q.Set("key", apiKey)
		u.RawQuery = q.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (p *GeminiProvider) TransformChatResponse(resp *http.Response) (*types.ChatCompletionResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini: %s: %s", resp.Status, string(body))
	}

	var gr GeminiResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, err
	}
	return geminiResponseToChatCompletion(&gr)
}

func (p *GeminiProvider) TransformEmbeddingsRequest(ctx context.Context, req *types.EmbeddingRequest, apiKey string, apiBase string) (*http.Request, error) {
	inputs, err := parseEmbeddingInputs(req.Input)
	if err != nil {
		return nil, err
	}

	base := normalizeAPIBase(apiBase)
	model := normalizeModelName(req.Model)

	var payload []byte
	var pathSuffix string

	if len(inputs) == 1 {
		gr := GeminiEmbedRequest{
			Model:   fmt.Sprintf("models/%s", model),
			Content: GeminiEmbedContent{Parts: []GeminiPart{{Text: inputs[0]}}},
		}
		payload, err = json.Marshal(gr)
		if err != nil {
			return nil, err
		}
		pathSuffix = ":embedContent"
	} else {
		items := make([]GeminiBatchEmbedItem, 0, len(inputs))
		for _, s := range inputs {
			items = append(items, GeminiBatchEmbedItem{
				Content: GeminiEmbedContent{Parts: []GeminiPart{{Text: s}}},
			})
		}
		batch := GeminiBatchEmbedRequest{Requests: items}
		payload, err = json.Marshal(batch)
		if err != nil {
			return nil, err
		}
		pathSuffix = ":batchEmbedContents"
	}

	u, err := url.Parse(fmt.Sprintf("%s/v1beta/models/%s%s", base, url.PathEscape(model), pathSuffix))
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("key", apiKey)
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (p *GeminiProvider) TransformEmbeddingsResponse(resp *http.Response) (*types.EmbeddingResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini embeddings: %s: %s", resp.Status, string(body))
	}

	var keys struct {
		Embedding  json.RawMessage   `json:"embedding"`
		Embeddings []GeminiEmbedding `json:"embeddings"`
	}
	if err := json.Unmarshal(body, &keys); err != nil {
		return nil, err
	}

	if len(keys.Embedding) > 0 {
		var single GeminiEmbedResponse
		if err := json.Unmarshal(body, &single); err != nil {
			return nil, err
		}
		return &types.EmbeddingResponse{
			Object: "list",
			Data: []types.EmbeddingData{
				{Object: "embedding", Embedding: single.Embedding.Values, Index: 0},
			},
			Model: "",
			Usage: nil,
		}, nil
	}

	if len(keys.Embeddings) > 0 {
		data := make([]types.EmbeddingData, 0, len(keys.Embeddings))
		for i, emb := range keys.Embeddings {
			data = append(data, types.EmbeddingData{
				Object:    "embedding",
				Embedding: emb.Values,
				Index:     i,
			})
		}
		return &types.EmbeddingResponse{
			Object: "list",
			Data:   data,
			Model:  "",
			Usage:  nil,
		}, nil
	}

	return nil, fmt.Errorf("gemini embeddings: unexpected response body")
}

func (p *GeminiProvider) TransformCompletionsRequest(ctx context.Context, req *types.CompletionRequest, apiKey string, apiBase string) (*http.Request, error) {
	messages, err := promptToChatMessages(req.Prompt)
	if err != nil {
		return nil, err
	}
	chatReq := &types.ChatCompletionRequest{
		Model:            req.Model,
		Messages:         messages,
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
	return p.TransformChatRequest(ctx, chatReq, apiKey, apiBase)
}

func (p *GeminiProvider) TransformCompletionsResponse(resp *http.Response) (*types.CompletionResponse, error) {
	chat, err := p.TransformChatResponse(resp)
	if err != nil {
		return nil, err
	}
	out := &types.CompletionResponse{
		ID:      chat.ID,
		Object:  "text_completion",
		Created: chat.Created,
		Model:   chat.Model,
		Usage:   chat.Usage,
	}
	if len(chat.Choices) == 0 {
		return out, nil
	}
	cc := make([]types.CompletionChoice, 0, len(chat.Choices))
	for i, ch := range chat.Choices {
		text := ""
		if ch.Message != nil {
			text = messageTextContent(ch.Message.Content)
		}
		cc = append(cc, types.CompletionChoice{
			Text:         text,
			Index:        i,
			FinishReason: ch.FinishReason,
		})
	}
	out.Choices = cc
	return out, nil
}

// --- helpers ---

func normalizeAPIBase(apiBase string) string {
	s := strings.TrimSpace(apiBase)
	if s == "" {
		return defaultAPIBase
	}
	return strings.TrimSuffix(s, "/")
}

func normalizeModelName(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "models/")
}

func buildGeminiRequest(req *types.ChatCompletionRequest) (*GeminiRequest, error) {
	var systemTexts []string
	var contents []GeminiContent

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			t := messageTextContent(msg.Content)
			if t != "" {
				systemTexts = append(systemTexts, t)
			}
		case "user":
			c, err := openAIMessageToGeminiUser(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, c)
		case "assistant":
			c, err := openAIAssistantToGemini(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, c)
		case "tool":
			c, err := openAIToolToGemini(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, c)
		default:
			c, err := openAIMessageToGeminiUser(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, c)
		}
	}

	out := &GeminiRequest{Contents: contents}
	if len(systemTexts) > 0 {
		merged := strings.Join(systemTexts, "\n\n")
		out.SystemInstruction = &GeminiContent{Parts: []GeminiPart{{Text: merged}}}
	}

	maxTok := req.MaxTokens
	if req.MaxCompletionTokens != nil {
		maxTok = req.MaxCompletionTokens
	}
	if req.Temperature != nil || req.TopP != nil || maxTok != nil || len(parseStopSequences(req.Stop)) > 0 {
		out.GenerationConfig = &GenerationConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: maxTok,
			StopSequences:   parseStopSequences(req.Stop),
		}
	}

	if len(req.Tools) > 0 {
		decls := make([]GeminiFunctionDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			if t.Type != "" && t.Type != "function" {
				continue
			}
			decls = append(decls, GeminiFunctionDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		if len(decls) > 0 {
			out.Tools = []GeminiToolDeclaration{{FunctionDeclarations: decls}}
		}
	}

	return out, nil
}

func openAIMessageToGeminiUser(msg types.ChatMessage) (GeminiContent, error) {
	text := messageTextContent(msg.Content)
	return GeminiContent{Role: "user", Parts: []GeminiPart{{Text: text}}}, nil
}

func openAIAssistantToGemini(msg types.ChatMessage) (GeminiContent, error) {
	parts := make([]GeminiPart, 0, 1+len(msg.ToolCalls))
	if t := strings.TrimSpace(messageTextContent(msg.Content)); t != "" {
		parts = append(parts, GeminiPart{Text: t})
	}
	for _, tc := range msg.ToolCalls {
		if tc.Type != "" && tc.Type != "function" {
			continue
		}
		args := json.RawMessage(tc.Function.Arguments)
		if !json.Valid(args) {
			args = json.RawMessage(`{}`)
		}
		parts = append(parts, GeminiPart{
			FunctionCall: &GeminiFunctionCall{
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}
	if len(parts) == 0 {
		parts = append(parts, GeminiPart{Text: ""})
	}
	return GeminiContent{Role: "model", Parts: parts}, nil
}

func openAIToolToGemini(msg types.ChatMessage) (GeminiContent, error) {
	name := msg.Name
	if name == "" {
		name = "tool"
	}
	resp := toolResponsePayload(msg.Content)
	return GeminiContent{
		Role: "user",
		Parts: []GeminiPart{
			{
				FunctionResponse: &GeminiFunctionResponse{
					Name:     name,
					Response: resp,
				},
			},
		},
	}, nil
}

func toolResponsePayload(content json.RawMessage) json.RawMessage {
	if len(content) == 0 {
		return json.RawMessage(`{}`)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(content, &obj) == nil && len(obj) > 0 {
		out, err := json.Marshal(obj)
		if err != nil {
			return json.RawMessage(`{}`)
		}
		return out
	}
	var s string
	if json.Unmarshal(content, &s) == nil {
		type wrap struct {
			Result string `json:"result"`
		}
		out, _ := json.Marshal(wrap{Result: s})
		return out
	}
	return content
}

func messageTextContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" || p.Text != "" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}

func parseStopSequences(stop json.RawMessage) []string {
	if len(stop) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(stop, &s) == nil {
		return []string{s}
	}
	var ss []string
	if json.Unmarshal(stop, &ss) == nil {
		return ss
	}
	return nil
}

func parseEmbeddingInputs(input json.RawMessage) ([]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("empty embedding input")
	}
	var s string
	if json.Unmarshal(input, &s) == nil {
		return []string{s}, nil
	}
	var ss []string
	if json.Unmarshal(input, &ss) == nil {
		if len(ss) == 0 {
			return nil, fmt.Errorf("empty embedding input array")
		}
		return ss, nil
	}
	return nil, fmt.Errorf("unsupported embedding input shape")
}

func promptToChatMessages(prompt json.RawMessage) ([]types.ChatMessage, error) {
	if len(prompt) == 0 {
		return nil, fmt.Errorf("empty prompt")
	}
	var s string
	if json.Unmarshal(prompt, &s) == nil {
		c, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		return []types.ChatMessage{{Role: "user", Content: json.RawMessage(c)}}, nil
	}
	var ss []string
	if json.Unmarshal(prompt, &ss) == nil {
		joined := strings.Join(ss, "")
		c, err := json.Marshal(joined)
		if err != nil {
			return nil, err
		}
		return []types.ChatMessage{{Role: "user", Content: json.RawMessage(c)}}, nil
	}
	return nil, fmt.Errorf("unsupported prompt format")
}

func mapFinishReason(reason string) *string {
	switch strings.TrimSpace(reason) {
	case "", "FINISH_REASON_UNSPECIFIED", "FINISH_REASON_OTHER":
		return nil
	case "STOP", "FINISH_REASON_STOP":
		s := "stop"
		return &s
	case "MAX_TOKENS", "FINISH_REASON_MAX_TOKENS":
		s := "length"
		return &s
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII",
		"FINISH_REASON_SAFETY", "FINISH_REASON_RECITATION", "FINISH_REASON_BLOCKLIST",
		"FINISH_REASON_PROHIBITED_CONTENT", "FINISH_REASON_SPII":
		s := "content_filter"
		return &s
	default:
		s := "stop"
		return &s
	}
}

func geminiUsageToOpenAI(u *GeminiUsage) *types.Usage {
	if u == nil {
		return nil
	}
	return &types.Usage{
		PromptTokens:     u.PromptTokenCount,
		CompletionTokens: u.CandidatesTokenCount,
		TotalTokens:      u.TotalTokenCount,
	}
}

func geminiResponseToChatCompletion(gr *GeminiResponse) (*types.ChatCompletionResponse, error) {
	id := "chatcmpl-" + uuid.NewString()
	out := &types.ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "",
		Choices: nil,
		Usage:   geminiUsageToOpenAI(gr.UsageMetadata),
	}
	if len(gr.Candidates) == 0 {
		out.Choices = []types.Choice{{Index: 0, Message: &types.ChatMessage{Role: "assistant", Content: json.RawMessage(`""`)}, FinishReason: mapFinishReason("")}}
		return out, nil
	}

	c0 := gr.Candidates[0]
	text, toolCalls, err := partsToAssistantMessage(c0.Content.Parts)
	if err != nil {
		return nil, err
	}
	contentJSON, err := json.Marshal(text)
	if err != nil {
		return nil, err
	}
	msg := &types.ChatMessage{Role: "assistant", Content: contentJSON}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	out.Choices = []types.Choice{
		{
			Index:        0,
			Message:      msg,
			FinishReason: mapFinishReason(c0.FinishReason),
		},
	}
	return out, nil
}

func partsToAssistantMessage(parts []GeminiPart) (string, []types.ToolCall, error) {
	var textBuf strings.Builder
	var calls []types.ToolCall
	for i, p := range parts {
		if p.Text != "" {
			textBuf.WriteString(p.Text)
		}
		if p.FunctionCall != nil {
			argsStr := string(p.FunctionCall.Args)
			if !json.Valid(p.FunctionCall.Args) {
				argsStr = "{}"
			}
			calls = append(calls, types.ToolCall{
				ID:   fmt.Sprintf("call_%d", i),
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      p.FunctionCall.Name,
					Arguments: argsStr,
				},
			})
		}
	}
	return textBuf.String(), calls, nil
}

var (
	_ providers.ChatProvider        = (*GeminiProvider)(nil)
	_ providers.EmbeddingsProvider  = (*GeminiProvider)(nil)
	_ providers.CompletionsProvider = (*GeminiProvider)(nil)
)
