package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Redis         RedisConfig         `yaml:"redis"`
	Auth          AuthConfig          `yaml:"auth"`
	ModelList     []ModelAlias        `yaml:"model_list"`
	Routing       RoutingConfig       `yaml:"routing"`
	RateLimiting  RateLimitConfig     `yaml:"rate_limiting"`
	Logging       LoggingConfig       `yaml:"logging"`
	Callbacks     []CallbackConfig    `yaml:"callbacks,omitempty"`
	ResponseCache ResponseCacheConfig `yaml:"response_cache"`
	Guardrails    GuardrailsConfig    `yaml:"guardrails"`
	SmartRoute    SmartRouteConfig    `yaml:"smart_route"`
	SemanticCache SemanticCacheConfig `yaml:"semantic_cache"`
	Plugins       PluginsConfig       `yaml:"plugins"`
	Recording     RecordingConfig     `yaml:"recording"`
	Privacy       PrivacyConfig       `yaml:"privacy"`
	Receipts      ReceiptsConfig      `yaml:"receipts"`
	Policy        PolicyConfig        `yaml:"policy"`
	Metrics       MetricsConfig       `yaml:"metrics"`
	OpenAICompat  OpenAICompatConfig  `yaml:"openai_compat"`
}

// MetricsConfig controls the Prometheus /metrics scrape endpoint exposed
// alongside the gateway's HTTP API. Defaults: enabled at /metrics.
// Disable for compliance-locked environments where any unauthenticated
// endpoint is forbidden; in that case use the callback exporters
// (datadog/otel_otlp) instead.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Path    string `yaml:"path" json:"path"`

	// OTLP push of metrics. Independent of Prometheus pull so an
	// operator can run both side-by-side or pick one. Endpoint is the
	// collector address (host:port for grpc, full URL for http);
	// leaving it blank disables the OTLP path.
	OTLP OTLPMetricsConfig `yaml:"otlp" json:"otlp"`
}

// OTLPMetricsConfig configures the optional OTLP metric exporter that
// pushes the same metric names as /metrics to any OTel-compatible
// backend (Honeycomb, New Relic, Tempo+Mimir, Datadog OTLP intake,
// Grafana Cloud OTLP).
type OTLPMetricsConfig struct {
	Endpoint string            `yaml:"endpoint" json:"endpoint"`
	Protocol string            `yaml:"protocol" json:"protocol"` // "http" (default) or "grpc"
	Headers  map[string]string `yaml:"headers" json:"headers,omitempty"`
	Insecure bool              `yaml:"insecure" json:"insecure,omitempty"`
	Interval time.Duration     `yaml:"interval" json:"interval,omitempty"`
}

// PolicyConfig controls the embedded policy engine (pillar 3b).
// Rules are evaluated on every inference call and can deny, cap
// tokens, pin providers/regions, or require tags.
type PolicyConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	// RulesPath points at a YAML/JSON file of Rule specs. The file is
	// hot-reloaded on SIGHUP.
	RulesPath string `yaml:"rules_path" json:"rules_path"`
}

// RecordingConfig controls the request/response recorder (pillar 1).
// Recordings are what power online eval, A/B replay, and regression testing.
type RecordingConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	// BlobStoreURL picks the backend. Supported: "file:///path", "s3://bucket/prefix",
	// "memory://" (tests only). Default: file://$TMPDIR/gateway-llm-recordings.
	BlobStoreURL string `yaml:"blob_store_url" json:"blob_store_url"`
	// Sample is the fraction of requests to record even when the caller did
	// not set x_gateway_llm_record=true. 0..1; default 0.
	Sample float64 `yaml:"sample" json:"sample"`
	// RedactBeforeRecord runs the privacy engine (pillar 3) before storing
	// the payload so PII never lands in the blob store.
	RedactBeforeRecord bool `yaml:"redact_before_record" json:"redact_before_record"`
}

