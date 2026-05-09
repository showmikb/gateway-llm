// Package wasmplugin runs sandboxed gateway-llm plugins compiled to
// WebAssembly. A plugin is any .wasm module that exports the following
// functions (any subset is fine):
//
//	on_request(ptr, len) -> i64   // called before upstream call
//	on_response(ptr, len) -> i64  // called after upstream call
//	on_stream_chunk(ptr, len) -> i64 // called per SSE chunk
//	on_error(ptr, len) -> i64     // called on provider failure
//	on_route(ptr, len) -> i64     // called during alias resolution
//	on_eval(ptr, len) -> i64      // called during replay scoring
//	alloc(size) -> i32            // WASM side memory allocator
//	dealloc(ptr, size)            // frees above
//
// The low 32 bits of the return value point at result bytes; the high
// 32 bits are the length. A zero return means "no mutation, continue".
//
// Host functions provided:
//
//	gateway_log(level, ptr, len)       // 0=debug,1=info,2=warn,3=error
//	gateway_http_send(ptr, len) -> i64 // JSON outbound HTTP (for webhooks)
//	gateway_kv_get/set                 // per-plugin persistent KV (optional)
//
// Plugins run with no filesystem, no network (unless gateway_http_send
// is enabled) and a per-call CPU/instruction budget. This is the
// "community filter hub" pattern pioneered by Envoy and OPA.
package wasmplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/plugins"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"go.uber.org/zap"
)

// Hook names are stable strings the plugin SDK exposes.
const (
	HookRequest     = "on_request"
	HookResponse    = "on_response"
	HookStreamChunk = "on_stream_chunk"
	HookError       = "on_error"
	HookRoute       = "on_route"
	HookEval        = "on_eval"
)

// Config is supplied per loaded plugin.
type Config struct {
	Name    string
	Path    string
	Options map[string]any
	// CallTimeout bounds a single hook invocation. Default 250ms.
	CallTimeout time.Duration
	// MaxMemoryPages caps linear memory, one page = 64 KiB. Default 64 (4 MiB).
	MaxMemoryPages uint32
}

// Host holds the shared wazero runtime and compiled modules.
type Host struct {
	mu       sync.RWMutex
	runtime  wazero.Runtime
	compiled map[string]wazero.CompiledModule
	plugins  map[string]*Plugin
	logger   *zap.Logger
}

// NewHost creates a fresh runtime with WASI imported. The runtime is
// long-lived; modules are instantiated on demand per-hook call so each
// invocation gets a clean linear memory.
func NewHost(ctx context.Context, logger *zap.Logger) *Host {
	cfg := wazero.NewRuntimeConfig().WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	h := &Host{
		runtime:  rt,
		compiled: make(map[string]wazero.CompiledModule),
		plugins:  make(map[string]*Plugin),
		logger:   logger,
	}
	h.registerHostModule(ctx)
	return h
}

func (h *Host) Close(ctx context.Context) error { return h.runtime.Close(ctx) }

// Load compiles a .wasm file and registers it under cfg.Name. Calling
// Load twice with the same name replaces the previous module.
func (h *Host) Load(ctx context.Context, cfg Config) (*Plugin, error) {
	if cfg.Name == "" {
		return nil, errors.New("wasmplugin: name required")
	}
	if cfg.Path == "" {
		return nil, errors.New("wasmplugin: path required")
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 250 * time.Millisecond
	}
	if cfg.MaxMemoryPages == 0 {
		cfg.MaxMemoryPages = 64
	}
	data, err := os.ReadFile(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("read wasm %s: %w", cfg.Path, err)
	}
	mod, err := h.runtime.CompileModule(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", cfg.Path, err)
	}
	h.mu.Lock()
	if prev, ok := h.compiled[cfg.Name]; ok {
		_ = prev.Close(ctx)
	}
	h.compiled[cfg.Name] = mod
	p := &Plugin{cfg: cfg, host: h, exports: detectExports(mod)}
	h.plugins[cfg.Name] = p
	h.mu.Unlock()
	if h.logger != nil {
		h.logger.Info("wasm plugin loaded",
			zap.String("name", cfg.Name),
			zap.String("path", cfg.Path),
			zap.Strings("hooks", p.exports))
	}
	return p, nil
}

// Plugin represents one compiled wasm module. It implements
// plugins.PreRequest and plugins.PostResponse so it drops straight into
// the existing plugin pipeline.
type Plugin struct {
	cfg     Config
	host    *Host
	exports []string
}

// Name implements plugins.Plugin.
func (p *Plugin) Name() string { return p.cfg.Name }

