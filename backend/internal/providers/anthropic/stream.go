package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

type streamEventRaw struct {
	Type string `json:"type"`
}

type messageStartPayload struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Role  string `json:"role"`
	} `json:"message"`
}

type contentBlockStartPayload struct {
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
}

type contentBlockStartBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type contentBlockDeltaPayload struct {
	Type  string          `json:"type"`
	Index int             `json:"index"`
	Delta json.RawMessage `json:"delta"`
}

type textDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type inputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type messageDeltaPayload struct {
	Type  string          `json:"type"`
	Usage json.RawMessage `json:"usage,omitempty"`
	Delta json.RawMessage `json:"delta,omitempty"`
}

type messageDeltaInner struct {
	StopReason *string `json:"stop_reason,omitempty"`
}

type sseState struct {
	id        string
	model     string
	created   int64
	blockType map[int]string
	toolIndex map[int]int
	toolID    map[int]string
	toolName  map[int]string
	nextTool  int
}

func parseAnthropicSSE(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		ch := make(chan providers.StreamEvent, 1)
		ch <- providers.StreamEvent{Error: fmt.Errorf("anthropic stream: HTTP %d: %s", resp.StatusCode, truncateBody(body))}
		close(ch)
		return ch, nil
	}

	out := make(chan providers.StreamEvent, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		st := &sseState{
			blockType: make(map[int]string),
			toolIndex: make(map[int]int),
			toolID:    make(map[int]string),
			toolName:  make(map[int]string),
		}

		sc := bufio.NewScanner(resp.Body)
		const max = 1024 * 1024
		buf := make([]byte, 0, 64*1024)
		sc.Buffer(buf, max)

		var dataLines []byte
		flush := func() {
			if len(dataLines) == 0 {
				return
			}
			payload := bytes.TrimSpace(dataLines)
			dataLines = nil
			if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
				return
			}

			var head streamEventRaw
			if err := json.Unmarshal(payload, &head); err != nil {
				select {
				case out <- providers.StreamEvent{Error: fmt.Errorf("anthropic stream: decode event: %w", err)}:
				case <-ctx.Done():
				}
				return
			}

			events, err := mapAnthropicStreamEvent(st, head.Type, payload)
			if err != nil {
				select {
				case out <- providers.StreamEvent{Error: err}:
				case <-ctx.Done():
				}
				return
			}
			for _, ev := range events {
				if ev == nil {
					continue
				}
				select {
				case out <- *ev:
				case <-ctx.Done():
					return
				}
			}
		}

		for sc.Scan() {
			select {
			case <-ctx.Done():
				out <- providers.StreamEvent{Error: ctx.Err()}
				return
			default:
			}

			line := sc.Bytes()
			if len(line) == 0 {
				flush()
				continue
			}
			if bytes.HasPrefix(line, []byte(":")) {
				continue
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				rest := bytes.TrimSpace(line[len("data:"):])
				if len(dataLines) > 0 {
					dataLines = append(dataLines, '\n')
				}
				dataLines = append(dataLines, rest...)
			}
		}
		flush()

		if err := sc.Err(); err != nil {
			select {
			case out <- providers.StreamEvent{Error: fmt.Errorf("anthropic stream: read: %w", err)}:
			case <-ctx.Done():
			}
			return
		}

		select {
		case out <- providers.StreamEvent{Done: true}:
		case <-ctx.Done():
		}
	}()

	return out, nil
}

