package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

func TestExtractMessagesFromAlternateFormats(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantLen  int
		wantRole string
		wantText string
	}{
		{
			name:    "standard messages field present - returns nil (caller keeps original)",
			body:    `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`,
			wantLen: 0,
		},
		{
			name:    "null messages - no alternate field - returns nil",
			body:    `{"model":"gpt-4o-mini","messages":null}`,
			wantLen: 0,
		},
		{
			name:    "missing messages - no alternate field - returns nil",
			body:    `{"model":"gpt-4o-mini"}`,
			wantLen: 0,
		},
		{
			name:     "input as string - Responses API style",
			body:     `{"model":"gpt-4o-mini","input":"Tell me a joke"}`,
			wantLen:  1,
			wantRole: "user",
			wantText: "Tell me a joke",
		},
		{
			name:     "input as messages array",
			body:     `{"model":"gpt-4o-mini","input":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`,
			wantLen:  2,
			wantRole: "user",
			wantText: "hello",
		},
		{
			name:     "prompt as string - legacy completions style",
			body:     `{"model":"gpt-4o-mini","prompt":"Say hello"}`,
			wantLen:  1,
			wantRole: "user",
			wantText: "Say hello",
		},
		{
			name:    "input null - returns nil",
			body:    `{"model":"gpt-4o-mini","input":null}`,
			wantLen: 0,
		},
		{
			name:    "input empty string - returns nil",
			body:    `{"model":"gpt-4o-mini","input":""}`,
			wantLen: 0,
		},
		{
			name:    "invalid json - returns nil",
			body:    `not json at all`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMessagesFromAlternateFormats([]byte(tt.body))
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
				return
			}
			if tt.wantLen > 0 {
				if got[0].Role != tt.wantRole {
					t.Errorf("role = %q, want %q", got[0].Role, tt.wantRole)
				}
				var text string
				if err := json.Unmarshal(got[0].Content, &text); err != nil {
					t.Fatalf("content unmarshal: %v", err)
				}
				if text != tt.wantText {
					t.Errorf("content = %q, want %q", text, tt.wantText)
				}
			}
		})
	}
}

func TestNormalizeChatMessageContent(t *testing.T) {
	tests := []struct {
		name         string
		messages     []types.ChatMessage
		wantRewrites int
		wantContents []string
	}{
		{
			name: "string content - no rewrite",
			messages: []types.ChatMessage{
				{Role: "user", Content: json.RawMessage(`"hello"`)},
			},
			wantRewrites: 0,
			wantContents: []string{`"hello"`},
		},
		{
			name: "input_text part - rewritten to text",
			messages: []types.ChatMessage{
				{Role: "user", Content: json.RawMessage(`[{"type":"input_text","text":"hello"}]`)},
			},
			wantRewrites: 1,
			wantContents: []string{`[{"text":"hello","type":"text"}]`},
		},
		{
			name: "output_text part - rewritten to text (assistant reply in Responses API)",
			messages: []types.ChatMessage{
				{Role: "assistant", Content: json.RawMessage(`[{"type":"output_text","text":"hello back"}]`)},
			},
			wantRewrites: 1,
			wantContents: []string{`[{"text":"hello back","type":"text"}]`},
		},
		{
			name: "Cursor-style: text + input_text mix across messages",
			messages: []types.ChatMessage{
				{Role: "system", Content: json.RawMessage(`"You are helpful"`)},
				{Role: "user", Content: json.RawMessage(`[{"type":"input_text","text":"first"}]`)},
				{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"answer"}]`)},
				{Role: "user", Content: json.RawMessage(`[{"type":"input_text","text":"follow up"}]`)},
			},
			wantRewrites: 2,
			wantContents: []string{
				`"You are helpful"`,
				`[{"text":"first","type":"text"}]`,
				`[{"type":"text","text":"answer"}]`,
				`[{"text":"follow up","type":"text"}]`,
			},
		},
		{
			name: "input_image string url - rewritten to image_url object",
			messages: []types.ChatMessage{
				{Role: "user", Content: json.RawMessage(`[{"type":"input_image","image_url":"https://example.com/x.png"}]`)},
			},
			wantRewrites: 1,
		},
		{
			name: "already correct - no rewrite",
			messages: []types.ChatMessage{
				{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"hello"}]`)},
			},
			wantRewrites: 0,
			wantContents: []string{`[{"type":"text","text":"hello"}]`},
		},
		{
			name: "empty content - no rewrite",
			messages: []types.ChatMessage{
				{Role: "user", Content: nil},
			},
			wantRewrites: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeChatMessageContent(tt.messages)
			if got != tt.wantRewrites {
				t.Errorf("rewrites = %d, want %d", got, tt.wantRewrites)
			}
			if tt.wantContents != nil {
				for i, want := range tt.wantContents {
					if string(tt.messages[i].Content) != want {
						t.Errorf("messages[%d].Content = %s, want %s", i, string(tt.messages[i].Content), want)
					}
				}
			}
			// All rewritten messages must have an upstream-valid type
			for i, m := range tt.messages {
				if len(m.Content) == 0 || !strings.HasPrefix(strings.TrimSpace(string(m.Content)), "[") {
					continue
				}
				var parts []map[string]interface{}
				if err := json.Unmarshal(m.Content, &parts); err != nil {
					continue
				}
				for j, p := range parts {
					t2, _ := p["type"].(string)
					if t2 == "input_text" || t2 == "input_image" {
						t.Errorf("messages[%d].content[%d].type still %q after normalize", i, j, t2)
					}
				}
			}
		})
	}
}

