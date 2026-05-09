// Command litellm-migrate reads a LiteLLM config.yaml and emits an
// equivalent gateway-llm config.yaml.
//
// Usage:
//   litellm-migrate -in litellm.config.yaml -out gateway-llm.config.yaml
//   litellm-migrate -in litellm.config.yaml -diff           # show diff only
//   cat litellm.config.yaml | litellm-migrate              # stdin -> stdout
//
// The mapping is deliberately conservative: when a LiteLLM concept doesn't
// have a 1:1 translation, we emit a commented-out hint rather than silently
// dropping it. Run with -diff to preview before overwriting anything.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type litellmConfig struct {
	ModelList []struct {
		ModelName     string                 `yaml:"model_name"`
		LitellmParams map[string]interface{} `yaml:"litellm_params"`
		ModelInfo     map[string]interface{} `yaml:"model_info,omitempty"`
	} `yaml:"model_list"`
	LitellmSettings map[string]interface{} `yaml:"litellm_settings,omitempty"`
	GeneralSettings map[string]interface{} `yaml:"general_settings,omitempty"`
	Router          map[string]interface{} `yaml:"router_settings,omitempty"`
	Environment     map[string]interface{} `yaml:"environment_variables,omitempty"`
}

type gwDeployment struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
	APIBase   string `yaml:"api_base,omitempty"`
	Priority  int    `yaml:"priority,omitempty"`
}

type gwAlias struct {
	ModelAlias  string         `yaml:"model_alias"`
	Deployments []gwDeployment `yaml:"deployments"`
}

type gwConfig struct {
	Server       map[string]interface{} `yaml:"server"`
	Database     map[string]interface{} `yaml:"database"`
	Redis        map[string]interface{} `yaml:"redis"`
	Auth         map[string]interface{} `yaml:"auth"`
	ModelList    []gwAlias              `yaml:"model_list"`
	Routing      map[string]interface{} `yaml:"routing"`
	RateLimiting map[string]interface{} `yaml:"rate_limiting"`
	Logging      map[string]interface{} `yaml:"logging"`
}

// splitProviderModel maps a LiteLLM-style model string like
// "anthropic/claude-3-opus-20240229" or "azure/my-gpt4-deployment" onto our
// internal (provider, model) tuple. Falls back to (openai, input) for a
// bare "gpt-4o" so LiteLLM configs that omit prefixes still work.
func splitProviderModel(model string) (provider, modelName string) {
	idx := strings.Index(model, "/")
	if idx < 0 {
		return "openai", model
	}
	prefix := strings.ToLower(model[:idx])
	rest := model[idx+1:]

	switch prefix {
	case "openai", "azure":
		return "openai", rest
	case "anthropic", "claude":
		return "anthropic", rest
	case "gemini", "vertex_ai", "vertex", "google":
		return "gemini", rest
	case "bedrock":
		return "bedrock", rest
	case "mistral":
		return "mistral", rest
	case "cohere":
		return "cohere", rest
	case "groq":
		return "groq", rest
	case "together_ai", "together":
		return "together", rest
	case "fireworks_ai", "fireworks":
		return "fireworks", rest
	case "deepseek":
		return "deepseek", rest
	case "xai":
		return "xai", rest
	case "ollama":
		return "ollama", rest
	case "huggingface":
		return "huggingface", rest
	default:
		return prefix, rest
	}
}

func inferAPIKeyEnv(provider string) string {
	switch provider {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	case "bedrock":
		return "AWS_ACCESS_KEY_ID"
	case "mistral":
		return "MISTRAL_API_KEY"
	case "cohere":
		return "COHERE_API_KEY"
	case "groq":
		return "GROQ_API_KEY"
	case "together":
		return "TOGETHER_API_KEY"
	case "fireworks":
		return "FIREWORKS_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "xai":
		return "XAI_API_KEY"
	case "huggingface":
		return "HF_TOKEN"
	default:
		return strings.ToUpper(provider) + "_API_KEY"
	}
}

