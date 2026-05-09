package privacy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/types"
)

func TestRedactor_BasicCategories(t *testing.T) {
	r := NewRedactor([]byte("salt-1"))
	text := "Email john.doe@example.com or call +1-415-555-1234. " +
		"SSN 123-45-6789. IP 192.168.1.1. Card 4242 4242 4242 4242. IBAN GB82WEST12345698765432."
	out, m := r.Redact(text)
	if strings.Contains(out, "john.doe@example.com") {
		t.Fatalf("email leaked: %s", out)
	}
	if strings.Contains(out, "123-45-6789") {
		t.Fatalf("ssn leaked: %s", out)
	}
	if strings.Contains(out, "4242 4242 4242 4242") {
		t.Fatalf("cc leaked: %s", out)
	}
	if len(m.Entries) < 5 {
		t.Fatalf("expected >=5 tokens, got %d:\n%s", len(m.Entries), out)
	}
}

func TestRedactor_Deterministic(t *testing.T) {
	r := NewRedactor([]byte("x"))
	a, _ := r.Redact("email me at foo@bar.com")
	b, _ := r.Redact("previously foo@bar.com was the address")
	if !strings.Contains(a, "<<pii:EMAIL") || !strings.Contains(b, "<<pii:EMAIL") {
		t.Fatalf("missing tokens")
	}
	// Token for foo@bar.com should be identical in both.
	tokA := extractPiiToken(a)
	tokB := extractPiiToken(b)
	if tokA == "" || tokA != tokB {
		t.Fatalf("deterministic token mismatch: %q vs %q", tokA, tokB)
	}
}

func TestRedactor_RejectsInvalidLuhn(t *testing.T) {
	r := NewRedactor(nil)
	// 16 digits that don't pass Luhn.
	out, _ := r.Redact("card: 1234 5678 9012 3456")
	if strings.Contains(out, "<<pii:CC") {
		t.Fatalf("non-luhn should not be redacted: %s", out)
	}
}

func TestRehydrator_Whole(t *testing.T) {
	r := NewRedactor([]byte("salt"))
	in := "email a@b.com and b@c.com"
	red, m := r.Redact(in)
	reh := NewRehydrator(m)
	if got := reh.Whole(red); got != in {
		t.Fatalf("round-trip failed: %q != %q", got, in)
	}
}

func TestEngine_RequestResponseRoundTrip(t *testing.T) {
	e := NewEngine([]byte("e"))
	payload, _ := json.Marshal("Please email john@doe.com with the update.")
	req := &types.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []types.ChatMessage{{Role: "user", Content: payload}},
	}
	ctx := context.Background()
	n, err := e.RedactRequest(ctx, "trace-1", req)
	if err != nil || n < 1 {
		t.Fatalf("redact err=%v n=%d", err, n)
	}
	// After redaction the message body must no longer contain the email.
	raw, _ := extractText(req.Messages[0].Content)
	if strings.Contains(raw, "john@doe.com") {
		t.Fatalf("email leaked past redactor: %s", raw)
	}

	// Simulate a response that echoes the token back (this is what LLMs
	// typically do) and verify rehydration restores the original.
	respMsg := &types.ChatMessage{Role: "assistant"}
	respMsg.Content, _ = json.Marshal("I will email " + findFirstToken(raw) + " now.")
	resp := &types.ChatCompletionResponse{Choices: []types.Choice{{Index: 0, Message: respMsg}}}
	e.RehydrateResponse(ctx, "trace-1", resp)
	got, _ := extractText(resp.Choices[0].Message.Content)
	if !strings.Contains(got, "john@doe.com") {
		t.Fatalf("rehydrator failed: %s", got)
	}
}

func TestStreamWriter_PreservesTokensAcrossChunks(t *testing.T) {
	r := NewRedactor(nil)
	original := "ping me at foo@bar.com soon"
	red, m := r.Redact(original)
	_ = red

	var out strings.Builder
	sw := NewStreamWriter(m, func(p []byte) (int, error) { return out.Write(p) })
	// Split the redacted text halfway through the token.
	tok := firstToken(red)
	mid := strings.Index(red, tok) + len(tok)/2
	sw.Write([]byte(red[:mid]))
	sw.Write([]byte(red[mid:]))
	_ = sw.Flush()
	if got := out.String(); got != original {
		t.Fatalf("streaming rehydration mismatch:\nwant %q\ngot  %q", original, got)
	}
}

// ---- helpers ----

func extractPiiToken(s string) string {
	i := strings.Index(s, "<<pii:")
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], ">>")
	if j < 0 {
		return ""
	}
	return s[i : i+j+2]
}

func findFirstToken(s string) string { return extractPiiToken(s) }

func firstToken(s string) string { return extractPiiToken(s) }
