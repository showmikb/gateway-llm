package handlers

import (
	"net/http"
)

func (h *Handlers) APIRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":    "Gateway-LLM",
		"version": "0.1.0",
		"docs":    "/docs",
		"health":  "/health",
		"endpoints": map[string]string{
			"chat_completions": "/v1/chat/completions",
			"completions":      "/v1/completions",
			"embeddings":       "/v1/embeddings",
			"models":           "/v1/models",
			"images":           "/v1/images/generations",
			"audio_speech":     "/v1/audio/speech",
			"audio_transcribe": "/v1/audio/transcriptions",
			"moderations":      "/v1/moderations",
			"responses":        "/v1/responses",
			"management":       "/v1/management/*",
		},
		"openai_passthrough": map[string]string{
			"description": "Any /v1/* path matching an official OpenAI endpoint family that is not listed above is forwarded to the upstream provider. This enables files, assistants, threads, vector stores, batches, fine-tuning, image edits/variations, audio translations, and future OpenAI endpoints without typed handler support.",
			"config":      "openai_compat section in gateway config",
		},
		"cursor_setup": map[string]string{
			"base_url":     "https://api.gateway-llm.com/v1",
			"api_key":      "Your Gateway-LLM API key (sk-gatewayllm-...)",
			"model_select": "Use the exact model alias configured in your Gateway-LLM deployment (e.g. gpt-4o-mini)",
		},
		"auth": "Bearer token required for all /v1/* endpoints. See /docs for details.",
	})
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Gateway-LLM API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    html { box-sizing: border-box; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
    .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: '/docs/openapi.yaml',
      dom_id: '#swagger-ui',
      deepLinking: true,
      presets: [
        SwaggerUIBundle.presets.apis,
        SwaggerUIBundle.SwaggerUIStandalonePreset
      ],
      layout: 'BaseLayout'
    });
  </script>
</body>
</html>`

func (h *Handlers) SwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(swaggerHTML))
}

func ServeOpenAPISpec(specData []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		w.Write(specData)
	}
}