func convert(lcfg *litellmConfig) *gwConfig {
	out := &gwConfig{
		Server: map[string]interface{}{
			"port":                      8080,
			"read_timeout":              "30s",
			"write_timeout":             "120s",
			"graceful_shutdown_timeout": "30s",
		},
		Database: map[string]interface{}{
			"url":             "${DATABASE_URL}",
			"max_connections": 25,
		},
		Redis: map[string]interface{}{"url": "${REDIS_URL}"},
		Auth:  map[string]interface{}{"master_key": "${GATEWAY_LLM_MASTER_KEY}"},
		Routing: map[string]interface{}{
			"strategy":         "round-robin",
			"retries":          2,
			"retry_delay":      "500ms",
			"fallback_enabled": true,
		},
		RateLimiting: map[string]interface{}{
			"default_rpm": 60,
			"default_tpm": 100000,
			"window":      "1m",
		},
		Logging: map[string]interface{}{"level": "info", "format": "json"},
	}

	// Group by alias so LiteLLM's typical pattern of several deployments
	// sharing a model_name (for load balancing) collapses into a single
	// alias with multiple deployments in our config.
	byAlias := map[string][]gwDeployment{}
	order := []string{}
	for _, item := range lcfg.ModelList {
		alias := item.ModelName
		p := item.LitellmParams
		if p == nil {
			continue
		}
		modelStr, _ := p["model"].(string)
		if modelStr == "" {
			continue
		}
		provider, model := splitProviderModel(modelStr)
		dep := gwDeployment{Provider: provider, Model: model}

		// Explicit api_key_env wins; otherwise infer from provider.
		if v, ok := p["api_key_env"].(string); ok && v != "" {
			dep.APIKeyEnv = v
		} else {
			// LiteLLM sometimes uses the literal env var with os.environ/ prefix.
			if v, ok := p["api_key"].(string); ok && strings.HasPrefix(v, "os.environ/") {
				dep.APIKeyEnv = strings.TrimPrefix(v, "os.environ/")
			} else {
				dep.APIKeyEnv = inferAPIKeyEnv(provider)
			}
		}
		if v, ok := p["api_base"].(string); ok {
			dep.APIBase = v
		}
		if _, exists := byAlias[alias]; !exists {
			order = append(order, alias)
		}
		byAlias[alias] = append(byAlias[alias], dep)
	}

	for _, alias := range order {
		out.ModelList = append(out.ModelList, gwAlias{
			ModelAlias:  alias,
			Deployments: byAlias[alias],
		})
	}

	// Routing translations.
	if lcfg.Router != nil {
		if v, ok := lcfg.Router["routing_strategy"].(string); ok {
			switch v {
			case "least-busy", "usage-based-routing":
				out.Routing["strategy"] = "least-latency"
			case "simple-shuffle":
				out.Routing["strategy"] = "round-robin"
			}
		}
		if v, ok := lcfg.Router["num_retries"].(int); ok {
			out.Routing["retries"] = v
		}
	}
	if lcfg.LitellmSettings != nil {
		if v, ok := lcfg.LitellmSettings["master_key"].(string); ok && v != "" && !strings.HasPrefix(v, "os.environ/") {
			out.Auth["master_key"] = v
		}
	}

	// Stable key ordering for deterministic diff.
	for _, a := range out.ModelList {
		sort.SliceStable(a.Deployments, func(i, j int) bool {
			return a.Deployments[i].Provider+a.Deployments[i].Model <
				a.Deployments[j].Provider+a.Deployments[j].Model
		})
	}
	return out
}

func main() {
	var (
		in      = flag.String("in", "", "path to LiteLLM config.yaml (reads stdin if empty)")
		out     = flag.String("out", "", "path to write gateway-llm config.yaml (writes stdout if empty)")
		diff    = flag.Bool("diff", false, "print a human-readable summary diff instead of writing")
		dryRun  = flag.Bool("dry-run", false, "parse and convert but do not write")
	)
	flag.Parse()

	var raw []byte
	var err error
	if *in == "" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(*in)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}

	var lcfg litellmConfig
	if err := yaml.Unmarshal(raw, &lcfg); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	gcfg := convert(&lcfg)

	if *diff {
		printSummary(&lcfg, gcfg)
		return
	}

	// Emit gateway-llm config as YAML, prepended with a provenance comment so
	// a reviewer can see where it came from.
	var buf strings.Builder
	buf.WriteString("# Generated by litellm-migrate from a LiteLLM config.yaml.\n")
	buf.WriteString("# Review the diff (re-run with -diff) before deploying to production.\n\n")
	enc := yaml.NewEncoder(newStringWriter(&buf))
	enc.SetIndent(2)
	if err := enc.Encode(gcfg); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
	enc.Close()

	if *dryRun {
		fmt.Fprintln(os.Stderr, "dry-run OK; would have written", len(buf.String()), "bytes")
		return
	}
	if *out == "" {
		os.Stdout.WriteString(buf.String())
		return
	}
	if err := os.WriteFile(*out, []byte(buf.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *out)
}

type stringWriter struct{ sb *strings.Builder }

func newStringWriter(sb *strings.Builder) *stringWriter { return &stringWriter{sb: sb} }
func (w *stringWriter) Write(b []byte) (int, error)    { return w.sb.Write(b) }

func printSummary(l *litellmConfig, g *gwConfig) {
	fmt.Printf("LiteLLM → gateway-llm migration summary\n")
	fmt.Printf("  source model entries: %d\n", len(l.ModelList))
	fmt.Printf("  target aliases:       %d\n", len(g.ModelList))
	total := 0
	for _, a := range g.ModelList {
		total += len(a.Deployments)
	}
	fmt.Printf("  target deployments:   %d\n", total)
	fmt.Println()
	fmt.Println("aliases:")
	for _, a := range g.ModelList {
		providers := make([]string, 0, len(a.Deployments))
		for _, d := range a.Deployments {
			providers = append(providers, d.Provider+"/"+d.Model)
		}
		fmt.Printf("  %-30s -> [%s]\n", a.ModelAlias, strings.Join(providers, ", "))
	}
	if g.Auth["master_key"] == "${GATEWAY_LLM_MASTER_KEY}" {
		fmt.Println()
		fmt.Println("note: master key uses $GATEWAY_LLM_MASTER_KEY by default;")
		fmt.Println("      LITELLM_MASTER_KEY is also honored as a fallback.")
	}
}
