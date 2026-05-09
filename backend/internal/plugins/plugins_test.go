package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

type stampPlugin struct {
	stamp string
}

func (s *stampPlugin) Name() string { return "stamp-" + s.stamp }

func (s *stampPlugin) BeforeChat(_ context.Context, req *types.ChatCompletionRequest) (*ShortCircuit, error) {
	req.Messages = append(req.Messages, types.ChatMessage{Role: "system", Content: []byte(`"` + s.stamp + `"`)})
	return nil, nil
}

func (s *stampPlugin) AfterChat(_ context.Context, _ *types.ChatCompletionRequest, resp *types.ChatCompletionResponse) error {
	resp.ID = resp.ID + "-" + s.stamp
	return nil
}

type shortCircuitPlugin struct{}

func (s *shortCircuitPlugin) Name() string { return "short" }
func (s *shortCircuitPlugin) BeforeChat(_ context.Context, _ *types.ChatCompletionRequest) (*ShortCircuit, error) {
	return &ShortCircuit{Status: 418, Response: &types.ChatCompletionResponse{ID: "teapot"}}, nil
}

type errorPlugin struct{}

func (e *errorPlugin) Name() string { return "err" }
func (e *errorPlugin) BeforeChat(_ context.Context, _ *types.ChatCompletionRequest) (*ShortCircuit, error) {
	return nil, errors.New("boom")
}

func TestBeforeChatAppliesHooksInOrder(t *testing.T) {
	Register(&stampPlugin{stamp: "a"})
	Register(&stampPlugin{stamp: "b"})
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, "stamp-a")
		delete(registry, "stamp-b")
		registryMu.Unlock()
	})

	mgr, errs := NewManager(config.PluginsConfig{
		Enabled: true,
		List:    []config.PluginConfig{{Name: "stamp-a"}, {Name: "stamp-b"}},
	}, nil)
	if len(errs) > 0 {
		t.Fatalf("unexpected load errors: %v", errs)
	}

	req := &types.ChatCompletionRequest{}
	sc, err := mgr.RunBeforeChat(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sc != nil {
		t.Fatalf("expected no short-circuit")
	}
	if len(req.Messages) != 2 || string(req.Messages[0].Content) != `"a"` || string(req.Messages[1].Content) != `"b"` {
		t.Fatalf("hooks did not run in order: %+v", req.Messages)
	}
}

func TestShortCircuitHalts(t *testing.T) {
	Register(&shortCircuitPlugin{})
	Register(&stampPlugin{stamp: "after"})
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, "short")
		delete(registry, "stamp-after")
		registryMu.Unlock()
	})

	mgr, _ := NewManager(config.PluginsConfig{
		Enabled: true,
		List:    []config.PluginConfig{{Name: "short"}, {Name: "stamp-after"}},
	}, nil)
	req := &types.ChatCompletionRequest{}
	sc, err := mgr.RunBeforeChat(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sc == nil || sc.Response.ID != "teapot" || sc.Status != 418 {
		t.Fatalf("expected short-circuit: %+v", sc)
	}
	if len(req.Messages) != 0 {
		t.Fatalf("later plugin must not have run: %+v", req.Messages)
	}
}

func TestBeforeChatErrorSurfaces(t *testing.T) {
	Register(&errorPlugin{})
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, "err")
		registryMu.Unlock()
	})
	mgr, _ := NewManager(config.PluginsConfig{
		Enabled: true,
		List:    []config.PluginConfig{{Name: "err"}},
	}, nil)
	if _, err := mgr.RunBeforeChat(context.Background(), &types.ChatCompletionRequest{}); err == nil {
		t.Fatal("expected error")
	}
}
