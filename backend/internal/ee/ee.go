//go:build !ee

// Package ee is the enforcement point for gateway-llm's open-core split.
// The OSS build (no `ee` tag) ships a no-op License and a set of
// feature flags that always report the Community tier. The EE build
// (`-tags ee`, closed source) replaces this file with one that reads
// a signed license file and gates features accordingly.
//
// The rules are:
//
//   1. Every feature is callable from OSS. If a feature happens to be
//      EE-only at the code level, it must degrade gracefully (log a
//      warning and return a sensible zero value) rather than panic.
//   2. The OSS build must *never* import paid-tier code. Gating is
//      done through this package alone.
//   3. EE features that enforce quotas (e.g. seat count) must always
//      fail-closed: an invalid license disables the EE feature, not
//      the whole gateway.
package ee

// Tier labels the active edition.
type Tier string

const (
	TierCommunity  Tier = "community"
	TierEnterprise Tier = "enterprise"
)

// License is the runtime view of the license file. OSS always returns
// the zero value; the EE build fills it from a signed descriptor.
type License struct {
	Tier          Tier
	CustomerID    string
	ExpiresAtUnix int64
	Seats         int
	Features      map[string]bool
}

// Load reads the license at path. OSS ignores path entirely and always
// reports Community.
func Load(_ string) (*License, error) {
	return &License{Tier: TierCommunity, Features: map[string]bool{}}, nil
}

// Enabled reports whether a named EE feature is active. OSS always
// returns false. The list of well-known feature keys is documented in
// `docs/open-core.md` so customers can audit what they're buying.
func (l *License) Enabled(_ string) bool { return false }

// IsEnterprise is a convenience predicate.
func (l *License) IsEnterprise() bool { return l != nil && l.Tier == TierEnterprise }