// PrivacyConfig controls PII redaction / tokenization middleware.
type PrivacyConfig struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`
	// Redactors names the detector stack to use: "email", "phone", "ssn",
	// "credit_card", "api_key", "ip", "person_ner".
	Redactors      []string `yaml:"redactors" json:"redactors"`
	// VaultURL backs tokenized values so they can be rehydrated on the
	// response path. "memory://" for dev; Postgres or redis in prod.
	VaultURL       string   `yaml:"vault_url" json:"vault_url"`
	// Residency is the data residency zone this gateway instance is
	// authorized to egress from (e.g. "eu", "us"). Consumed by OPA policies.
	Residency      string   `yaml:"residency" json:"residency"`
}

// ReceiptsConfig controls cryptographic billing receipts.
type ReceiptsConfig struct {
	Enabled        bool   `yaml:"enabled" json:"enabled"`
	// SigningKeyPath points at an Ed25519 private key (PEM). If missing and
	// Enabled=true, a key is generated on first boot and written here.
	SigningKeyPath string `yaml:"signing_key_path" json:"signing_key_path"`
	// ChainHashPrevious enables hash-chaining of receipts so the log is
	// append-only verifiable.
	ChainHashPrevious bool `yaml:"chain_hash_previous" json:"chain_hash_previous"`
}

// ResponseCacheConfig controls exact-match LLM response caching.
// Deterministic-only requests (temperature==0 and non-streaming) are
// cached by a SHA-256 of their canonical form. TTL of 0 disables.
type ResponseCacheConfig struct {
	Enabled bool          `yaml:"enabled" json:"enabled"`
	TTL     time.Duration `yaml:"ttl" json:"ttl"`
	// SemanticEnabled turns on the optional semantic/similarity tier. It
	// requires EmbeddingModel to be set; if not, exact-match is used.
	SemanticEnabled bool    `yaml:"semantic_enabled" json:"semantic_enabled"`
	EmbeddingModel  string  `yaml:"embedding_model" json:"embedding_model"`
	SimilarityMin   float64 `yaml:"similarity_min" json:"similarity_min"`
}

// GuardrailsConfig configures pre/post-request content safety checks.
type GuardrailsConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// RedactPII enables best-effort regex-based redaction of emails,
	// credit card numbers, and US SSN formats in prompts before they
	// leave the gateway.
	RedactPII bool `yaml:"redact_pii" json:"redact_pii"`
	// BlockPromptInjection enables a heuristic scan for common prompt
	// injection triggers (e.g. "ignore previous instructions").
	BlockPromptInjection bool `yaml:"block_prompt_injection" json:"block_prompt_injection"`
	// BlockedPatterns are regex patterns that, if matched, cause the
	// request to be rejected with 400.
	BlockedPatterns []string `yaml:"blocked_patterns" json:"blocked_patterns"`
}

// PluginsConfig enables the generic pre/post hook pipeline. See the
// plugins package for the hook contract.
type PluginsConfig struct {
	Enabled bool           `yaml:"enabled" json:"enabled"`
	List    []PluginConfig `yaml:"list" json:"list"`
	// Wasm holds sandboxed WebAssembly plugins that run under wazero.
	// These coexist with compile-in plugins and are the recommended
	// distribution path for third-party extensions.
	Wasm []WasmPluginConfig `yaml:"wasm,omitempty" json:"wasm,omitempty"`
}

// WasmPluginConfig points at a .wasm module implementing the gateway-llm
// plugin ABI. Per-hook timeout defaults to 250ms.
type WasmPluginConfig struct {
	Name        string         `yaml:"name" json:"name"`
	Path        string         `yaml:"path" json:"path"`
	Options     map[string]any `yaml:"options,omitempty" json:"options,omitempty"`
	CallTimeout time.Duration  `yaml:"call_timeout,omitempty" json:"call_timeout,omitempty"`
}

// PluginConfig describes a single plugin. When Path is empty, the
// plugin must be compile-time registered via plugins.Register(); when
// set, the file is dlopen'd via Go's `plugin` package.
type PluginConfig struct {
	Name    string         `yaml:"name" json:"name"`
	Path    string         `yaml:"path,omitempty" json:"path,omitempty"`
	Options map[string]any `yaml:"options,omitempty" json:"options,omitempty"`
}

// SemanticCacheConfig controls the semantic (vector-similarity) LLM
// response cache. When disabled the feature is a total no-op.
//
// Embedder selects the vector strategy:
//   - "openai" (default) calls the OpenAI embeddings API for high-quality
//     vectors. Requires an API key via EmbedderAPIKey, EmbedderAPIKeyEnv,
//     or a stored OpenAI provider credential.
//   - "hash" uses a zero-dependency hashed bag-of-words; fast but only
//     suitable for demos and testing.
type SemanticCacheConfig struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	SimilarityMin float64       `yaml:"similarity_min" json:"similarity_min"`
	MaxEntries    int           `yaml:"max_entries" json:"max_entries"`
	TTL           time.Duration `yaml:"ttl" json:"ttl"`
	Dim           int           `yaml:"dim" json:"dim"`

	Embedder         string `yaml:"embedder" json:"embedder"`
	EmbedderModel    string `yaml:"embedder_model" json:"embedder_model"`
	EmbedderAPIKey   string `yaml:"embedder_api_key" json:"-"`
	EmbedderAPIKeyEnv string `yaml:"embedder_api_key_env" json:"embedder_api_key_env"`
}

// SmartRouteConfig controls the autonomous cost/quality optimizer.
type SmartRouteConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Tiers maps a complexity bucket (simple, medium, complex) to a
	// model alias that should handle it. When unset the feature is a
	// no-op even if Enabled=true.
	Tiers map[string]string `yaml:"tiers" json:"tiers"`
	// ShadowPercent enables side-by-side evaluation: for this percent
	// of requests, after serving the chosen model we also kick off a
	// background call to a challenger and log quality deltas.
	ShadowPercent int `yaml:"shadow_percent" json:"shadow_percent"`
	// ShadowChallenger is the model alias to run alongside the primary
	// during shadow evaluations.
	ShadowChallenger string `yaml:"shadow_challenger" json:"shadow_challenger"`
	// ShadowMaxConcurrent bounds how many shadow runs can be in flight
	// at once, protecting upstream quotas.
	ShadowMaxConcurrent int `yaml:"shadow_max_concurrent" json:"shadow_max_concurrent"`
	// MLModelPath points to a JSON model file produced by
	// `gateway-llm train`. When set and loadable the ML classifier
	// replaces the rule-based scorer.
	MLModelPath string `yaml:"ml_model_path" json:"ml_model_path"`
	// SavingsCutPct is the operator's share of savings recorded into
	// daily_savings.our_cut_usd. Used purely for billing rollups; it
	// does NOT affect per-request math. Range [0,1); default 0.20.
	SavingsCutPct float64 `yaml:"savings_cut_pct" json:"savings_cut_pct"`
}

// OpenAICompatConfig controls the generic OpenAI passthrough layer.
// When enabled, any /v1/* path that does not match a typed Gateway-LLM
// handler is forwarded to an upstream OpenAI-compatible provider using
// the deployment resolved from DefaultModelAlias. This lets Cursor,
// the OpenAI SDK, and other clients use files, assistants, batches,
// fine-tuning, vector stores, and future OpenAI endpoints without
// requiring typed handler support for each one.
type OpenAICompatConfig struct {
	Enabled           bool     `yaml:"enabled" json:"enabled"`
	DefaultModelAlias string   `yaml:"default_model_alias" json:"default_model_alias"`
	AllowedPrefixes   []string `yaml:"allowed_prefixes" json:"allowed_prefixes"`
	BlockedPrefixes   []string `yaml:"blocked_prefixes" json:"blocked_prefixes"`
}

// DefaultAllowedPrefixes returns the official OpenAI endpoint families
// that the passthrough should forward.
func DefaultAllowedPrefixes() []string {
	return []string{
		"/v1/files",
		"/v1/uploads",
		"/v1/batches",
		"/v1/fine_tuning",
		"/v1/assistants",
		"/v1/threads",
		"/v1/vector_stores",
		"/v1/images/edits",
		"/v1/images/variations",
		"/v1/audio/translations",
		"/v1/responses/",
		"/v1/organization",
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/completions",
		"/v1/images/generations",
		"/v1/audio/speech",
		"/v1/audio/transcriptions",
		"/v1/moderations",
		"/v1/models",
		"/v1/responses",
	}
}

// DefaultBlockedPrefixes returns Gateway-owned paths that must never be
// forwarded to an upstream provider.
func DefaultBlockedPrefixes() []string {
	return []string{
		"/v1/management",
		"/v1/metrics",
		"/v1/receipts",
		"/v1/recordings",
		"/v1/replay",
		"/v1/eval",
		"/v1/feedback",
		"/v1/audit",
	}
}

type CallbackConfig struct {
	Type        string            `yaml:"type" json:"type"`
	Endpoint    string            `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Protocol    string            `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	ServiceName string            `yaml:"service_name,omitempty" json:"service_name,omitempty"`
	APIKeyEnv   string            `yaml:"api_key_env,omitempty" json:"api_key_env,omitempty"`
	SpaceKeyEnv string            `yaml:"space_key_env,omitempty" json:"space_key_env,omitempty"`
	Site        string            `yaml:"site,omitempty" json:"site,omitempty"`
	Project     string            `yaml:"project,omitempty" json:"project,omitempty"`
	ModelID     string            `yaml:"model_id,omitempty" json:"model_id,omitempty"`
	Method      string            `yaml:"method,omitempty" json:"method,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty" json:"-"`
	Timeout     time.Duration     `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Langfuse
	PublicKeyEnv string `yaml:"public_key_env,omitempty" json:"public_key_env,omitempty"`
	SecretKeyEnv string `yaml:"secret_key_env,omitempty" json:"secret_key_env,omitempty"`

	// Splunk HEC
	TokenEnv string `yaml:"token_env,omitempty" json:"token_env,omitempty"`
	Index    string `yaml:"index,omitempty" json:"index,omitempty"`
	Source   string `yaml:"source,omitempty" json:"source,omitempty"`

	// Slack: only forward events whose computed cost crosses this floor.
	MinCostUSD float64 `yaml:"min_cost_usd,omitempty" json:"min_cost_usd,omitempty"`

	// Datadog StatsD/UDP: when StatsdAddr is set the Datadog callback
	// emits gateway-llm.* custom metrics over DogStatsD in addition to
	// the existing log intake. Tags are appended verbatim to every
	// metric, useful for env / region labels.
	StatsdAddr string   `yaml:"statsd_addr,omitempty" json:"statsd_addr,omitempty"`
	StatsdTags []string `yaml:"statsd_tags,omitempty" json:"statsd_tags,omitempty"`
}

