// Package receipt implements Pillar 3's cryptographic billing receipts.
// Every response produces a canonical JSON record:
//
//   { req_hash, resp_hash, tokens, cost_usd, model, ts, prev_receipt_hash,
//     sig, public_key_id }
//
// signed with Ed25519 and (optionally) hash-chained per-org so auditors
// can detect tampering offline. Verification needs only the org's
// public key, which is published at /.well-known/gateway-llm-receipts.json.
//
// This package owns signing, verification, and key persistence. Storage
// of emitted receipts is handled by `db/receipts.go`; this package is
// storage-agnostic.
package receipt

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Receipt is the public, verifiable envelope.
type Receipt struct {
	ID             string    `json:"id"`
	TraceID        string    `json:"trace_id,omitempty"`
	OrgID          string    `json:"org_id,omitempty"`
	APIKeyID       string    `json:"api_key_id,omitempty"`
	Alias          string    `json:"alias"`
	Provider       string    `json:"provider"`
	ProviderModel  string    `json:"provider_model"`
	Region         string    `json:"region,omitempty"`
	Timestamp      time.Time `json:"ts"`
	ReqHash        string    `json:"req_hash"`
	RespHash       string    `json:"resp_hash"`
	PromptTokens   int       `json:"prompt_tokens"`
	OutputTokens   int       `json:"completion_tokens"`
	TotalTokens    int       `json:"total_tokens"`
	CostUSD        float64   `json:"cost_usd"`
	PrevReceiptID  string    `json:"prev_receipt_id,omitempty"`
	PrevHash       string    `json:"prev_hash,omitempty"`
	PublicKeyID    string    `json:"public_key_id"`
	Signature      string    `json:"sig"` // base64 Ed25519 over canonical bytes

	// canonical returns the bytes that were actually signed. Exposed
	// via CanonicalBytes below so verifiers can reconstruct them
	// without depending on Go's JSON field ordering.
}

// Signer holds the private key and a running hash chain per-org.
// Concurrent-safe.
type Signer struct {
	mu        sync.Mutex
	priv      ed25519.PrivateKey
	pub       ed25519.PublicKey
	keyID     string
	chainLast map[string]chainTail // orgID -> last id+hash
	chained   bool
}

type chainTail struct {
	ID   string
	Hash string
}

// New returns a Signer that uses the supplied key pair. If chained is
// true, each receipt's PrevReceiptID / PrevHash is populated with the
// previous receipt in the same org.
func New(priv ed25519.PrivateKey, chained bool) *Signer {
	pub := priv.Public().(ed25519.PublicKey)
	keyID := hex.EncodeToString(sha256.New().Sum(pub))[:16]
	return &Signer{priv: priv, pub: pub, keyID: keyID, chainLast: make(map[string]chainTail), chained: chained}
}

// PublicKey returns the raw Ed25519 public key. Handlers publish this
// at /.well-known/gateway-llm-receipts.json for in-browser verification
// via WebCrypto.
func (s *Signer) PublicKey() ed25519.PublicKey { return s.pub }

// PrivateKey returns the raw Ed25519 private key. Used by the operator
// price catalog handlers and the savings ledger writer to produce
// secondary signatures (the catalog is signed with the same key as
// receipts so verifiers only need one pubkey).
func (s *Signer) PrivateKey() ed25519.PrivateKey { return s.priv }

// KeyID returns the stable short id of the public key.
func (s *Signer) KeyID() string { return s.keyID }

