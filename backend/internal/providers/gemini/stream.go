package gemini

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

// StreamChatResponse parses Gemini SSE (data lines with JSON GeminiResponse objects) into OpenAI chat completion chunks.
func (p *GeminiProvider) StreamChatResponse(ctx context.Context, resp *http.Response) (<-chan providers.StreamEvent, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("gemini stream: %s: %s", resp.Status, string(body))
	}

	ch := make(chan providers.StreamEvent, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		sc := bufio.NewScanner(resp.Body)
		// Gemini JSON payloads can be large.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		streamID := "chatcmpl-" + uuid.NewString()
		created := time.Now().Unix()
		var prevText string

		for sc.Scan() {
			select {
			case <-ctx.Done():
				ch <- providers.StreamEvent{Error: ctx.Err()}
				return
			default:
			}

			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "" || data == "[DONE]" {
				continue
			}

			var gr GeminiResponse
			if err := json.Unmarshal([]byte(data), &gr); err != nil {
				ch <- providers.StreamEvent{Error: fmt.Errorf("gemini stream: decode chunk: %w", err)}
				return
			}

			fullText := extractCandidateText(&gr)
			deltaStr := streamingTextDelta(prevText, fullText)
			prevText = streamingAdvancePrev(prevText, fullText, deltaStr)

			var finishPtr *string
			if len(gr.Candidates) > 0 && gr.Candidates[0].FinishReason != "" {
				finishPtr = mapFinishReason(gr.Candidates[0].FinishReason)
			}

			var usagePtr *types.Usage
			if gr.UsageMetadata != nil {
				usagePtr = geminiUsageToOpenAI(gr.UsageMetadata)
			}

			// Emit a chunk when there is new text, terminal reason, or usage metadata.
			if deltaStr == "" && finishPtr == nil && usagePtr == nil {
				continue
			}

			contentJSON, err := json.Marshal(deltaStr)
			if err != nil {
				ch <- providers.StreamEvent{Error: err}
				return
			}

			chunk := types.ChatCompletionChunk{
				ID:      streamID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   "",
				Choices: []types.Choice{
					{
						Index: 0,
						Delta: &types.ChatMessage{
							Role:    "assistant",
							Content: contentJSON,
						},
						FinishReason: finishPtr,
					},
				},
				Usage: usagePtr,
			}

			payload, err := json.Marshal(chunk)
			if err != nil {
				ch <- providers.StreamEvent{Error: err}
				return
			}
			ch <- providers.StreamEvent{Data: payload}
		}

		if err := sc.Err(); err != nil {
			ch <- providers.StreamEvent{Error: fmt.Errorf("gemini stream: read: %w", err)}
			return
		}

		ch <- providers.StreamEvent{Done: true}
	}()

	return ch, nil
}

func extractCandidateText(gr *GeminiResponse) string {
	if len(gr.Candidates) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range gr.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func streamingTextDelta(prev, full string) string {
	if full == "" {
		return ""
	}
	if strings.HasPrefix(full, prev) {
		return full[len(prev):]
	}
	return full
}

func streamingAdvancePrev(prev, full, delta string) string {
	if full != "" && strings.HasPrefix(full, prev) {
		return full
	}
	return prev + delta
}
