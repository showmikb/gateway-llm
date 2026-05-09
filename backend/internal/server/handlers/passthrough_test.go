package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/config"
)

func TestIsAllowedPassthrough(t *testing.T) {
	cfg := config.OpenAICompatConfig{
		Enabled:         true,
		AllowedPrefixes: config.DefaultAllowedPrefixes(),
		BlockedPrefixes: config.DefaultBlockedPrefixes(),
	}

	tests := []struct {
		path    string
		allowed bool
	}{
		{"/v1/files", true},
		{"/v1/files/file-abc123", true},
		{"/v1/assistants", true},
		{"/v1/assistants/asst_abc/messages", true},
		{"/v1/threads", true},
		{"/v1/threads/thread_abc/runs", true},
		{"/v1/vector_stores", true},
		{"/v1/batches", true},
		{"/v1/batches/batch_abc", true},
		{"/v1/fine_tuning/jobs", true},
		{"/v1/images/edits", true},
		{"/v1/images/variations", true},
		{"/v1/audio/translations", true},
		{"/v1/uploads", true},
		{"/v1/uploads/upload_abc/parts", true},
		{"/v1/chat/completions", true},
		{"/v1/embeddings", true},
		{"/v1/models", true},
		{"/v1/models/gpt-4o", true},
		{"/v1/responses", true},
		{"/v1/responses/resp_abc/cancel", true},
		{"/v1/responses/resp_abc/input_items", true},

		// Gateway-owned paths must be blocked
		{"/v1/management/keys", false},
		{"/v1/management/deployments", false},
		{"/v1/metrics", false},
		{"/v1/receipts/abc123", false},
		{"/v1/recordings", false},
		{"/v1/recordings/abc123", false},
		{"/v1/replay/runs", false},
		{"/v1/eval/recordings/abc", false},
		{"/v1/feedback", false},
		{"/v1/audit", false},

		// Unknown paths not in allowlist
		{"/v1/something-unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isAllowedPassthrough(tt.path, cfg)
			if got != tt.allowed {
				t.Errorf("isAllowedPassthrough(%q) = %v, want %v", tt.path, got, tt.allowed)
			}
		})
	}
}

func TestIsAllowedPassthrough_EmptyAllowlist(t *testing.T) {
	cfg := config.OpenAICompatConfig{
		Enabled:         true,
		AllowedPrefixes: nil,
		BlockedPrefixes: config.DefaultBlockedPrefixes(),
	}
	if !isAllowedPassthrough("/v1/anything", cfg) {
		t.Error("empty allowlist should allow all non-blocked paths")
	}
	if isAllowedPassthrough("/v1/management/keys", cfg) {
		t.Error("blocked paths should still be blocked with empty allowlist")
	}
}

func TestExtractModelFromBody(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		want        string
	}{
		{
			name:        "json with model",
			body:        `{"model":"gpt-4o","messages":[]}`,
			contentType: "application/json",
			want:        "gpt-4o",
		},
		{
			name:        "json without model",
			body:        `{"purpose":"fine-tune"}`,
			contentType: "application/json",
			want:        "",
		},
		{
			name:        "empty body",
			body:        "",
			contentType: "application/json",
			want:        "",
		},
		{
			name:        "non-json content type",
			body:        `{"model":"gpt-4o"}`,
			contentType: "text/plain",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", "/v1/test", nil)
			r.Header.Set("Content-Type", tt.contentType)
			got := extractModelFromBody([]byte(tt.body), r)
			if got != tt.want {
				t.Errorf("extractModelFromBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteModelInBody(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		target      string
		wantModel   string
	}{
		{
			name:        "rewrite model in json",
			body:        `{"model":"gpt-4o","messages":[]}`,
			contentType: "application/json",
			target:      "gpt-4o-mini",
			wantModel:   "gpt-4o-mini",
		},
		{
			name:        "no model field",
			body:        `{"purpose":"fine-tune"}`,
			contentType: "application/json",
			target:      "gpt-4o-mini",
			wantModel:   "",
		},
		{
			name:        "non-json unchanged",
			body:        `binary data`,
			contentType: "application/octet-stream",
			target:      "gpt-4o-mini",
			wantModel:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", "/v1/test", nil)
			r.Header.Set("Content-Type", tt.contentType)
			result := rewriteModelInBody([]byte(tt.body), r, tt.target)
			if tt.wantModel != "" {
				var parsed map[string]interface{}
				if err := parseJSON(result, &parsed); err != nil {
					t.Fatalf("failed to parse result: %v", err)
				}
				if got, _ := parsed["model"].(string); got != tt.wantModel {
					t.Errorf("model = %q, want %q", got, tt.wantModel)
				}
			}
		})
	}
}

func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
