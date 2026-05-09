// Package plugins provides Gateway-LLM's extension mechanism: a
// config-driven pre-request and post-response hook pipeline. Hooks run
// in the order registered and may mutate requests, short-circuit
// responses, or attach metadata for downstream middleware.
//
// There are two ways to ship a plugin:
//
//  1. Compile-in registration via plugins.Register at init(); this is
//     the reliable path and the only one that works cross-platform.
//  2. Dynamic loading via Go's stdlib `plugin` package on Linux/macOS;
//     enabled via the "path" field in PluginConfig. The plugin .so
//     must export a symbol named `Plugin` satisfying Plugin below. Use
//     this sparingly because Go plugins require exact toolchain match.
//
// Hooks should be cheap: they run synchronously inside the request
// path. Long work should be pushed to background goroutines.
package plugins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"plugin"
	"sync"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"go.uber.org/zap"
)

// Plugin is the root interface a plugin implements. A plugin may satisfy
// PreRequest, PostResponse, or both. Name() is used in config and logs.
type Plugin interface {
	Name() string
}

// PreRequest hooks run before the upstream provider call. Returning an
// error short-circuits the request with a 400; returning a non-nil
// Response short-circuits with that payload (status 200 unless the
// plugin set another via a custom field in its Response).
type PreRequest interface {
	Plugin
	BeforeChat(ctx context.Context, req *types.ChatCompletionRequest) (*ShortCircuit, error)
}

// PostResponse hooks observe the finalized response and may mutate it
// in place (for redaction, rewrites, etc). Returning an error is
// logged but does not fail the request.
type PostResponse interface {
	Plugin
	AfterChat(ctx context.Context, req *types.ChatCompletionRequest, resp *types.ChatCompletionResponse) error
}

// ShortCircuit lets a pre-request plugin return a synthetic response
// (e.g., cached, safety-refusal) without calling upstream.
type ShortCircuit struct {
	Status   int
	Response *types.ChatCompletionResponse
	Headers  http.Header
}

// Manager owns the ordered pipeline of enabled hooks for a gateway
// instance. It is safe for concurrent use.
type Manager struct {
	mu     sync.RWMutex
	pre    []PreRequest
	post   []PostResponse
	byName map[string]Plugin
	logger *zap.Logger
}

// NewManager applies the config, wiring up every enabled plugin and
// returning the populated manager plus any load errors collected.
func NewManager(cfg config.PluginsConfig, logger *zap.Logger) (*Manager, []error) {
	m := &Manager{byName: map[string]Plugin{}, logger: logger}
	var errs []error
	if !cfg.Enabled {
		return m, nil
	}
	for _, pc := range cfg.List {
		p, err := resolve(pc)
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin %q: %w", pc.Name, err))
			continue
		}
		if pre, ok := p.(PreRequest); ok {
			m.pre = append(m.pre, pre)
		}
		if post, ok := p.(PostResponse); ok {
			m.post = append(m.post, post)
		}
		m.byName[p.Name()] = p
		if logger != nil {
			logger.Info("plugin registered", zap.String("name", p.Name()))
		}
	}
	return m, errs
}

// RunBeforeChat executes every registered pre-request hook in order.
// The first hook to return a ShortCircuit wins.
func (m *Manager) RunBeforeChat(ctx context.Context, req *types.ChatCompletionRequest) (*ShortCircuit, error) {
	if m == nil {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, h := range m.pre {
		sc, err := h.BeforeChat(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("pre-hook %s: %w", h.Name(), err)
		}
		if sc != nil {
			return sc, nil
		}
	}
	return nil, nil
}

// RunAfterChat runs post-response hooks; errors are logged but not
// returned so a broken plugin cannot break the response.
func (m *Manager) RunAfterChat(ctx context.Context, req *types.ChatCompletionRequest, resp *types.ChatCompletionResponse) {
	if m == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, h := range m.post {
		if err := h.AfterChat(ctx, req, resp); err != nil && m.logger != nil {
			m.logger.Warn("post-hook error", zap.String("name", h.Name()), zap.Error(err))
		}
	}
}

// Attach registers an already-constructed plugin with the manager. This
// is used by the wasmplugin package: each wasm module is compiled at
// startup and handed back to the manager so it participates in the same
// pre/post pipeline as compile-in plugins.
func (m *Manager) Attach(p Plugin) {
	if m == nil || p == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if pre, ok := p.(PreRequest); ok {
		m.pre = append(m.pre, pre)
	}
	if post, ok := p.(PostResponse); ok {
		m.post = append(m.post, post)
	}
	m.byName[p.Name()] = p
	if m.logger != nil {
		m.logger.Info("plugin attached", zap.String("name", p.Name()))
	}
}

// Has reports whether a plugin with the given name was registered.
func (m *Manager) Has(name string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.byName[name]
	return ok
}

// registry holds compile-time registered plugins. External code drops
// them in via Register() from init() functions.
var (
	registryMu sync.Mutex
	registry   = map[string]Plugin{}
)

// Register is called by plugin init() functions to make a plugin
// discoverable by name. Calling Register twice with the same name
// panics to catch misconfiguration at startup rather than at request
// time.
func Register(p Plugin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[p.Name()]; exists {
		panic("plugins: duplicate registration for " + p.Name())
	}
	registry[p.Name()] = p
}

// Get returns a compile-time registered plugin by name.
func Get(name string) (Plugin, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	p, ok := registry[name]
	return p, ok
}

func resolve(pc config.PluginConfig) (Plugin, error) {
	if pc.Name == "" {
		return nil, errors.New("name is required")
	}
	if pc.Path != "" {
		return loadFromFile(pc)
	}
	p, ok := Get(pc.Name)
	if !ok {
		return nil, fmt.Errorf("plugin not registered; either import the plugin package for its init() or set path")
	}
	if cfgP, ok := p.(Configurable); ok {
		if err := cfgP.Configure(pc.Options); err != nil {
			return nil, fmt.Errorf("configure: %w", err)
		}
	}
	return p, nil
}

// Configurable is an optional interface plugins may implement to
// receive their per-deployment options from YAML.
type Configurable interface {
	Configure(opts map[string]any) error
}

func loadFromFile(pc config.PluginConfig) (Plugin, error) {
	pl, err := plugin.Open(pc.Path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", pc.Path, err)
	}
	sym, err := pl.Lookup("Plugin")
	if err != nil {
		return nil, fmt.Errorf("lookup Plugin symbol: %w", err)
	}
	p, ok := sym.(Plugin)
	if !ok {
		// Some plugins export a pointer to a package-level var.
		if pp, ok2 := sym.(*Plugin); ok2 && pp != nil {
			p = *pp
		} else {
			return nil, errors.New("Plugin symbol does not implement plugins.Plugin")
		}
	}
	if cfgP, ok := p.(Configurable); ok {
		if err := cfgP.Configure(pc.Options); err != nil {
			return nil, fmt.Errorf("configure: %w", err)
		}
	}
	return p, nil
}