func mapAnthropicStreamEvent(st *sseState, eventType string, payload []byte) ([]*providers.StreamEvent, error) {
	switch eventType {
	case "ping":
		return nil, nil

	case "message_start":
		var ms messageStartPayload
		if err := json.Unmarshal(payload, &ms); err != nil {
			return nil, err
		}
		st.id = ms.Message.ID
		st.model = ms.Message.Model
		st.created = time.Now().Unix()

		chunk := &types.ChatCompletionChunk{
			ID:      st.id,
			Object:  "chat.completion.chunk",
			Created: st.created,
			Model:   st.model,
			Choices: []types.Choice{
				{
					Index: 0,
					Delta: &types.ChatMessage{
						Role: "assistant",
					},
				},
			},
		}
		return []*providers.StreamEvent{wrapChunk(chunk)}, nil

	case "content_block_start":
		var cs contentBlockStartPayload
		if err := json.Unmarshal(payload, &cs); err != nil {
			return nil, err
		}
		var blk contentBlockStartBlock
		if err := json.Unmarshal(cs.ContentBlock, &blk); err != nil {
			return nil, err
		}
		st.blockType[cs.Index] = blk.Type

		if blk.Type == "tool_use" {
			ti := st.nextTool
			st.nextTool++
			st.toolIndex[cs.Index] = ti
			st.toolID[cs.Index] = blk.ID
			st.toolName[cs.Index] = blk.Name

			chunk := &types.ChatCompletionChunk{
				ID:      st.id,
				Object:  "chat.completion.chunk",
				Created: st.created,
				Model:   st.model,
				Choices: []types.Choice{
					{
						Index: 0,
						Delta: &types.ChatMessage{
							ToolCalls: []types.ToolCall{
								{
									ID:   blk.ID,
									Type: "function",
									Function: types.ToolCallFunction{
										Name:      blk.Name,
										Arguments: "",
									},
								},
							},
						},
					},
				},
			}
			return []*providers.StreamEvent{wrapChunk(chunk)}, nil
		}

		return nil, nil

	case "content_block_delta":
		var cd contentBlockDeltaPayload
		if err := json.Unmarshal(payload, &cd); err != nil {
			return nil, err
		}

		var td textDelta
		if err := json.Unmarshal(cd.Delta, &td); err == nil && td.Type == "text_delta" {
			raw, _ := json.Marshal(td.Text)
			chunk := &types.ChatCompletionChunk{
				ID:      st.id,
				Object:  "chat.completion.chunk",
				Created: st.created,
				Model:   st.model,
				Choices: []types.Choice{
					{
						Index: 0,
						Delta: &types.ChatMessage{
							Content: raw,
						},
					},
				},
			}
			return []*providers.StreamEvent{wrapChunk(chunk)}, nil
		}

		var ij inputJSONDelta
		if err := json.Unmarshal(cd.Delta, &ij); err == nil && ij.Type == "input_json_delta" {
			toolID := st.toolID[cd.Index]
			name := st.toolName[cd.Index]

			chunk := &types.ChatCompletionChunk{
				ID:      st.id,
				Object:  "chat.completion.chunk",
				Created: st.created,
				Model:   st.model,
				Choices: []types.Choice{
					{
						Index: 0,
						Delta: &types.ChatMessage{
							ToolCalls: []types.ToolCall{
								{
									ID:   toolID,
									Type: "function",
									Function: types.ToolCallFunction{
										Name:      name,
										Arguments: ij.PartialJSON,
									},
								},
							},
						},
					},
				},
			}
			return []*providers.StreamEvent{wrapChunk(chunk)}, nil
		}

		return nil, nil

	case "content_block_stop":
		return nil, nil

	case "message_delta":
		var md messageDeltaPayload
		if err := json.Unmarshal(payload, &md); err != nil {
			return nil, err
		}

		var out []*providers.StreamEvent

		if len(md.Usage) > 0 {
			var u struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}
			if err := json.Unmarshal(md.Usage, &u); err == nil {
				chunk := &types.ChatCompletionChunk{
					ID:      st.id,
					Object:  "chat.completion.chunk",
					Created: st.created,
					Model:   st.model,
					Usage: &types.Usage{
						PromptTokens:     u.InputTokens,
						CompletionTokens: u.OutputTokens,
						TotalTokens:      u.InputTokens + u.OutputTokens,
					},
					Choices: []types.Choice{{Index: 0, Delta: &types.ChatMessage{}}},
				}
				out = append(out, wrapChunk(chunk))
			}
		}

		if len(md.Delta) > 0 {
			var inner messageDeltaInner
			if err := json.Unmarshal(md.Delta, &inner); err == nil && inner.StopReason != nil && *inner.StopReason != "" {
				hasTools := strings.EqualFold(*inner.StopReason, "tool_use")
				fr := mapStopReason(*inner.StopReason, hasTools)
				chunk := &types.ChatCompletionChunk{
					ID:      st.id,
					Object:  "chat.completion.chunk",
					Created: st.created,
					Model:   st.model,
					Choices: []types.Choice{
						{
							Index:        0,
							Delta:        &types.ChatMessage{},
							FinishReason: ptrString(fr),
						},
					},
				}
				out = append(out, wrapChunk(chunk))
			}
		}

		return out, nil

	case "message_stop":
		return nil, nil

	default:
		return nil, nil
	}
}

func wrapChunk(c *types.ChatCompletionChunk) *providers.StreamEvent {
	data, err := json.Marshal(c)
	if err != nil {
		return &providers.StreamEvent{Error: err}
	}
	return &providers.StreamEvent{Data: data}
}
