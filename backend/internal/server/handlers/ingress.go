package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gateway-llm/gateway-llm/internal/ingress/anthropic"
	"github.com/gateway-llm/gateway-llm/internal/ingress/gemini"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

// AnthropicMessages is the /anthropic/v1/messages ingress adapter. It
// reuses the full gateway pipeline (auth, smart routing, privacy,
// recorder, cache, etc.) by re-dispatching to ChatCompletions; only the
// wire format changes on the boundary.
func (h *Handlers) AnthropicMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<22))
	if err != nil {
		writeAnthErr(w, http.StatusBadRequest, "failed to read body")
		return
	}
	parsed, wantStream, err := anthropic.Parse(body)
	if err != nil {
		writeAnthErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.dispatchIngress(w, r, parsed, wantStream, ingressCoders{
		name: anthropic.Name,
		format: func(resp *types.ChatCompletionResponse) ([]byte, string, error) {
			b, err := anthropic.Format(resp)
			return b, "application/json", err
		},
		streamTranslate: func(upstream []byte, model string) []byte {
			return anthropic.TranslateStream(upstream, model)
		},
		errBody: anthropic.FormatError,
	})
}

// GeminiGenerateContent handles both :generateContent and :streamGenerateContent.
// The model + mode are parsed out of the URL path.
func (h *Handlers) GeminiGenerateContent(w http.ResponseWriter, r *http.Request) {
	model, mode, ok := parseGeminiPath(r.URL.Path)
	if !ok {
		writeGemErr(w, http.StatusNotFound, "unrecognized gemini path")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<22))
	if err != nil {
		writeGemErr(w, http.StatusBadRequest, "failed to read body")
		return
	}
	parsed, _, err := gemini.Parse(body, model)
	if err != nil {
		writeGemErr(w, http.StatusBadRequest, err.Error())
		return
	}
	parsed.Stream = mode == "streamGenerateContent"
	h.dispatchIngress(w, r, parsed, parsed.Stream, ingressCoders{
		name: gemini.Name,
		format: func(resp *types.ChatCompletionResponse) ([]byte, string, error) {
			b, err := gemini.Format(resp)
			return b, "application/json", err
		},
		// Gemini streaming SDK expects newline-delimited JSON arrays; a
		// practical adapter emits the full translated JSON blob at once
		// for :streamGenerateContent. Full incremental translation is
		// tracked as an enhancement.
		streamTranslate: func(upstream []byte, model string) []byte {
			// Parse the last chunk and emit as single Gemini blob wrapped
			// in a one-element array, which is the documented JSON mode.
			final := lastSSEData(upstream)
			if len(final) == 0 {
				return []byte("[]")
			}
			var chunk types.ChatCompletionResponse
			if err := json.Unmarshal(final, &chunk); err != nil {
				return []byte("[]")
			}
			b, err := gemini.Format(&chunk)
			if err != nil {
				return []byte("[]")
			}
			return append(append([]byte{'['}, b...), ']')
		},
		errBody: gemini.FormatError,
	})
}

type ingressCoders struct {
	name            string
	format          func(*types.ChatCompletionResponse) ([]byte, string, error)
	streamTranslate func(openaiSSE []byte, model string) []byte
	errBody         func(status int, msg string) []byte
}

// dispatchIngress re-serializes the parsed OpenAI request and delegates
// to ChatCompletions via an internal ResponseRecorder. Afterwards it
// translates the captured response into the ingress's wire format.
//
// This is the simple, correct wiring: we never duplicate the pipeline.
func (h *Handlers) dispatchIngress(
	w http.ResponseWriter,
	r *http.Request,
	parsed *types.ChatCompletionRequest,
	wantStream bool,
	c ingressCoders,
) {
	payload, err := json.Marshal(parsed)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(c.errBody(http.StatusInternalServerError, "encode ingress body: "+err.Error()))
		return
	}

	rec := httptest.NewRecorder()
	inner := r.Clone(r.Context())
	inner.Body = io.NopCloser(bytes.NewReader(payload))
	inner.ContentLength = int64(len(payload))
	inner.Header.Set("Content-Type", "application/json")
	// Tag the ingress so the recorder stamps a non-openai origin.
	inner.Header.Set("X-Gateway-LLM-Ingress", c.name)

	h.ChatCompletions(rec, inner)

	status := rec.Code
	body := rec.Body.Bytes()

	// Propagate telemetry headers (trace id, smart-route decision,
	// savings, etc.) back to the caller's SDK even though we reshape
	// the payload.
	for k, vs := range rec.Header() {
		if strings.EqualFold(k, "Content-Type") {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	if status < 200 || status >= 300 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		msg := "upstream error"
		var m map[string]any
		if err := json.Unmarshal(body, &m); err == nil {
			if e, ok := m["error"].(map[string]any); ok {
				if s, ok := e["message"].(string); ok && s != "" {
					msg = s
				}
			}
		}
		_, _ = w.Write(c.errBody(status, msg))
		return
	}

	if wantStream {
		translated := c.streamTranslate(body, parsed.Model)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(status)
		_, _ = w.Write(translated)
		return
	}

	var chat types.ChatCompletionResponse
	if err := json.Unmarshal(body, &chat); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(c.errBody(http.StatusBadGateway, "invalid upstream json"))
		return
	}
	out, ct, err := c.format(&chat)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(c.errBody(http.StatusInternalServerError, err.Error()))
		return
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

// parseGeminiPath extracts (model, mode) from /v1beta/models/{model}:{mode}.
func parseGeminiPath(p string) (model, mode string, ok bool) {
	const prefix = "/v1beta/models/"
	if !strings.HasPrefix(p, prefix) {
		return "", "", false
	}
	rest := p[len(prefix):]
	i := strings.LastIndex(rest, ":")
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// lastSSEData returns the payload of the last non-[DONE] `data:` line in
// a raw OpenAI SSE buffer. Used when we need to fold a stream into a
// single response envelope for non-SSE-aware ingresses.
func lastSSEData(sse []byte) []byte {
	var last []byte
	for _, line := range bytes.Split(sse, []byte("\n")) {
		trim := bytes.TrimSpace(line)
		if !bytes.HasPrefix(trim, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(trim[len("data:"):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		last = payload
	}
	return last
}

func writeAnthErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(anthropic.FormatError(status, msg))
}

func writeGemErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(gemini.FormatError(status, msg))
}
