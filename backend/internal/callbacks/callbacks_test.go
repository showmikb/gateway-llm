package callbacks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLangfuseCallback(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got == "" {
			t.Errorf("missing auth header")
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cb := NewLangfuse(srv.URL, "pk", "sk")
	ev := RequestEvent{TraceID: "t1", ModelAlias: "gpt-4o", Timestamp: time.Now(), DurationMS: 120}
	if err := cb.Send(context.Background(), ev); err != nil {
		t.Fatalf("send: %v", err)
	}
	_ = got
}

func TestSlackSkipsBoringEvents(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cb := NewSlack(srv.URL, 1.0) // only above $1
	if err := cb.Send(context.Background(), RequestEvent{Status: 200, CostUSD: 0.01}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("expected 0 calls for cheap successful event, got %d", calls)
	}
	if err := cb.Send(context.Background(), RequestEvent{Status: 500, ErrorMessage: "boom"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call for error, got %d", calls)
	}
}

func TestSplunkRequiresConfig(t *testing.T) {
	cb := NewSplunk("", "", "", "")
	if err := cb.Send(context.Background(), RequestEvent{}); err != nil {
		t.Fatalf("expected no-op, got: %v", err)
	}
}
