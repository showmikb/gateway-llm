package ir

import (
	"encoding/json"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

func TestRoundTripSimpleText(t *testing.T) {
	src := &types.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []types.ChatMessage{{Role: "user", Content: []byte(`"hello"`)}},
	}
	got := ToOpenAI(FromOpenAI(src))
	if got.Model != "gpt-4o" || len(got.Messages) != 1 {
		t.Fatalf("lost shape: %#v", got)
	}
	var text string
	if err := json.Unmarshal(got.Messages[0].Content, &text); err != nil || text != "hello" {
		t.Fatalf("content roundtrip broken: err=%v text=%q", err, text)
	}
}

func TestRoundTripMultimodal(t *testing.T) {
	content := `[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"http://x/y.png","detail":"high"}}]`
	src := &types.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []types.ChatMessage{{Role: "user", Content: []byte(content)}},
	}
	irReq := FromOpenAI(src)
	if len(irReq.Messages[0].ContentParts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(irReq.Messages[0].ContentParts))
	}
	if irReq.Messages[0].ContentParts[0].Text != "describe" {
		t.Errorf("text lost: %q", irReq.Messages[0].ContentParts[0].Text)
	}
	if irReq.Messages[0].ContentParts[1].ImageURL == nil ||
		irReq.Messages[0].ContentParts[1].ImageURL.URL != "http://x/y.png" {
		t.Errorf("image url lost: %#v", irReq.Messages[0].ContentParts[1].ImageURL)
	}

	// Round-trip to OpenAI and ensure it decodes back to the same parts.
	out := ToOpenAI(irReq)
	var parts []map[string]any
	if err := json.Unmarshal(out.Messages[0].Content, &parts); err != nil {
		t.Fatalf("output not array: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts after round trip, got %d", len(parts))
	}
}

func TestToolCallRoundTrip(t *testing.T) {
	src := &types.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []types.ChatMessage{{
			Role: "assistant",
			Content: []byte(`""`),
			ToolCalls: []types.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"city":"SF"}`,
				},
			}},
		}},
	}
	irReq := FromOpenAI(src)
	if len(irReq.Messages[0].ToolCalls) != 1 || irReq.Messages[0].ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool call lost: %#v", irReq.Messages)
	}
	out := ToOpenAI(irReq)
	if out.Messages[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("tool call id lost: %#v", out.Messages[0].ToolCalls)
	}
}

func TestPassthroughFields(t *testing.T) {
	src := &types.ChatCompletionRequest{
		Model:          "gpt-4o",
		Messages:       []types.ChatMessage{{Role: "user", Content: []byte(`"hi"`)}},
		Metadata:       map[string]any{"customer": "acme"},
		Tags:           []string{"prod", "team-a"},
		TraceID:        "abc-123",
		GenerationName: "summarizer",
		RouterHint:     "smart",
	}
	ir := FromOpenAI(src)
	if ir.Metadata["customer"] != "acme" || len(ir.Tags) != 2 || ir.TraceID != "abc-123" ||
		ir.GenerationName != "summarizer" || ir.RouterHint != "smart" {
		t.Errorf("passthrough fields lost: %#v", ir)
	}
}