// BeforeChat invokes on_request. A non-empty JSON return value of the
// shape {"short_circuit":{"status":200,"response":{...}}} short-circuits
// the pipeline; {"request":{...}} replaces the outbound request.
func (p *Plugin) BeforeChat(ctx context.Context, req *types.ChatCompletionRequest) (*plugins.ShortCircuit, error) {
	if !p.has(HookRequest) {
		return nil, nil
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	out, err := p.call(ctx, HookRequest, body)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	var env struct {
		Request      *types.ChatCompletionRequest `json:"request,omitempty"`
		ShortCircuit *struct {
			Status   int                              `json:"status"`
			Response *types.ChatCompletionResponse    `json:"response"`
		} `json:"short_circuit,omitempty"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("plugin %s: decode on_request: %w", p.cfg.Name, err)
	}
	if env.Request != nil {
		*req = *env.Request
	}
	if env.ShortCircuit != nil && env.ShortCircuit.Response != nil {
		return &plugins.ShortCircuit{
			Status:   env.ShortCircuit.Status,
			Response: env.ShortCircuit.Response,
		}, nil
	}
	return nil, nil
}

// AfterChat invokes on_response. A non-empty JSON return value of the
// shape {"response":{...}} replaces the response body.
func (p *Plugin) AfterChat(ctx context.Context, req *types.ChatCompletionRequest, resp *types.ChatCompletionResponse) error {
	if !p.has(HookResponse) {
		return nil
	}
	body, err := json.Marshal(map[string]any{"request": req, "response": resp})
	if err != nil {
		return err
	}
	out, err := p.call(ctx, HookResponse, body)
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return nil
	}
	var env struct {
		Response *types.ChatCompletionResponse `json:"response,omitempty"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("plugin %s: decode on_response: %w", p.cfg.Name, err)
	}
	if env.Response != nil {
		*resp = *env.Response
	}
	return nil
}

func (p *Plugin) has(hook string) bool {
	for _, e := range p.exports {
		if e == hook {
			return true
		}
	}
	return false
}

// call runs one hook invocation in a fresh module instance.
func (p *Plugin) call(ctx context.Context, hook string, in []byte) ([]byte, error) {
	p.host.mu.RLock()
	mod := p.host.compiled[p.cfg.Name]
	p.host.mu.RUnlock()
	if mod == nil {
		return nil, fmt.Errorf("plugin %s: module gone", p.cfg.Name)
	}

	ctx, cancel := context.WithTimeout(ctx, p.cfg.CallTimeout)
	defer cancel()

	modCfg := wazero.NewModuleConfig().
		WithName(p.cfg.Name+"_"+hook).
		WithStartFunctions() // skip _start
	inst, err := p.host.runtime.InstantiateModule(ctx, mod, modCfg)
	if err != nil {
		return nil, fmt.Errorf("instantiate: %w", err)
	}
	defer inst.Close(ctx)

	// Allocate a buffer inside the plugin, write input, call hook.
	alloc := inst.ExportedFunction("alloc")
	dealloc := inst.ExportedFunction("dealloc")
	fn := inst.ExportedFunction(hook)
	if fn == nil {
		return nil, fmt.Errorf("hook %s not exported", hook)
	}
	mem := inst.Memory()
	if mem == nil {
		return nil, fmt.Errorf("plugin %s: no memory export", p.cfg.Name)
	}

	var ptr uint64
	if alloc != nil && len(in) > 0 {
		res, err := alloc.Call(ctx, uint64(len(in)))
		if err != nil {
			return nil, fmt.Errorf("alloc: %w", err)
		}
		ptr = res[0]
		if !mem.Write(uint32(ptr), in) {
			return nil, errors.New("plugin memory write out of bounds")
		}
	}

	res, err := fn.Call(ctx, ptr, uint64(len(in)))
	if dealloc != nil && ptr != 0 {
		_, _ = dealloc.Call(ctx, ptr, uint64(len(in)))
	}
	if err != nil {
		return nil, fmt.Errorf("hook %s: %w", hook, err)
	}
	if len(res) == 0 || res[0] == 0 {
		return nil, nil
	}
	retPtr := uint32(res[0] >> 32)
	retLen := uint32(res[0] & 0xffff_ffff)
	if retLen == 0 {
		return nil, nil
	}
	buf, ok := mem.Read(retPtr, retLen)
	if !ok {
		return nil, errors.New("plugin memory read out of bounds")
	}
	out := make([]byte, retLen)
	copy(out, buf)
	if dealloc != nil {
		_, _ = dealloc.Call(ctx, uint64(retPtr), uint64(retLen))
	}
	return out, nil
}

// detectExports returns the list of hooks implemented by mod.
func detectExports(mod wazero.CompiledModule) []string {
	known := []string{HookRequest, HookResponse, HookStreamChunk, HookError, HookRoute, HookEval}
	var found []string
	exports := mod.ExportedFunctions()
	for _, h := range known {
		if _, ok := exports[h]; ok {
			found = append(found, h)
		}
	}
	return found
}

// registerHostModule installs the `env` module with the gateway-llm
// host functions (logging is the only one in the v0 alpha; HTTP send and
// KV will follow once we ship an allowlist policy for them).
func (h *Host) registerHostModule(ctx context.Context) {
	_, err := h.runtime.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, level, ptr, ln uint32) {
			mem := m.Memory()
			if mem == nil || ln == 0 {
				return
			}
			buf, ok := mem.Read(ptr, ln)
			if !ok {
				return
			}
			if h.logger == nil {
				return
			}
			msg := string(buf)
			switch level {
			case 0:
				h.logger.Debug("wasm", zap.String("msg", msg))
			case 1:
				h.logger.Info("wasm", zap.String("msg", msg))
			case 2:
				h.logger.Warn("wasm", zap.String("msg", msg))
			case 3:
				h.logger.Error("wasm", zap.String("msg", msg))
			default:
				h.logger.Info("wasm", zap.String("msg", msg))
			}
		}).
		Export("gateway_log").
		Instantiate(ctx)
	if err != nil && h.logger != nil {
		h.logger.Error("wasm host module init failed", zap.Error(err))
	}
}

// Plugin list returns the registered names. Used by /v1/plugins.
func (h *Host) Plugins() []*Plugin {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Plugin, 0, len(h.plugins))
	for _, p := range h.plugins {
		out = append(out, p)
	}
	return out
}
