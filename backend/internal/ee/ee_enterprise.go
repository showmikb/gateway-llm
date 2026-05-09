//go:build ee

// Enterprise build of the open-core gate. This file is only compiled
// into the closed-source EE binary (`-tags ee`). It reads a signed
// license file produced by our license service and exposes the feature
// flags Gateway-LLM checks at runtime.
//
// The signing key lives in `ee/keys.go` (not in OSS). Verification
// failures fail-closed: the customer drops to the Community tier and
// gets a loud log line so they can notice and open a ticket.
package ee

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type Tier string

const (
	TierCommunity  Tier = "community"
	TierEnterprise Tier = "enterprise"
)

type License struct {
	Tier          Tier            `json:"tier"`
	CustomerID    string          `json:"customer_id"`
	ExpiresAtUnix int64           `json:"expires_at_unix"`
	Seats         int             `json:"seats"`
	Features      map[string]bool `json:"features"`
	// Signature is base64 over the canonical bytes of the rest of the
	// struct. Verified by verifyLicense (implementation lives in the
	// private keys.go file that we never publish).
	Signature string `json:"signature"`
}

func Load(path string) (*License, error) {
	if path == "" {
		return &License{Tier: TierCommunity, Features: map[string]bool{}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &License{Tier: TierCommunity, Features: map[string]bool{}}, nil
		}
		return nil, fmt.Errorf("read license: %w", err)
	}
	var l License
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse license: %w", err)
	}
	if err := verifyLicense(&l); err != nil {
		return nil, fmt.Errorf("license signature invalid; falling back to community: %w", err)
	}
	if l.ExpiresAtUnix > 0 && time.Now().Unix() > l.ExpiresAtUnix {
		return nil, errors.New("license expired; falling back to community")
	}
	if l.Features == nil {
		l.Features = map[string]bool{}
	}
	return &l, nil
}

func (l *License) Enabled(feature string) bool {
	if l == nil || l.Tier != TierEnterprise {
		return false
	}
	return l.Features[feature]
}

func (l *License) IsEnterprise() bool { return l != nil && l.Tier == TierEnterprise }
