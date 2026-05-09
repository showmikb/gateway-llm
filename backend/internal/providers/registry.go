package providers

import "fmt"

// ResponsesBridgeFactory adapts a ChatProvider into a ResponsesProvider.
// It is injected from the bridge package at startup to avoid an import
// cycle between providers and providers/bridge.
type ResponsesBridgeFactory func(ChatProvider) ResponsesProvider

type Registry struct {
	providers      map[string]Provider
	bridgeFactory  ResponsesBridgeFactory
	responsesCache map[string]ResponsesProvider
}

func NewRegistry() *Registry {
	return &Registry{
		providers:      make(map[string]Provider),
		responsesCache: make(map[string]ResponsesProvider),
	}
}

// SetResponsesBridge wires a factory that adapts ChatProviders lacking a
// native Responses implementation to the Responses API. Callers typically
// pass bridge.NewResponsesBridge during server bootstrap.
func (r *Registry) SetResponsesBridge(f ResponsesBridgeFactory) {
	r.bridgeFactory = f
	r.responsesCache = make(map[string]ResponsesProvider)
}

func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
}

// List returns all registered provider names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}
	return p, nil
}

func (r *Registry) GetChat(name string) (ChatProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	cp, ok := p.(ChatProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support chat", name)
	}
	return cp, nil
}

func (r *Registry) GetEmbeddings(name string) (EmbeddingsProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	ep, ok := p.(EmbeddingsProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support embeddings", name)
	}
	return ep, nil
}

func (r *Registry) GetCompletions(name string) (CompletionsProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	cp, ok := p.(CompletionsProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support completions", name)
	}
	return cp, nil
}

func (r *Registry) GetImages(name string) (ImageProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	ip, ok := p.(ImageProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support images", name)
	}
	return ip, nil
}

func (r *Registry) GetAudio(name string) (AudioProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	ap, ok := p.(AudioProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support audio", name)
	}
	return ap, nil
}

func (r *Registry) GetModeration(name string) (ModerationProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	mp, ok := p.(ModerationProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support moderations", name)
	}
	return mp, nil
}

func (r *Registry) GetResponses(name string) (ResponsesProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	if rp, ok := p.(ResponsesProvider); ok {
		return rp, nil
	}
	if cached, ok := r.responsesCache[name]; ok {
		return cached, nil
	}
	if r.bridgeFactory == nil {
		return nil, fmt.Errorf("provider %q does not support responses API", name)
	}
	cp, ok := p.(ChatProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support responses API", name)
	}
	bridged := r.bridgeFactory(cp)
	r.responsesCache[name] = bridged
	return bridged, nil
}

func (r *Registry) GetPassthrough(name string) (PassthroughProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	pp, ok := p.(PassthroughProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support passthrough", name)
	}
	return pp, nil
}

func HasCapability(p Provider, cap Capability) bool {
	for _, c := range p.Capabilities() {
		if c == cap {
			return true
		}
	}
	return false
}
