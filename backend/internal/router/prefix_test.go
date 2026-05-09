package router

import "testing"

func TestSplitLitellmPrefix(t *testing.T) {
	cases := []struct {
		in         string
		wantProv   string
		wantModel  string
		wantOK     bool
	}{
		{"anthropic/claude-3-opus", "anthropic", "claude-3-opus", true},
		{"vertex_ai/gemini-1.5-pro", "gemini", "gemini-1.5-pro", true},
		{"bedrock/anthropic.claude-3-sonnet-20240229-v1:0", "bedrock", "anthropic.claude-3-sonnet-20240229-v1:0", true},
		{"together_ai/meta-llama/Llama-3-70b", "together", "meta-llama/Llama-3-70b", true},
		{"gpt-4o", "", "", false},
		{"unknown-vendor/some-model", "", "", false},
		{"/no-prefix", "", "", false},
	}
	for _, c := range cases {
		p, m, ok := splitLitellmPrefix(c.in)
		if ok != c.wantOK {
			t.Errorf("%s: ok want %v got %v", c.in, c.wantOK, ok)
			continue
		}
		if ok && (p != c.wantProv || m != c.wantModel) {
			t.Errorf("%s: want %s/%s, got %s/%s", c.in, c.wantProv, c.wantModel, p, m)
		}
	}
}
