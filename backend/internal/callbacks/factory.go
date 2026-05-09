package callbacks

import (
	"os"
	"time"

	"go.uber.org/zap"
)

type CallbackConfig struct {
	Type        string            `yaml:"type"`
	Endpoint    string            `yaml:"endpoint,omitempty"`
	Protocol    string            `yaml:"protocol,omitempty"`
	ServiceName string            `yaml:"service_name,omitempty"`
	APIKeyEnv   string            `yaml:"api_key_env,omitempty"`
	SpaceKeyEnv string            `yaml:"space_key_env,omitempty"`
	Site        string            `yaml:"site,omitempty"`
	Project     string            `yaml:"project,omitempty"`
	ModelID     string            `yaml:"model_id,omitempty"`
	Method      string            `yaml:"method,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty"`
	Timeout     time.Duration     `yaml:"timeout,omitempty"`
	// Langfuse-only fields. Values are substituted from env if they
	// match ${ENV_VAR}.
	PublicKeyEnv string            `yaml:"public_key_env,omitempty"`
	SecretKeyEnv string            `yaml:"secret_key_env,omitempty"`
	// Splunk HEC
	TokenEnv string `yaml:"token_env,omitempty"`
	Index    string `yaml:"index,omitempty"`
	Source   string `yaml:"source,omitempty"`
	// Slack
	MinCostUSD float64 `yaml:"min_cost_usd,omitempty"`
	// Datadog StatsD/UDP
	StatsdAddr string   `yaml:"statsd_addr,omitempty"`
	StatsdTags []string `yaml:"statsd_tags,omitempty"`
}

func NewFromConfig(configs []CallbackConfig, logger *zap.Logger) *Dispatcher {
	var cbs []Callback

	for _, cfg := range configs {
		resolvedHeaders := make(map[string]string)
		for k, v := range cfg.Headers {
			resolvedHeaders[k] = substituteEnv(v)
		}

		switch cfg.Type {
		case "otel":
			cbs = append(cbs, NewOTEL(cfg.Endpoint, cfg.ServiceName, resolvedHeaders))
			logger.Warn("the 'otel' callback uses a hand-rolled OTLP-JSON exporter and is deprecated; switch to type=otel_otlp for the official OpenTelemetry SDK")
			logger.Info("registered callback", zap.String("type", "otel"), zap.String("endpoint", cfg.Endpoint))

		case "otel_otlp":
			cb, err := NewOTLP(cfg.Endpoint, cfg.Protocol, cfg.ServiceName, resolvedHeaders, cfg.Timeout)
			if err != nil {
				logger.Warn("otel_otlp callback init failed", zap.Error(err), zap.String("endpoint", cfg.Endpoint))
			} else {
				cbs = append(cbs, cb)
				logger.Info("registered callback", zap.String("type", "otel_otlp"), zap.String("endpoint", cfg.Endpoint), zap.String("protocol", cfg.Protocol))
			}

		case "webhook":
			name := "webhook"
			if cfg.Endpoint != "" {
				name = "webhook:" + cfg.Endpoint
			}
			cbs = append(cbs, NewWebhook(name, cfg.Endpoint, cfg.Method, resolvedHeaders, cfg.Timeout))
			logger.Info("registered callback", zap.String("type", "webhook"), zap.String("endpoint", cfg.Endpoint))

		case "datadog":
			dd := NewDatadog(cfg.Endpoint, cfg.APIKeyEnv, cfg.Site)
			if cfg.StatsdAddr != "" {
				dd = dd.WithStatsd(cfg.StatsdAddr, cfg.StatsdTags...)
			}
			cbs = append(cbs, dd)
			logger.Info("registered callback",
				zap.String("type", "datadog"),
				zap.String("statsd", cfg.StatsdAddr))

		case "langsmith":
			cbs = append(cbs, NewLangsmith(cfg.Endpoint, cfg.APIKeyEnv, cfg.Project))
			logger.Info("registered callback", zap.String("type", "langsmith"))

		case "arize":
			cbs = append(cbs, NewArize(cfg.APIKeyEnv, cfg.SpaceKeyEnv, cfg.ModelID))
			logger.Info("registered callback", zap.String("type", "arize"))

		case "langfuse":
			pk := substituteEnv(cfg.PublicKeyEnv)
			sk := substituteEnv(cfg.SecretKeyEnv)
			cbs = append(cbs, NewLangfuse(cfg.Endpoint, pk, sk))
			logger.Info("registered callback", zap.String("type", "langfuse"))

		case "helicone":
			cbs = append(cbs, NewHelicone(cfg.Endpoint, substituteEnv(cfg.APIKeyEnv)))
			logger.Info("registered callback", zap.String("type", "helicone"))

		case "splunk":
			cbs = append(cbs, NewSplunk(cfg.Endpoint, substituteEnv(cfg.TokenEnv), cfg.Index, cfg.Source))
			logger.Info("registered callback", zap.String("type", "splunk"))

		case "slack":
			cbs = append(cbs, NewSlack(cfg.Endpoint, cfg.MinCostUSD))
			logger.Info("registered callback", zap.String("type", "slack"))

		default:
			logger.Warn("unknown callback type", zap.String("type", cfg.Type))
		}
	}

	return NewDispatcher(cbs, logger)
}

func substituteEnv(s string) string {
	if len(s) > 3 && s[0] == '$' && s[1] == '{' && s[len(s)-1] == '}' {
		envName := s[2 : len(s)-1]
		if v := os.Getenv(envName); v != "" {
			return v
		}
	}
	return s
}
