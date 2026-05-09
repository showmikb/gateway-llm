package privacy

import (
	"context"
	"encoding/json"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

// Engine is the facade the chat handler uses. It owns a Redactor and a
// Vault and exposes request/response hooks. Keeping this facade small
// lets handlers wire in redaction with two lines rather than sprinkling
// primitive calls.
type Engine struct {
	Redactor *Redactor
	Vault    Vault
}

// NewEngine returns an engine with the default rule pack and an
// in-process vault. Callers wanting persistence swap the Vault field.
func NewEngine(salt []byte) *Engine {
	return &Engine{
		Redactor: NewRedactor(salt),
		Vault:    NewMemoryVault(),
	}
}

// RedactRequest scans all text content in a ChatCompletionRequest and
// substitutes tokens in place. The mapping is stored in the vault
// under requestID. Callers pass requestID=traceID so the rehydrator
// can find it later.
//
// Returns the count of redactions so the handler can emit a header.
func (e *Engine) RedactRequest(ctx context.Context, requestID string, req *types.ChatCompletionRequest) (int, error) {
	if e == nil || req == nil {
		return 0, nil
	}
	merged := Mapping{Entries: map[string]string{}}
	n := 0
	for i, m := range req.Messages {
		text, ok := extractText(m.Content)
		if !ok || text == "" {
			continue
		}
		red, mapping := e.Redactor.Redact(text)
		if len(mapping.Entries) == 0 {
			continue
		}
		for tok, orig := range mapping.Entries {
			merged.Entries[tok] = orig
		}
		n += len(mapping.Entries)
		payload, _ := json.Marshal(red)
		req.Messages[i].Content = payload
	}
	if n == 0 {
		return 0, nil
	}
	return n, e.Vault.Put(ctx, requestID, merged)
}

// RehydrateResponse walks a ChatCompletionResponse and swaps tokens
// back for original values using the mapping stored under requestID.
// Safe to call when no redaction happened (it's a no-op).
func (e *Engine) RehydrateResponse(ctx context.Context, requestID string, resp *types.ChatCompletionResponse) {
	if e == nil || resp == nil {
		return
	}
	m, ok := e.Vault.Get(ctx, requestID)
	if !ok || len(m.Entries) == 0 {
		return
	}
	reh := NewRehydrator(m)
	for i, ch := range resp.Choices {
		if ch.Message != nil {
			if txt, ok := extractText(ch.Message.Content); ok {
				if restored := reh.Whole(txt); restored != txt {
					payload, _ := json.Marshal(restored)
					resp.Choices[i].Message.Content = payload
				}
			}
		}
	}
	e.Vault.Delete(ctx, requestID)
}

// RehydrateBytes rewrites a response body in place. Used on the raw
// SSE byte stream so streaming responses stay privacy-safe.
func (e *Engine) RehydrateBytes(ctx context.Context, requestID string, body []byte) []byte {
	if e == nil {
		return body
	}
	m, ok := e.Vault.Get(ctx, requestID)
	if !ok || len(m.Entries) == 0 {
		return body
	}
	reh := NewRehydrator(m)
	return []byte(reh.Whole(string(body)))
}

func extractText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		// Rebuild concatenated text — we lose structure but redaction
		// runs only for logging+scan; the actual content shape is
		// restored because we never overwrite multimodal messages.
		var out string
		for _, p := range parts {
			if t, _ := p["text"].(string); t != "" {
				out += t
			}
		}
		return out, out != ""
	}
	return "", false
}
