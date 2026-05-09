package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

func TestParse_BasicMessage(t *testing.T) {
	body := []byte(`{
		"model":"claude-3-opus-20240229",
		"max_tokens":1024,
		"temperature":0.7,
		"messages":[{"role":"user","content":"Hello Claude"}]
	}`)
	req, stream, err := Parse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if stream {
		t.Fatalf("stream flag should be false")
	}
	if req.Model != "claude-3-opus-20240229" {
		t.Fatalf("model wrong: %s", req.Model)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 1024 {
		t.Fatalf("max_tokens: %v", req.MaxTokens)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("messages: %+v", req.Messages)
	}
}

func TestParse_SystemAndBlocks(t *testing.T) {
	body := []byte(`{
		"model":"claude-3-haiku-20240307",
		"max_tokens":256,
		"system":"You are a helpful assistant.",
		"messages":[{"role":"user","content":[{"type":"text","text":"Hi"}]}]
	}`)
	req, _, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected system + user, got %+v", req.Messages)
	}
	if req.Messages[0].Role != "system" {
		t.Fatalf("first must be system: %+v", req.Messages[0])
	}
}

func TestParse_ToolUse(t *testing.T) {
	body := []byte(`{
		"model":"claude-3-opus-20240229",
		"max_tokens":256,
		"messages":[{"role":"user","content":"what is 2+2"}],
		"tools":[{"name":"calc","description":"adds","input_schema":{"type":"object","properties":{"a":{"type":"number"}}}}]
	}`)
	req, _, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "calc" {
		t.Fatalf("tools not parsed: %+v", req.Tools)
	}
}

func TestFormat_BasicResponse(t *testing.T) {
	msg := &types.ChatMessage{Role: "assistant"}
	msg.Content, _ = json.Marshal("hi there")
	fr := "stop"
	resp := &types.ChatCompletionResponse{
		ID:    "chatcmpl-x",
		Model: "claude-3-opus-20240229",
		Choices: []types.Choice{
			{Index: 0, Message: msg, FinishReason: &fr},
		},
		Usage: &types.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}
	body, err := Format(resp)
	if err != nil {
		t.Fatal(err)
	}
	var parsed Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.StopReason != "end_turn" {
		t.Fatalf("stop_reason: %s", parsed.StopReason)
	}
	if len(parsed.Content) != 1 || parsed.Content[0].Type != "text" || parsed.Content[0].Text != "hi there" {
		t.Fatalf("content: %+v", parsed.Content)
	}
	if parsed.Usage.InputTokens != 5 || parsed.Usage.OutputTokens != 3 {
		t.Fatalf("usage: %+v", parsed.Usage)
	}
}

func TestTranslateStream_Smoke(t *testing.T) {
	sse := []byte("data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"he\"}}]}\n" +
		"data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"llo\"},\"finish_reason\":\"stop\"}]}\n" +
		"data: [DONE]\n")
	out := TranslateStream(sse, "claude-3")
	s := string(out)
	if !strings.Contains(s, "event: message_start") || !strings.Contains(s, "content_block_delta") || !strings.Contains(s, "message_stop") {
		t.Fatalf("translate stream missing events:\n%s", s)
	}
}