func TestNormalizeChatMessages(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantRewrites int
		wantLen      int
		assert       func(t *testing.T, msgs []types.ChatMessage)
	}{
		{
			name:         "no messages field - returns nil",
			body:         `{"model":"gpt-4o-mini"}`,
			wantRewrites: 0,
		},
		{
			name:         "all role-based - returns nil (no rewrites)",
			body:         `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`,
			wantRewrites: 0,
		},
		{
			name:         "function_call item - converts to assistant + tool_calls",
			body:         `{"messages":[{"role":"user","content":"weather?"},{"type":"function_call","name":"get_weather","arguments":"{\"loc\":\"sf\"}","call_id":"call_123"}]}`,
			wantRewrites: 1,
			wantLen:      2,
			assert: func(t *testing.T, msgs []types.ChatMessage) {
				if msgs[1].Role != "assistant" {
					t.Errorf("msg[1].role = %q, want assistant", msgs[1].Role)
				}
				if len(msgs[1].ToolCalls) != 1 {
					t.Fatalf("msg[1].tool_calls len = %d, want 1", len(msgs[1].ToolCalls))
				}
				tc := msgs[1].ToolCalls[0]
				if tc.ID != "call_123" || tc.Type != "function" || tc.Function.Name != "get_weather" {
					t.Errorf("tool_call = %+v, expected get_weather/call_123", tc)
				}
				if tc.Function.Arguments != `{"loc":"sf"}` {
					t.Errorf("tool_call args = %q", tc.Function.Arguments)
				}
			},
		},
		{
			name:         "function_call_output item - converts to tool message",
			body:         `{"messages":[{"role":"user","content":"weather?"},{"type":"function_call","name":"get_weather","arguments":"{}","call_id":"call_123"},{"type":"function_call_output","call_id":"call_123","output":"sunny"}]}`,
			wantRewrites: 2,
			wantLen:      3,
			assert: func(t *testing.T, msgs []types.ChatMessage) {
				if msgs[2].Role != "tool" {
					t.Errorf("msg[2].role = %q, want tool", msgs[2].Role)
				}
				if msgs[2].ToolCallID != "call_123" {
					t.Errorf("msg[2].tool_call_id = %q, want call_123", msgs[2].ToolCallID)
				}
				var content string
				if err := json.Unmarshal(msgs[2].Content, &content); err != nil {
					t.Fatalf("unmarshal content: %v", err)
				}
				if content != "sunny" {
					t.Errorf("msg[2].content = %q, want sunny", content)
				}
			},
		},
		{
			name:         "reasoning item - dropped",
			body:         `{"messages":[{"role":"user","content":"hi"},{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking..."}]}]}`,
			wantRewrites: 1,
			wantLen:      1,
			assert: func(t *testing.T, msgs []types.ChatMessage) {
				if msgs[0].Role != "user" {
					t.Errorf("msg[0].role = %q, want user", msgs[0].Role)
				}
			},
		},
		{
			name:         "function_call missing name - dropped",
			body:         `{"messages":[{"role":"user","content":"hi"},{"type":"function_call","call_id":"x"}]}`,
			wantRewrites: 1,
			wantLen:      1,
		},
		{
			name:         "Cursor input-array: role-less items inside input field",
			body:         `{"user":"u","model":"gpt-4o-mini","input":[{"role":"system","content":"sys"},{"role":"user","content":[{"type":"input_text","text":"q"}]},{"type":"function_call","name":"f","arguments":"{}","call_id":"c1"},{"type":"function_call_output","call_id":"c1","output":"r"}]}`,
			wantRewrites: 2,
			wantLen:      4,
			assert: func(t *testing.T, msgs []types.ChatMessage) {
				roles := []string{msgs[0].Role, msgs[1].Role, msgs[2].Role, msgs[3].Role}
				want := []string{"system", "user", "assistant", "tool"}
				for i := range roles {
					if roles[i] != want[i] {
						t.Errorf("msg[%d].role = %q, want %q", i, roles[i], want[i])
					}
				}
			},
		},
		{
			name:         "mixed Cursor sequence: user, function_call, function_call_output, reasoning, user",
			body:         `{"messages":[{"role":"user","content":[{"type":"input_text","text":"q1"}]},{"type":"function_call","name":"f","arguments":"{}","call_id":"c1"},{"type":"function_call_output","call_id":"c1","output":"r1"},{"type":"reasoning","summary":[]},{"role":"user","content":[{"type":"input_text","text":"q2"}]}]}`,
			wantRewrites: 3,
			wantLen:      4,
			assert: func(t *testing.T, msgs []types.ChatMessage) {
				roles := []string{msgs[0].Role, msgs[1].Role, msgs[2].Role, msgs[3].Role}
				want := []string{"user", "assistant", "tool", "user"}
				for i := range roles {
					if roles[i] != want[i] {
						t.Errorf("msg[%d].role = %q, want %q", i, roles[i], want[i])
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rewrites := normalizeChatMessages([]byte(tt.body))
			if rewrites != tt.wantRewrites {
				t.Errorf("rewrites = %d, want %d", rewrites, tt.wantRewrites)
			}
			if tt.wantRewrites == 0 {
				if got != nil {
					t.Errorf("expected nil, got %d msgs", len(got))
				}
				return
			}
			if len(got) != tt.wantLen {
				t.Fatalf("len(got) = %d, want %d", len(got), tt.wantLen)
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
			for i, m := range got {
				if m.Role == "" {
					t.Errorf("msg[%d] still has empty role", i)
				}
			}
		})
	}
}

func TestNormalizeChatTools(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantRewrites int
		wantNames    []string
	}{
		{
			name:         "no tools field - returns nil",
			body:         `{"model":"gpt-4o-mini","messages":[]}`,
			wantRewrites: 0,
		},
		{
			name:         "already-nested function tool - returns nil (no rewrite)",
			body:         `{"tools":[{"type":"function","function":{"name":"search","description":"d","parameters":{}}}]}`,
			wantRewrites: 0,
		},
		{
			name:         "Cursor-style flat function tool - lifts into function",
			body:         `{"tools":[{"type":"function","name":"edit_file","description":"Edit a file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]}`,
			wantRewrites: 1,
			wantNames:    []string{"edit_file"},
		},
		{
			name:         "mixed: nested + flat",
			body:         `{"tools":[{"type":"function","function":{"name":"a","description":"d","parameters":{}}},{"type":"function","name":"b","description":"d","parameters":{}}]}`,
			wantRewrites: 1,
			wantNames:    []string{"a", "b"},
		},
		{
			name:         "flat with empty function object - lifts top-level fields",
			body:         `{"tools":[{"type":"function","function":{},"name":"recover","description":"d","parameters":{}}]}`,
			wantRewrites: 1,
			wantNames:    []string{"recover"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rewrites := normalizeChatTools([]byte(tt.body))
			if rewrites != tt.wantRewrites {
				t.Errorf("rewrites = %d, want %d", rewrites, tt.wantRewrites)
			}
			if tt.wantRewrites == 0 {
				if got != nil {
					t.Errorf("expected nil result, got %d tools", len(got))
				}
				return
			}
			if len(got) != len(tt.wantNames) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if got[i].Function.Name != want {
					t.Errorf("tool[%d].function.name = %q, want %q", i, got[i].Function.Name, want)
				}
				if got[i].Type != "function" {
					t.Errorf("tool[%d].type = %q, want %q", i, got[i].Type, "function")
				}
			}
		})
	}
}