// Sign fills in ID, PrevReceipt*, PublicKeyID, and Signature fields on
// rcpt and returns it. Caller is expected to have already set the
// content fields (hashes, tokens, cost, etc.).
func (s *Signer) Sign(rcpt *Receipt) (*Receipt, error) {
	if rcpt == nil {
		return nil, errors.New("nil receipt")
	}
	if rcpt.Timestamp.IsZero() {
		rcpt.Timestamp = time.Now().UTC()
	}
	if rcpt.ID == "" {
		rcpt.ID = uuid.NewString()
	}
	rcpt.PublicKeyID = s.keyID

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.chained {
		tail, ok := s.chainLast[rcpt.OrgID]
		if ok {
			rcpt.PrevReceiptID = tail.ID
			rcpt.PrevHash = tail.Hash
		}
	}

	body, err := CanonicalBytes(rcpt)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(s.priv, body)
	rcpt.Signature = base64.StdEncoding.EncodeToString(sig)

	if s.chained {
		h := sha256.Sum256(body)
		s.chainLast[rcpt.OrgID] = chainTail{ID: rcpt.ID, Hash: hex.EncodeToString(h[:])}
	}
	return rcpt, nil
}

// Verify returns nil iff sig in rcpt matches pub over the canonical
// bytes of the receipt.
func Verify(pub ed25519.PublicKey, rcpt *Receipt) error {
	if rcpt == nil {
		return errors.New("nil receipt")
	}
	sig, err := base64.StdEncoding.DecodeString(rcpt.Signature)
	if err != nil {
		return fmt.Errorf("decode sig: %w", err)
	}
	body, err := CanonicalBytes(rcpt)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, body, sig) {
		return errors.New("ed25519: signature mismatch")
	}
	return nil
}

// CanonicalBytes returns the deterministic JSON that is actually signed.
// Signature is explicitly zeroed for determinism. Field order is fixed
// via an ordered struct rather than Go's map iteration.
func CanonicalBytes(r *Receipt) ([]byte, error) {
	type canonical struct {
		ID             string    `json:"id"`
		TraceID        string    `json:"trace_id,omitempty"`
		OrgID          string    `json:"org_id,omitempty"`
		APIKeyID       string    `json:"api_key_id,omitempty"`
		Alias          string    `json:"alias"`
		Provider       string    `json:"provider"`
		ProviderModel  string    `json:"provider_model"`
		Region         string    `json:"region,omitempty"`
		Timestamp      time.Time `json:"ts"`
		ReqHash        string    `json:"req_hash"`
		RespHash       string    `json:"resp_hash"`
		PromptTokens   int       `json:"prompt_tokens"`
		OutputTokens   int       `json:"completion_tokens"`
		TotalTokens    int       `json:"total_tokens"`
		CostUSD        float64   `json:"cost_usd"`
		PrevReceiptID  string    `json:"prev_receipt_id,omitempty"`
		PrevHash       string    `json:"prev_hash,omitempty"`
		PublicKeyID    string    `json:"public_key_id"`
	}
	c := canonical{
		ID:            r.ID,
		TraceID:       r.TraceID,
		OrgID:         r.OrgID,
		APIKeyID:      r.APIKeyID,
		Alias:         r.Alias,
		Provider:      r.Provider,
		ProviderModel: r.ProviderModel,
		Region:        r.Region,
		Timestamp:     r.Timestamp.UTC(),
		ReqHash:       r.ReqHash,
		RespHash:      r.RespHash,
		PromptTokens:  r.PromptTokens,
		OutputTokens:  r.OutputTokens,
		TotalTokens:   r.TotalTokens,
		CostUSD:       r.CostUSD,
		PrevReceiptID: r.PrevReceiptID,
		PrevHash:      r.PrevHash,
		PublicKeyID:   r.PublicKeyID,
	}
	return json.Marshal(c)
}

// HashBytes returns a hex sha256 of raw input. Handy when building
// req_hash / resp_hash from marshaled canonical payloads.
func HashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// LoadOrGenerateKey reads an Ed25519 private key from path; if the
// file does not exist it generates one and writes it with mode 0600.
// The file is a single base64 line of the 64-byte seed+public bytes.
func LoadOrGenerateKey(path string) (ed25519.PrivateKey, error) {
	if path == "" {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		return priv, err
	}
	if data, err := os.ReadFile(path); err == nil {
		raw, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode key %s: %w", path, err)
		}
		if len(raw) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid key size in %s: %d", path, len(raw))
		}
		return ed25519.PrivateKey(raw), nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o700)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		return nil, err
	}
	return priv, nil
}
