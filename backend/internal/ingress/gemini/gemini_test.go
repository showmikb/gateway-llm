package gemini

import (
	"encoding/json"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

func TestParse_GenerateContent(t *testing.T) {
	body := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"hi"}]}],
		"systemInstruction":{"parts":[{"text":"be concise"}]},
		"generationConfig":{"temperature":0.3,"maxOutputTokens":128,"responseMimeType":"application/json"}
	}`)
	req, stream, err := Parse(body, "gemini-1.5-pro")
	if err != nil {
		t.Fatal(err)
	}
	if stream {
		t.Fatalf("stream false expected")
	}
	if req.Model != "gemini-1.5-pro" {
		t.Fatalf("model: %s", req.Model)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected system+user, got %+v", req.Messages)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 128 {
		t.Fatalf("max tokens: %v", req.MaxTokens)
	}
	if string(req.ResponseFormat) == "" {
		t.Fatalf("response_format should be set")
	}
}

func TestParse_FunctionCallAndResponse(t *testing.T) {
	body := []byte(`{
		"contents":[
			{"role":"user","parts":[{"text":"weather"}]},
			{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"sf"}}}]},
			{"role":"function","parts":[{"functionResponse":{"name":"get_weather","response":{"temp":60}}}]}
		]
	}`)
	req, _, err := Parse(body, "gemini-1.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, m := range req.Messages {
		roles = append(roles, m.Role)
	}
	hasTool := false
	for _, r := range roles {
		if r == "tool" {
			hasTool = true
		}
	}
	if !hasTool {
		t.Fatalf("expected tool role from functionResponse, got %v", roles)
	}
}

func TestFormat_Basic(t *testing.T) {
	msg := &types.ChatMessage{Role: "assistant"}
	msg.Content, _ = json.Marshal("hello world")
	fr := "stop"
	resp := &types.ChatCompletionResponse{
		Model: "gemini-1.5-pro",
		Choices: []types.Choice{{Index: 0, Message: msg, FinishReason: &fr}},
		Usage: &types.Usage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6},
	}
	body, err := Format(resp)
	if err != nil {
		t.Fatal(err)
	}
	var out GenerateContentResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].FinishReason != "STOP" {
		t.Fatalf("candidates: %+v", out.Candidates)
	}
	if out.Candidates[0].Content.Parts[0].Text != "hello world" {
		t.Fatalf("part text: %+v", out.Candidates[0].Content.Parts)
	}
	if out.UsageMetadata == nil || out.UsageMetadata.TotalTokenCount != 6 {
		t.Fatalf("usage: %+v", out.UsageMetadata)
	}
}
