package receipt

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	s := New(priv, false)
	r := &Receipt{
		TraceID:       "tr-1",
		OrgID:         "org-1",
		Alias:         "gpt-4o",
		Provider:      "openai",
		ProviderModel: "gpt-4o",
		ReqHash:       "abc",
		RespHash:      "def",
		PromptTokens:  10,
		OutputTokens:  20,
		TotalTokens:   30,
		CostUSD:       0.0042,
		Timestamp:     time.Unix(1700000000, 0).UTC(),
	}
	out, err := s.Sign(r)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if out.Signature == "" || out.PublicKeyID == "" || out.ID == "" {
		t.Fatalf("missing fields: %+v", out)
	}
	if err := Verify(pub, out); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// Tamper with cost and expect failure.
	out.CostUSD = 99.99
	if err := Verify(pub, out); err == nil {
		t.Fatalf("tampered receipt should not verify")
	}
}

func TestChained(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s := New(priv, true)
	r1, err := s.Sign(&Receipt{OrgID: "o", Alias: "a", Provider: "p", ProviderModel: "m", ReqHash: "1", RespHash: "1"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Sign(&Receipt{OrgID: "o", Alias: "a", Provider: "p", ProviderModel: "m", ReqHash: "2", RespHash: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.PrevReceiptID != r1.ID {
		t.Fatalf("chain link broken: %s != %s", r2.PrevReceiptID, r1.ID)
	}
	if r2.PrevHash == "" {
		t.Fatalf("prev hash empty")
	}
}

func TestLoadOrGenerateKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "key.b64")
	k1, err := LoadOrGenerateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	k2, err := LoadOrGenerateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) != string(k2) {
		t.Fatalf("reloaded key differs")
	}
}