func TestRepairChatToolTranscript(t *testing.T) {
	toolCall := func(id string) types.ChatMessage {
		return types.ChatMessage{
			Role:    "assistant",
			Content: nil,
			ToolCalls: []types.ToolCall{{
				ID:   id,
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      "get_weather",
					Arguments: "{}",
				},
			}},
		}
	}
	toolCallMany := func(ids ...string) types.ChatMessage {
		msg := types.ChatMessage{Role: "assistant"}
		for _, id := range ids {
			msg.ToolCalls = append(msg.ToolCalls, types.ToolCall{
				ID:   id,
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      "tool_" + id,
					Arguments: "{}",
				},
			})
		}
		return msg
	}
	toolResult := func(id, text string) types.ChatMessage {
		content, _ := json.Marshal(text)
		return types.ChatMessage{Role: "tool", ToolCallID: id, Content: content}
	}
	user := func(text string) types.ChatMessage {
		content, _ := json.Marshal(text)
		return types.ChatMessage{Role: "user", Content: content}
	}
	assistantText := func(text string) types.ChatMessage {
		content, _ := json.Marshal(text)
		return types.ChatMessage{Role: "assistant", Content: content}
	}

	tests := []struct {
		name         string
		messages     []types.ChatMessage
		wantRewrites int
		wantRoles    []string
		wantCallIDs  []string
	}{
		{
			name:         "complete assistant tool call is preserved",
			messages:     []types.ChatMessage{user("weather?"), toolCall("call_1"), toolResult("call_1", "sunny"), user("thanks")},
			wantRewrites: 0,
		},
		{
			name:         "assistant tool call without output is dropped",
			messages:     []types.ChatMessage{user("weather?"), toolCall("call_1"), user("next")},
			wantRewrites: 1,
			wantRoles:    []string{"user", "user"},
		},
		{
			name:         "assistant tool call followed by user before output is dropped",
			messages:     []types.ChatMessage{user("weather?"), toolCall("call_1"), user("next"), toolResult("call_1", "late")},
			wantRewrites: 2,
			wantRoles:    []string{"user", "user"},
		},
		{
			name:         "multiple tool calls preserved only when all outputs are adjacent",
			messages:     []types.ChatMessage{toolCallMany("a", "b"), toolResult("a", "A"), toolResult("b", "B"), assistantText("done")},
			wantRewrites: 0,
		},
		{
			name:         "multiple tool calls with missing output are dropped together",
			messages:     []types.ChatMessage{user("x"), toolCallMany("a", "b"), toolResult("a", "A"), user("next")},
			wantRewrites: 2,
			wantRoles:    []string{"user", "user"},
		},
		{
			name:         "stray tool message is dropped",
			messages:     []types.ChatMessage{user("x"), toolResult("orphan", "ignored"), user("y")},
			wantRewrites: 1,
			wantRoles:    []string{"user", "user"},
		},
		{
			name:         "extra adjacent stray tool message is dropped while complete pair is preserved",
			messages:     []types.ChatMessage{toolCall("call_1"), toolResult("call_1", "ok"), toolResult("orphan", "ignored"), user("next")},
			wantRewrites: 1,
			wantRoles:    []string{"assistant", "tool", "user"},
			wantCallIDs:  []string{"", "call_1", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rewrites := repairChatToolTranscript(tt.messages)
			if rewrites != tt.wantRewrites {
				t.Errorf("rewrites = %d, want %d", rewrites, tt.wantRewrites)
			}
			if tt.wantRewrites == 0 {
				if got != nil {
					t.Errorf("expected nil result when no rewrite needed, got %d messages", len(got))
				}
				return
			}
			if len(got) != len(tt.wantRoles) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(tt.wantRoles))
			}
			for i, want := range tt.wantRoles {
				if got[i].Role != want {
					t.Errorf("got[%d].Role = %q, want %q", i, got[i].Role, want)
				}
			}
			for i, want := range tt.wantCallIDs {
				if got[i].ToolCallID != want {
					t.Errorf("got[%d].ToolCallID = %q, want %q", i, got[i].ToolCallID, want)
				}
			}
			assertValidToolTranscript(t, got)
		})
	}
}

