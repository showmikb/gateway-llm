package policy

import "testing"

func TestEngine_RegionPinning(t *testing.T) {
	e := NewEngine()
	allow := true
	e.SetRules("", []Rule{
		{
			Name:    "EU residency",
			When:    []Condition{{ResidencyEquals: "eu"}},
			Allow:   &allow,
			Regions: []string{"eu-west-1", "eu-central-1"},
		},
	})
	d := e.Evaluate(&Input{OrgID: "o", Residency: "eu", Region: "us-east-1", Provider: "openai"})
	if d.Allow {
		t.Fatalf("cross-region egress should be denied, got %+v", d)
	}
	d = e.Evaluate(&Input{OrgID: "o", Residency: "eu", Region: "eu-west-1", Provider: "openai"})
	if !d.Allow {
		t.Fatalf("eu->eu should allow, got %+v", d)
	}
}

func TestEngine_PIIGate(t *testing.T) {
	e := NewEngine()
	e.SetRules("", []Rule{
		{Name: "No PHI to generic providers", When: []Condition{{PIIClassIn: []string{"SSN"}}, {ProviderIn: []string{"openai"}}}, Deny: true, Reason: "SSN blocked for openai"},
	})
	d := e.Evaluate(&Input{PIIClasses: []string{"SSN"}, Provider: "openai"})
	if d.Allow {
		t.Fatalf("expected deny, got %+v", d)
	}
	d = e.Evaluate(&Input{PIIClasses: []string{"EMAIL"}, Provider: "openai"})
	if !d.Allow {
		t.Fatalf("expected allow for email, got %+v", d)
	}
}

func TestEngine_NoRulesAllowsAll(t *testing.T) {
	e := NewEngine()
	d := e.Evaluate(&Input{Provider: "whatever"})
	if !d.Allow {
		t.Fatalf("empty engine must allow, got %+v", d)
	}
}
