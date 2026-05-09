package ee

import "testing"

func TestOSSIsCommunity(t *testing.T) {
	l, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if l.IsEnterprise() {
		t.Fatal("OSS build must never report enterprise")
	}
	if l.Enabled("soc2_audit_exports") {
		t.Fatal("OSS build must refuse EE features")
	}
}