func TestRepairChatToolTranscriptAfterCursorInputNormalization(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"do a task"}]},{"type":"function_call","name":"edit_file","arguments":"{}","call_id":"call_missing"},{"role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	msgs, rewrites := normalizeChatMessages(body)
	if rewrites == 0 {
		t.Fatal("expected normalizeChatMessages to rewrite Cursor function_call item")
	}
	normalizeChatMessageContent(msgs)
	repaired, repairRewrites := repairChatToolTranscript(msgs)
	if repairRewrites == 0 {
		t.Fatal("expected repairChatToolTranscript to drop unresolved tool call")
	}
	if len(repaired) != 2 {
		t.Fatalf("len(repaired) = %d, want 2", len(repaired))
	}
	for i, msg := range repaired {
		if msg.Role != "user" {
			t.Errorf("repaired[%d].Role = %q, want user", i, msg.Role)
		}
	}
	assertValidToolTranscript(t, repaired)
}

func assertValidToolTranscript(t *testing.T, messages []types.ChatMessage) {
	t.Helper()
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "tool" {
			t.Fatalf("stray tool message at index %d", i)
		}
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		required := map[string]struct{}{}
		for _, tc := range msg.ToolCalls {
			required[tc.ID] = struct{}{}
		}
		j := i + 1
		for j < len(messages) && messages[j].Role == "tool" {
			delete(required, messages[j].ToolCallID)
			j++
		}
		if len(required) > 0 {
			t.Fatalf("assistant tool_calls at index %d not fully satisfied: %#v", i, required)
		}
		i = j - 1
	}
}

func TestTruncateBytes(t *testing.T) {
	long := make([]byte, 100)
	for i := range long {
		long[i] = 'x'
	}
	got := truncateBytes(long, 10)
	if len(got) != 10 {
		t.Errorf("len = %d, want 10", len(got))
	}
	short := []byte("hi")
	got = truncateBytes(short, 10)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}