type ServerConfig struct {
	Port                    int           `yaml:"port"`
	ReadTimeout             time.Duration `yaml:"read_timeout"`
	WriteTimeout            time.Duration `yaml:"write_timeout"`
	GracefulShutdownTimeout time.Duration `yaml:"graceful_shutdown_timeout"`
}

type DatabaseConfig struct {
	URL            string `yaml:"url"`
	MaxConnections int    `yaml:"max_connections"`
}

type RedisConfig struct {
	URL string `yaml:"url"`
}

type AuthConfig struct {
	MasterKey string `yaml:"master_key"`
}

type ModelAlias struct {
	ModelAlias  string       `yaml:"model_alias"`
	Deployments []Deployment `yaml:"deployments"`
}

type Deployment struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	APIKeyEnv  string `yaml:"api_key_env"`
	APIBase    string `yaml:"api_base,omitempty"`
	Priority   int    `yaml:"priority,omitempty"`
}

type RoutingConfig struct {
	Strategy        string        `yaml:"strategy" json:"strategy"`
	Retries         int           `yaml:"retries" json:"retries"`
	RetryDelay      time.Duration `yaml:"retry_delay" json:"retry_delay_ms"`
	FallbackEnabled bool          `yaml:"fallback_enabled" json:"fallback_enabled"`
}

