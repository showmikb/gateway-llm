// Command mockllm is a minimal OpenAI-compatible responder used as the
// upstream in gateway-llm benchmarks. It exists so our comparative
// numbers isolate proxy overhead from upstream variance.
//
// It responds to POST /v1/chat/completions with a canned 200 OK body after
// a configurable delay; no real model is invoked. Streaming is supported
// so we can measure the gateway's TTFB (time to first byte) overhead.
//
// Usage:
//   mockllm -addr :9090 -delay 50ms
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", ":9090", "listen address")
	delay := flag.Duration("delay", 50*time.Millisecond, "fixed response delay")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(*delay)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		stream, _ := body["stream"].(bool)
		if stream {
			writeStream(w)
			return
		}
		writeJSON(w, 200, chatResponse(body))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	log.Printf("mockllm listening on %s, delay=%s", *addr, *delay)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func writeJSON(w http.ResponseWriter, status int, obj any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(obj)
}

func chatResponse(req map[string]any) map[string]any {
	model, _ := req["model"].(string)
	if model == "" {
		model = "mock-llm"
	}
	return map[string]any{
		"id":      "chatcmpl-mock",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"finish_reason": "stop",
			"message":       map[string]any{"role": "assistant", "content": "ok"},
		}},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 1,
			"total_tokens":      11,
		},
	}
}

func writeStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)
	for _, text := range []string{"ok", ""} {
		chunk := map[string]any{
			"id":      "chatcmpl-mock",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   "mock-llm",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"role": "assistant", "content": text},
			}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.TrimSpace(string(b)))
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