func (r RoutingConfig) MarshalJSON() ([]byte, error) {
	type alias struct {
		Strategy        string `json:"strategy"`
		Retries         int    `json:"retries"`
		RetryDelayMS    int64  `json:"retry_delay_ms"`
		FallbackEnabled bool   `json:"fallback_enabled"`
	}
	return json.Marshal(alias{
		Strategy:        r.Strategy,
		Retries:         r.Retries,
		RetryDelayMS:    r.RetryDelay.Milliseconds(),
		FallbackEnabled: r.FallbackEnabled,
	})
}

type RateLimitConfig struct {
	DefaultRPM int           `yaml:"default_rpm" json:"default_rpm"`
	DefaultTPM int           `yaml:"default_tpm" json:"default_tpm"`
	Window     time.Duration `yaml:"window" json:"window_seconds"`
}

func (r RateLimitConfig) MarshalJSON() ([]byte, error) {
	type alias struct {
		DefaultRPM    int   `json:"default_rpm"`
		DefaultTPM    int   `json:"default_tpm"`
		WindowSeconds int64 `json:"window_seconds"`
	}
	return json.Marshal(alias{
		DefaultRPM:    r.DefaultRPM,
		DefaultTPM:    r.DefaultTPM,
		WindowSeconds: int64(r.Window.Seconds()),
	})
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}`)

func substituteEnvVars(data []byte) []byte {
	return envVarRegex.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := envVarRegex.FindSubmatch(match)[1]
		if val, ok := os.LookupEnv(string(varName)); ok {
			return []byte(val)
		}
		return match
	})
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	data = substituteEnvVars(data)

	cfg := &Config{
		Server: ServerConfig{
			Port:                    8080,
			ReadTimeout:             30 * time.Second,
			WriteTimeout:            120 * time.Second,
			GracefulShutdownTimeout: 30 * time.Second,
		},
		Database: DatabaseConfig{
			MaxConnections: 25,
		},
		Routing: RoutingConfig{
			Strategy:        "round-robin",
			Retries:         2,
			RetryDelay:      500 * time.Millisecond,
			FallbackEnabled: true,
		},
		RateLimiting: RateLimitConfig{
			DefaultRPM: 60,
			DefaultTPM: 100000,
			Window:     time.Minute,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		ResponseCache: ResponseCacheConfig{
			Enabled:       false,
			TTL:           10 * time.Minute,
			SimilarityMin: 0.92,
		},
		Guardrails: GuardrailsConfig{
			Enabled: false,
		},
		SmartRoute: SmartRouteConfig{
			Enabled: false,
		},
		SemanticCache: SemanticCacheConfig{
			Enabled:          true,
			SimilarityMin:    0.92,
			MaxEntries:       256,
			TTL:              30 * time.Minute,
			Dim:              256,
			Embedder:         "openai",
			EmbedderModel:    "text-embedding-3-small",
			EmbedderAPIKeyEnv: "OPENAI_API_KEY",
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
		OpenAICompat: OpenAICompatConfig{
			Enabled:         true,
			AllowedPrefixes: DefaultAllowedPrefixes(),
			BlockedPrefixes: DefaultBlockedPrefixes(),
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if len(cfg.ModelList) == 0 {
		return nil, fmt.Errorf("model_list must contain at least one model alias")
	}

	// LiteLLM env-var alias. A user whose infra already sets LITELLM_MASTER_KEY
	// should be able to point their client at gateway-llm without changing
	// secrets plumbing. GATEWAY_LLM_MASTER_KEY always wins if both are set.
	if cfg.Auth.MasterKey == "" {
		if v := os.Getenv("GATEWAY_LLM_MASTER_KEY"); v != "" {
			cfg.Auth.MasterKey = v
		} else if v := os.Getenv("LITELLM_MASTER_KEY"); v != "" {
			cfg.Auth.MasterKey = v
		}
	}

	return cfg, nil
}
