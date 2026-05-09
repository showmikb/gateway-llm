package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gateway-llm/gateway-llm/internal/audit"
	"github.com/gateway-llm/gateway-llm/internal/cache"
	"github.com/gateway-llm/gateway-llm/internal/callbacks"
	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/crypto"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/guardrails"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/observability"
	"github.com/gateway-llm/gateway-llm/internal/observability/otelmetrics"
	"github.com/gateway-llm/gateway-llm/internal/plugins"
	"github.com/gateway-llm/gateway-llm/internal/policy"
	"github.com/gateway-llm/gateway-llm/internal/privacy"
	"github.com/gateway-llm/gateway-llm/internal/receipt"
	"github.com/gateway-llm/gateway-llm/internal/replay"
	"github.com/gateway-llm/gateway-llm/internal/wasmplugin"
	"github.com/gateway-llm/gateway-llm/internal/respcache"
	"github.com/gateway-llm/gateway-llm/internal/savings"
	"github.com/gateway-llm/gateway-llm/internal/semcache"
	"github.com/gateway-llm/gateway-llm/internal/smartroute"
	"github.com/gateway-llm/gateway-llm/internal/providers"
	"github.com/gateway-llm/gateway-llm/internal/providers/anthropic"
	"github.com/gateway-llm/gateway-llm/internal/providers/azure"
	"github.com/gateway-llm/gateway-llm/internal/providers/bedrock"
	"github.com/gateway-llm/gateway-llm/internal/providers/bridge"
	"github.com/gateway-llm/gateway-llm/internal/providers/cohere"
	"github.com/gateway-llm/gateway-llm/internal/providers/gemini"
	"github.com/gateway-llm/gateway-llm/internal/providers/groq"
	"github.com/gateway-llm/gateway-llm/internal/providers/mistral"
	openai "github.com/gateway-llm/gateway-llm/internal/providers/openai"
	"github.com/gateway-llm/gateway-llm/internal/providers/vertexai"
	"github.com/gateway-llm/gateway-llm/internal/providers/xai"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/handlers"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"go.uber.org/zap"
)

//go:embed openapi.yaml
var openAPISpec []byte

type Server struct {
	cfg           *config.Config
	logger        *zap.Logger
	db            *db.DB
	cache         *cache.Cache
	h             *handlers.Handlers
	dispatcher    *callbacks.Dispatcher
	audit         *audit.Logger
	recorder      *replay.Recorder
	savingsRoller *savings.Roller
	otlpMetrics   *otelmetrics.Exporter
}

func sanitizedConfig(cfg *config.Config) map[string]interface{} {
	models := make([]map[string]interface{}, 0, len(cfg.ModelList))
	for _, m := range cfg.ModelList {
		deps := make([]map[string]string, 0, len(m.Deployments))
		for _, d := range m.Deployments {
			deps = append(deps, map[string]string{
				"provider": d.Provider,
				"model":    d.Model,
			})
		}
		models = append(models, map[string]interface{}{
			"model_alias": m.ModelAlias,
			"deployments": deps,
		})
	}
	out := map[string]interface{}{
		"server":        map[string]interface{}{"port": cfg.Server.Port},
		"model_list":    models,
		"routing":       cfg.Routing,
		"rate_limiting": cfg.RateLimiting,
	}
	if len(cfg.Callbacks) > 0 {
		out["callbacks"] = cfg.Callbacks
	}
	return out
}

func ensureMasterKey(ctx context.Context, cfg *config.Config, database *db.DB, logger *zap.Logger) error {
	type masterKeyEntry struct {
		Hash string `json:"hash"`
	}

	stored, err := database.GetSetting(ctx, "master_key_hash")
	if err != nil {
		return fmt.Errorf("read master_key_hash from DB: %w", err)
	}

	masterKey := cfg.Auth.MasterKey

	if stored == nil {
		if masterKey == "" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return fmt.Errorf("generate master key: %w", err)
			}
			masterKey = "sk-master-" + hex.EncodeToString(b)
			cfg.Auth.MasterKey = masterKey
			fmt.Fprintf(os.Stderr, "\n*** Generated new master key (save this — it will not be shown again): %s ***\n\n", masterKey)
			logger.Info("generated new master key — printed to stderr",
				zap.String("master_key_suffix", "..."+masterKey[len(masterKey)-4:]))
		}

		h := sha256.Sum256([]byte(masterKey))
		entry := masterKeyEntry{Hash: hex.EncodeToString(h[:])}
		data, _ := json.Marshal(entry)
		if err := database.UpsertSetting(ctx, "master_key_hash", data); err != nil {
			return fmt.Errorf("persist master_key_hash: %w", err)
		}
		logger.Info("master key hash persisted to database (locked for future starts)")
		return nil
	}

	if masterKey == "" {
		return fmt.Errorf("GATEWAY_LLM_MASTER_KEY environment variable is required (master key was previously configured)")
	}

	var entry masterKeyEntry
	if err := json.Unmarshal(stored, &entry); err != nil {
		return fmt.Errorf("parse stored master_key_hash: %w", err)
	}

	h := sha256.Sum256([]byte(masterKey))
	if hex.EncodeToString(h[:]) != entry.Hash {
		return fmt.Errorf("GATEWAY_LLM_MASTER_KEY does not match the key used during initial setup — " +
			"changing the master key would break encrypted credentials")
	}

	return nil
}

func New(cfg *config.Config, logger *zap.Logger) (*Server, error) {
	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("database.url is required")
	}
	ctx := context.Background()
	database, err := db.New(ctx, cfg.Database.URL, cfg.Database.MaxConnections, logger)
	if err != nil {
		return nil, err
	}
	if err := database.RunMigrations(ctx); err != nil {
		logger.Warn("migrations", zap.Error(err))
	}

	if err := ensureMasterKey(ctx, cfg, database, logger); err != nil {
		return nil, fmt.Errorf("master key check: %w", err)
	}

	reg := providers.NewRegistry()
	reg.SetResponsesBridge(func(cp providers.ChatProvider) providers.ResponsesProvider {
		return bridge.NewResponsesBridge(cp)
	})
	reg.Register(openai.New())
	reg.Register(anthropic.New())
	reg.Register(gemini.New())
	reg.Register(azure.New())
	reg.Register(bedrock.New())
	reg.Register(cohere.New())
	reg.Register(groq.New())
	reg.Register(mistral.New())
	reg.Register(vertexai.New())
	reg.Register(xai.New())

	modelRouter := router.New(cfg.ModelList, cfg.Routing, reg, logger)

	seedDeployments(ctx, database, cfg, logger)
	reloadRouterFromDB(ctx, database, modelRouter, logger)

	costEng := cost.NewEngine(database, logger)
	if err := costEng.LoadCustomPricing(ctx); err != nil {
		logger.Warn("load custom pricing", zap.Error(err))
	}

	// Wire pricing into the router so the "cheapest" strategy can compare
	// deployments by per-token cost without importing the cost package.
	modelRouter.SetPricingLookup(func(provider, model string) (float64, float64, bool) {
		p := costEng.GetPricing(provider, model)
		if p == nil {
			return 0, 0, false
		}
		return p.InputCostPerToken, p.OutputCostPerToken, true
	})

	c := cache.New(cfg.Redis.URL, logger)

	var cbConfigs []callbacks.CallbackConfig
	for _, cc := range cfg.Callbacks {
		cbConfigs = append(cbConfigs, callbacks.CallbackConfig{
			Type:         cc.Type,
			Endpoint:     cc.Endpoint,
			Protocol:     cc.Protocol,
			ServiceName:  cc.ServiceName,
			APIKeyEnv:    cc.APIKeyEnv,
			SpaceKeyEnv:  cc.SpaceKeyEnv,
			Site:         cc.Site,
			Project:      cc.Project,
			ModelID:      cc.ModelID,
			Method:       cc.Method,
			Headers:      cc.Headers,
			Timeout:      cc.Timeout,
			PublicKeyEnv: cc.PublicKeyEnv,
			SecretKeyEnv: cc.SecretKeyEnv,
			TokenEnv:     cc.TokenEnv,
			Index:        cc.Index,
			Source:       cc.Source,
			MinCostUSD:   cc.MinCostUSD,
		})
	}
	dispatcher := callbacks.NewFromConfig(cbConfigs, logger)

	var rc *respcache.Cache
	if cfg.ResponseCache.Enabled {
		rc = respcache.New(&respcache.RedisAdapter{Client: c.RedisClient()}, cfg.ResponseCache.TTL, logger)
	}
	guardEngine := guardrails.NewEngine(cfg.Guardrails)
	smart := smartroute.New(cfg.SmartRoute)
	if cfg.SmartRoute.MLModelPath != "" {
		ml := smartroute.NewMLClassifier()
		if err := ml.LoadFile(cfg.SmartRoute.MLModelPath); err != nil {
			logger.Warn("smartroute ml model load failed, using rule fallback",
				zap.String("path", cfg.SmartRoute.MLModelPath), zap.Error(err))
		} else {
			smart.WithML(ml)
			logger.Info("smartroute ml model loaded", zap.String("path", cfg.SmartRoute.MLModelPath))
		}
	}
	if cfg.SmartRoute.ShadowChallenger != "" && cfg.SmartRoute.ShadowPercent > 0 {
		smart.WithShadow(smartroute.NewShadowRunner(cfg.SmartRoute.ShadowMaxConcurrent, logger))
	}
	auditLogger := audit.New(database, logger)

	pluginMgr, pluginErrs := plugins.NewManager(cfg.Plugins, logger)
	for _, e := range pluginErrs {
		logger.Warn("plugin load error", zap.Error(e))
	}

	// WASM plugin host. Loaded modules are attached to the pluginMgr so
	// they share the same pre/post pipeline as compile-in plugins.
	var wasmHost *wasmplugin.Host
	if cfg.Plugins.Enabled && len(cfg.Plugins.Wasm) > 0 {
		wasmHost = wasmplugin.NewHost(context.Background(), logger)
		for _, wc := range cfg.Plugins.Wasm {
			p, err := wasmHost.Load(context.Background(), wasmplugin.Config{
				Name:        wc.Name,
				Path:        wc.Path,
				Options:     wc.Options,
				CallTimeout: wc.CallTimeout,
			})
			if err != nil {
				logger.Warn("wasm plugin load failed", zap.String("name", wc.Name), zap.Error(err))
				continue
			}
			pluginMgr.Attach(p)
		}
	}
	_ = wasmHost

	var semCache *semcache.Cache
	if cfg.SemanticCache.Enabled {
		var semEmb semcache.Embedder
		switch cfg.SemanticCache.Embedder {
		case "openai":
			embKey := cfg.SemanticCache.EmbedderAPIKey
			if embKey == "" && cfg.SemanticCache.EmbedderAPIKeyEnv != "" {
				embKey = os.Getenv(cfg.SemanticCache.EmbedderAPIKeyEnv)
			}
			if embKey == "" {
				if cred, err := database.FindFirstActiveCredentialByProvider(ctx, "openai"); err == nil && cred != nil {
					if decrypted, err := crypto.Decrypt(cred.APIKeyEnc); err == nil {
						embKey = decrypted
					}
				}
			}
			if embKey == "" {
				logger.Warn("semantic_cache.embedder=openai but no API key found; falling back to hash embedder")
				semEmb = semcache.NewHashEmbedder(cfg.SemanticCache.Dim)
			} else {
				semEmb = semcache.NewOpenAIEmbedder(embKey, cfg.SemanticCache.EmbedderModel, logger)
				logger.Info("semantic cache using OpenAI embeddings",
					zap.String("model", cfg.SemanticCache.EmbedderModel))
			}
		default:
			semEmb = semcache.NewHashEmbedder(cfg.SemanticCache.Dim)
			logger.Info("semantic cache using hash embedder")
		}
		semCache = semcache.New(semcache.Config{
			Enabled:       true,
			SimilarityMin: cfg.SemanticCache.SimilarityMin,
			MaxEntries:    cfg.SemanticCache.MaxEntries,
			TTL:           cfg.SemanticCache.TTL,
		}, semEmb, logger)
	}

	// Record layer (pillar 1). When enabled, every /v1 endpoint with
	// x_gateway_llm_record=true (or sampled-in by the config) enqueues a
	// copy of the canonical request/response to the blob store + Postgres.
	var recorder *replay.Recorder
	var blobStore replay.BlobStore
	if cfg.Recording.Enabled {
		store, sErr := replay.NewBlobStoreFromURL(cfg.Recording.BlobStoreURL)
		if sErr != nil {
			logger.Warn("recording disabled: blob store init failed", zap.Error(sErr))
		} else {
			blobStore = store
			recorder = replay.NewRecorder(store, database, logger)
			logger.Info("recording enabled",
				zap.String("blob_store", cfg.Recording.BlobStoreURL),
				zap.String("scheme", store.Scheme()))
		}
	}

	// Privacy layer (pillar 3a). Redacts PII on the outbound request
	// and rehydrates tokens on the response. Enabled per-config; the
	// default rule pack covers email/phone/SSN/CC/IBAN/IP.
	var privacyEng *privacy.Engine
	if cfg.Privacy.Enabled {
		salt := []byte(cfg.Auth.MasterKey)
		if len(salt) == 0 {
			salt = []byte("gateway-llm-default-privacy-salt")
		}
		privacyEng = privacy.NewEngine(salt)
		logger.Info("privacy redaction enabled",
			zap.String("residency", cfg.Privacy.Residency),
			zap.Int("custom_rules", len(cfg.Privacy.Redactors)))
	}

	var policyEng *policy.Engine
	if cfg.Policy.Enabled {
		eng, err := policy.LoadFile(cfg.Policy.RulesPath)
		if err != nil {
			logger.Warn("policy load failed; continuing with empty rules", zap.Error(err))
			policyEng = policy.NewEngine()
		} else {
			policyEng = eng
			logger.Info("policy engine enabled", zap.String("rules_path", cfg.Policy.RulesPath))
		}
	}

	var receiptSigner *receipt.Signer
	if cfg.Receipts.Enabled {
		priv, err := receipt.LoadOrGenerateKey(cfg.Receipts.SigningKeyPath)
		if err != nil {
			logger.Error("receipt key load failed; disabling receipts", zap.Error(err))
		} else {
			receiptSigner = receipt.New(priv, cfg.Receipts.ChainHashPrevious)
			logger.Info("cryptographic receipts enabled",
				zap.String("key_id", receiptSigner.KeyID()),
				zap.Bool("hash_chain", cfg.Receipts.ChainHashPrevious))
		}
	}

	// Operator-signed price catalog (smartroute moat). Reuses the
	// receipt key as the catalog signer so verifiers only need one
	// pubkey. On a fresh DB we auto-seed from the embedded
	// model_prices.json so single-tenant deploys "just work".
	if receiptSigner != nil {
		costEng.SetOperatorPubKey(receiptSigner.PublicKey())
		if err := costEng.LoadOperatorCatalog(ctx, database); err != nil {
			logger.Warn("load operator catalog", zap.Error(err))
		}
		if n, err := database.CountOperatorCatalog(ctx); err == nil && n == 0 {
			if seeded := autoSeedCatalog(ctx, database, costEng, receiptSigner, logger); seeded > 0 {
				logger.Info("operator catalog auto-seeded from embedded prices",
					zap.Int("rows", seeded))
				if err := costEng.LoadOperatorCatalog(ctx, database); err != nil {
					logger.Warn("reload operator catalog after seed", zap.Error(err))
				}
			}
		}
	}
	if err := costEng.LoadEffectiveDiscounts(ctx, database); err != nil {
		logger.Warn("load effective discounts", zap.Error(err))
	}

	// Smart-routing moat: Writer signs each routing_savings row with
	// the same Ed25519 key as receipts. Roller materializes
	// daily_savings on a 5-minute tick so the dashboard chart and
	// invoicing rollups stay fresh without operator intervention.
	var savingsWriter *savings.Writer
	var savingsRoller *savings.Roller
	if database != nil && receiptSigner != nil {
		savingsWriter = savings.NewWriter(costEng, database, receiptSigner.PrivateKey(), logger)
		cutPct := 0.20
		if cfg != nil && cfg.SmartRoute.SavingsCutPct > 0 {
			cutPct = cfg.SmartRoute.SavingsCutPct
		}
		savingsRoller = savings.NewRoller(database, cutPct, 5*time.Minute, logger)
		savingsRoller.Start(ctx)
	}

	// OTLP metric push (optional). Same metric names as /metrics so
	// dashboards stay 1:1 whether the operator chose Prometheus pull
	// or OTLP push.
	var otlpExp *otelmetrics.Exporter
	if cfg.Metrics.OTLP.Endpoint != "" {
		exp, err := otelmetrics.Start(ctx, otelmetrics.Config{
			Endpoint:       cfg.Metrics.OTLP.Endpoint,
			Protocol:       cfg.Metrics.OTLP.Protocol,
			Headers:        cfg.Metrics.OTLP.Headers,
			Insecure:       cfg.Metrics.OTLP.Insecure,
			Interval:       cfg.Metrics.OTLP.Interval,
			ServiceName:    "gateway-llm",
			ServiceVersion: "",
		}, logger)
		if err != nil {
			logger.Warn("otlp metrics exporter init failed", zap.Error(err))
		} else {
			otlpExp = exp
		}
	}

	h := &handlers.Handlers{
		Router:     modelRouter,
		Registry:   reg,
		CostEng:    costEng,
		DB:         database,
		Cache:      c,
		RespCache:  rc,
		SemCache:   semCache,
		Plugins:    pluginMgr,
		Guard:      guardEngine,
		SmartRoute: smart,
		Audit:      auditLogger,
		Logger:     logger,
		Dispatcher:   dispatcher,
		Recorder:     recorder,
		BlobStore:    blobStore,
		Privacy:      privacyEng,
		Policy:       policyEng,
		Receipts:     receiptSigner,
		Cfg:          cfg,
		Savings:      savingsWriter,
		OTLPMetrics:  otlpExp,
	}
	_ = savingsRoller

	// Replay engine needs a back-reference to the handlers so it can
	// re-execute chat requests through the live router+providers.
	if blobStore != nil && database != nil {
		h.ReplayEngine = replay.NewEngine(database, blobStore, h.ChatExecutorForReplay(), logger)
	}

	return &Server{cfg: cfg, logger: logger, db: database, cache: c, h: h, dispatcher: dispatcher, audit: auditLogger, recorder: recorder, savingsRoller: savingsRoller, otlpMetrics: otlpExp}, nil
}

// autoSeedCatalog mirrors the embedded model_prices.json into
// operator_price_catalog and signs each row using the receipt key. It
// returns the number of rows seeded. Idempotent: running it again
// supersedes existing rows with fresh signatures (date moves forward,
// values stay the same), which is harmless.
func autoSeedCatalog(ctx context.Context, database *db.DB, costEng *cost.Engine, signer *receipt.Signer, logger *zap.Logger) int {
	if signer == nil || costEng == nil {
		return 0
	}
	priv := signer.PrivateKey()
	if priv == nil {
		return 0
	}
	all := costEng.GetAllPricing()
	seeded := 0
	for _, p := range all {
		if p == nil || p.Provider == "" || p.Model == "" {
			continue
		}
		row := &models.OperatorPriceCatalog{
			Provider:           p.Provider,
			Model:              p.Model,
			Mode:               p.Mode,
			InputCostPerToken:  p.InputCostPerToken,
			OutputCostPerToken: p.OutputCostPerToken,
			Source:             "seed",
			SizePricing:        p.SizePricing,
		}
		if p.CacheReadCostPerToken > 0 {
			v := p.CacheReadCostPerToken
			row.CacheReadCostPerToken = &v
		}
		if p.InputCostPerImage > 0 {
			v := p.InputCostPerImage
			row.InputCostPerImage = &v
		}
		if p.InputCostPerCharacter > 0 {
			v := p.InputCostPerCharacter
			row.InputCostPerCharacter = &v
		}
		if p.InputCostPerSecond > 0 {
			v := p.InputCostPerSecond
			row.InputCostPerSecond = &v
		}
		if p.MaxInputTokens > 0 {
			v := p.MaxInputTokens
			row.MaxInputTokens = &v
		}
		if p.MaxOutputTokens > 0 {
			v := p.MaxOutputTokens
			row.MaxOutputTokens = &v
		}
		if err := cost.SignCatalogRow(priv, row); err != nil {
			logger.Warn("seed sign failed", zap.String("provider", p.Provider), zap.String("model", p.Model), zap.Error(err))
			continue
		}
		if err := database.UpsertOperatorCatalogRow(ctx, row); err != nil {
			logger.Warn("seed upsert failed", zap.String("provider", p.Provider), zap.String("model", p.Model), zap.Error(err))
			continue
		}
		seeded++
	}
	return seeded
}

func seedDeployments(ctx context.Context, database *db.DB, cfg *config.Config, logger *zap.Logger) {
	count, err := database.CountDeployments(ctx)
	if err != nil {
		logger.Warn("count deployments for seed", zap.Error(err))
		return
	}
	if count > 0 {
		return
	}

	logger.Info("seeding deployments from config.yaml")
	for _, alias := range cfg.ModelList {
		for _, d := range alias.Deployments {
			dep := &models.Deployment{
				ModelAlias:    alias.ModelAlias,
				Provider:      d.Provider,
				ProviderModel: d.Model,
				APIKeyEnv:     d.APIKeyEnv,
				APIBase:       d.APIBase,
				Priority:      d.Priority,
				IsActive:      true,
			}
			if err := database.CreateDeployment(ctx, dep); err != nil {
				logger.Warn("seed deployment", zap.Error(err), zap.String("alias", alias.ModelAlias))
			}
		}
	}
}

func reloadRouterFromDB(ctx context.Context, database *db.DB, modelRouter *router.ModelRouter, logger *zap.Logger) {
	deps, err := database.GetActiveDeployments(ctx)
	if err != nil {
		logger.Warn("load deployments from DB", zap.Error(err))
		return
	}
	if len(deps) == 0 {
		return
	}

	aliases := make(map[string][]*router.DeploymentInfo)
	for _, d := range deps {
		apiKey := ""
		if d.CredentialID != nil {
			cred, err := database.GetCredential(ctx, *d.CredentialID)
			if err == nil && cred != nil && cred.IsActive {
				decrypted, err := crypto.Decrypt(cred.APIKeyEnc)
				if err == nil {
					apiKey = decrypted
				} else {
					logger.Warn("decrypt credential", zap.Error(err), zap.String("name", cred.Name))
				}
			}
		}
		if apiKey == "" && d.APIKeyEnv != "" {
			apiKey = os.Getenv(d.APIKeyEnv)
		}

		aliases[d.ModelAlias] = append(aliases[d.ModelAlias], &router.DeploymentInfo{
			Provider:        d.Provider,
			ProviderModel:   d.ProviderModel,
			APIKey:          apiKey,
			APIBase:         d.APIBase,
			Priority:        d.Priority,
			Weight:          d.Weight,
			VariantID:       d.VariantID,
			RoutingStrategy: d.RoutingStrategy,
			OrgID:           d.OrgID,
		})
	}

	modelRouter.Reload(aliases)
	logger.Info("router reloaded from DB", zap.Int("aliases", len(aliases)))
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// Normalize duplicate slashes (e.g. /v1//chat/completions ->
	// /v1/chat/completions) so OpenAI clients that join base URLs with
	// trailing slashes (notably Cursor's "Override OpenAI Base URL"
	// setting) don't 404 on otherwise valid endpoints.
	r.Use(chimiddleware.CleanPath)
	r.Use(cors.Handler(middleware.CORS()))
	r.Use(middleware.RequestLogger(s.logger))

	r.Get("/", s.h.APIRoot)
	r.Get("/docs", s.h.SwaggerUI)
	r.Get("/docs/openapi.yaml", handlers.ServeOpenAPISpec(openAPISpec))

	r.Get("/health", s.h.HealthLive)
	r.Get("/health/ready", s.h.HealthReady)
	r.Get("/.well-known/gateway-llm-receipts.json", s.h.WellKnownReceiptKey)

	// Prometheus scrape surface. Unauthenticated by design (same as
	// /healthz) so any standard scraper - Datadog Agent, Grafana Agent,
	// OTel collector - can pull from it without managing tokens. Bind on
	// an internal listener if you need to keep it off the public IP.
	if s.cfg.Metrics.Enabled {
		path := s.cfg.Metrics.Path
		if path == "" {
			path = "/metrics"
		}
		r.Method(http.MethodGet, path, observability.Handler())
	}
	// LiteLLM spells these differently; accept both for drop-in compat.
	r.Get("/health/liveliness", s.h.LiteLLMLiveliness)
	r.Get("/health/readiness", s.h.HealthReady)

	r.Post("/v1/management/users/login", s.h.LoginUser)

	registerLimiter := middleware.NewRegisterRateLimit(5, time.Hour)
	r.Group(func(r chi.Router) {
		r.Use(registerLimiter.Middleware)
		r.Post("/v1/management/users/register", s.h.RegisterUser)
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(s.db, s.cfg.Auth.MasterKey, s.logger))
		r.Get("/api/config", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sanitizedConfig(s.cfg))
		})
		r.Put("/api/config", s.h.UpdateSettings)
	})

	// --- LiteLLM drop-in compatibility routes ---
	// LiteLLM clients call these paths directly (no /v1 prefix). We mount
	// them at the top level and let Auth gate everything except the
	// public liveliness probe.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(s.db, s.cfg.Auth.MasterKey, s.logger))
		r.Get("/model/info", s.h.LiteLLMModelInfo)
		r.Post("/key/generate", s.h.LiteLLMKeyGenerate)
		r.Get("/key/info", s.h.LiteLLMKeyInfo)
		r.Post("/key/delete", s.h.LiteLLMKeyDelete)
		r.Post("/spend/calculate", s.h.LiteLLMSpendCalculate)
		r.Get("/spend/logs", s.h.LiteLLMSpendLogs)
		r.Post("/team/new", s.h.LiteLLMTeamNew)
		r.Get("/team/info", s.h.LiteLLMTeamInfo)
		r.Get("/team/info/{team_id}", s.h.LiteLLMTeamInfo)
		r.Post("/budget/new", s.h.LiteLLMBudgetNew)
		r.Post("/user/new", s.h.LiteLLMUserNew)
		r.Get("/user/info", s.h.LiteLLMUserInfo)
	})

	// Universal Ingress (Pillar 4). Non-OpenAI SDKs hit these paths and
	// are translated to the canonical pipeline. Same auth and rate
	// limits as /v1 so governance semantics don't depend on which SDK
	// the customer uses.
	r.Route("/anthropic/v1", func(r chi.Router) {
		r.Use(middleware.Auth(s.db, s.cfg.Auth.MasterKey, s.logger))
		r.Use(middleware.RateLimit(s.cache, s.cfg.RateLimiting, s.logger))
		r.Use(middleware.AuditLLMRequests(s.audit))
		r.Post("/messages", s.h.AnthropicMessages)
	})
	r.Route("/v1beta", func(r chi.Router) {
		r.Use(middleware.Auth(s.db, s.cfg.Auth.MasterKey, s.logger))
		r.Use(middleware.RateLimit(s.cache, s.cfg.RateLimiting, s.logger))
		r.Use(middleware.AuditLLMRequests(s.audit))
		// Gemini uses :generateContent and :streamGenerateContent suffixes
		// which chi doesn't natively route on, so catch-all the path and
		// parse in the handler.
		r.Post("/models/*", s.h.GeminiGenerateContent)
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.Auth(s.db, s.cfg.Auth.MasterKey, s.logger))
		r.Use(middleware.RateLimit(s.cache, s.cfg.RateLimiting, s.logger))
		r.Use(middleware.AuditLLMRequests(s.audit))

		r.Get("/models", s.h.ListModels)
		r.Get("/models/{model_id}", s.h.GetModel)

		r.Post("/chat/completions", s.h.ChatCompletions)
		r.Post("/embeddings", s.h.Embeddings)
		r.Post("/completions", s.h.Completions)
		r.Post("/images/generations", s.h.ImageGenerations)
		r.Post("/audio/speech", s.h.AudioSpeech)
		r.Post("/audio/transcriptions", s.h.AudioTranscriptions)
		r.Post("/moderations", s.h.Moderations)
		r.Post("/responses", s.h.CreateResponse)
		r.Get("/responses/{response_id}", s.h.GetResponse)
		r.Delete("/responses/{response_id}", s.h.DeleteResponse)

		r.Post("/feedback", s.h.CreateFeedback)
		r.Get("/metrics", s.h.GetMetrics)

		// Record & Replay (pillar 1). Recordings are scoped by org; replay
		// runs re-execute stored requests through the live router and score
		// the results against the original response.
		r.Get("/recordings", s.h.ListRecordings)
		r.Get("/recordings/{id}", s.h.GetRecording)
		r.Post("/replay/runs", s.h.StartReplayRun)
		r.Get("/replay/runs/{id}", s.h.GetReplayRun)
		r.Get("/replay/runs/{id}/results", s.h.ListReplayResults)
		r.Post("/eval/recordings/{id}", s.h.ScoreRecording)
		r.Get("/eval/recordings/{id}", s.h.ListEvalScores)

		// Cryptographic billing receipts (pillar 3b). GET is authenticated
		// because receipts leak token and cost data; the public key used
		// for verification is served at /.well-known/gateway-llm-receipts.json
		// without auth.
		r.Get("/receipts/{id}", s.h.GetReceipt)
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireRole(middleware.RoleOrgAdmin))
			r.Get("/audit", s.h.ListAuditEvents)
		})

		r.Route("/management", func(r chi.Router) {
			// Viewer-readable: GET-only endpoints for read-only dashboard access
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(middleware.RoleViewer))
				r.Get("/keys", s.h.ListAPIKeys)
				r.Get("/keys/{id}/usage", s.h.GetKeyUsage)
				r.Get("/keys/{id}/usage/daily", s.h.GetKeyDailyUsage)
				r.Get("/teams", s.h.ListTeams)
				r.Get("/deployments", s.h.GetDeployments)
				r.Get("/users", s.h.ListUsers)
				r.Get("/users/{id}", s.h.GetUser)
				r.Get("/organizations", s.h.ListOrganizations)
				r.Get("/credentials", s.h.ListCredentials)
				r.Get("/usage", s.h.GetUsage)
				r.Get("/usage/daily", s.h.GetDailyUsage)
				r.Get("/pricing", s.h.ListPricing)
				r.Get("/pricing/{provider}/{model}", s.h.GetModelPricing)
				r.Get("/callbacks", s.h.ListCallbacks)
				r.Get("/providers", s.h.ListProviders)
			})

			// Team admin: team + key management
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(middleware.RoleTeamAdmin))
				r.Post("/teams", s.h.CreateTeam)
				r.Delete("/teams/{id}", s.h.DeleteTeam)

				r.Post("/keys", s.h.CreateAPIKey)
				r.Put("/keys/{id}", s.h.UpdateAPIKeyHandler)
				r.Delete("/keys/{id}", s.h.DeleteAPIKey)
			})

			// Org admin / super admin: full management
			r.Group(func(r chi.Router) {
				r.Use(middleware.AdminOnly(s.cfg.Auth.MasterKey))

				r.Post("/users", s.h.CreateUser)
				r.Put("/users/{id}", s.h.UpdateUser)
				r.Delete("/users/{id}", s.h.DeleteUser)

				r.Post("/credentials", s.h.CreateCredential)
				r.Put("/credentials/{id}", s.h.UpdateCredential)
				r.Delete("/credentials/{id}", s.h.DeleteCredential)
				r.Post("/credentials/{id}/test", s.h.TestCredential)
				r.Post("/credentials/test-raw", s.h.TestRawCredential)

				r.Post("/deployments", s.h.CreateDeploymentHandler)
				r.Put("/deployments/{id}", s.h.UpdateDeploymentHandler)
				r.Delete("/deployments/{id}", s.h.DeleteDeploymentHandler)
				r.Post("/deployments/alias-strategy", s.h.UpdateAliasStrategyHandler)

				r.Post("/organizations", s.h.CreateOrganization)
				r.Put("/organizations/{id}", s.h.UpdateOrganization)
				r.Delete("/organizations/{id}", s.h.DeleteOrganization)

				r.Post("/callbacks/test", s.h.TestCallbacks)

				r.Post("/providers/discover", s.h.DiscoverModels)
				r.Post("/routes/sync", s.h.SyncRoutes)
			})

			// Smart-routing moat: routing policy editor is org-admin
			// scoped (per-alias config), savings ledger is viewer-readable.
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(middleware.RoleViewer))
				r.Get("/operator/catalog", s.h.ListOperatorCatalog)
				r.Get("/savings", s.h.ListSavings)
				r.Get("/savings/daily", s.h.ListDailySavings)
				r.Get("/savings/{id}", s.h.GetSavingsRecord)
				r.Get("/savings/by-trace/{trace}", s.h.GetSavingsByTrace)
				r.Get("/savings/{id}/verify", s.h.VerifySavingsRecord)
				r.Get("/discounts", s.h.ListDiscounts)
				r.Get("/routing-policy", s.h.ListRoutingPolicies)
			})

			// Org admin tier: declare discounts + edit routing policies.
			r.Group(func(r chi.Router) {
				r.Use(middleware.AdminOnly(s.cfg.Auth.MasterKey))
				r.Post("/discounts", s.h.DeclareDiscount)
				r.Delete("/discounts/{id}", s.h.RevokeDiscount)
				r.Put("/routing-policy/{alias}", s.h.UpsertRoutingPolicy)
				r.Delete("/routing-policy/{alias}", s.h.DeleteRoutingPolicy)
			})

			// Master-key only: pricing edits, catalog signing, discount
			// counter-signature. The moat's tamper-proof guarantee
			// depends on these endpoints staying behind the master key.
			r.Group(func(r chi.Router) {
				r.Use(middleware.MasterKeyOnly())
				r.Put("/pricing/{provider}/{model}", s.h.SetModelPricing)
				r.Delete("/pricing/{provider}/{model}", s.h.DeleteModelPricing)
				r.Put("/operator/catalog/{provider}/{model}", s.h.SetOperatorCatalogRow)
				r.Post("/operator/catalog/seed", s.h.SeedOperatorCatalogFromEmbedded)
				r.Post("/discounts/{id}/countersign", s.h.CountersignDiscount)
			})
		})

		// OpenAI-compatible passthrough: catch-all for any /v1/* path
		// not matched by typed handlers above. Must be registered last
		// so it never shadows Gateway-owned routes.
		r.HandleFunc("/*", s.h.OpenAIPassthrough)
	})

	return r
}

func (s *Server) ReloadRouter() {
	ctx := context.Background()
	reloadRouterFromDB(ctx, s.db, s.h.Router, s.logger)
}

func (s *Server) Close() {
	if s.dispatcher != nil {
		s.dispatcher.Close()
	}
	if s.audit != nil {
		s.audit.Close()
	}
	if s.recorder != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = s.recorder.Stop(ctx)
		cancel()
	}
	if s.savingsRoller != nil {
		s.savingsRoller.Stop()
	}
	if s.otlpMetrics != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.otlpMetrics.Shutdown(ctx)
		cancel()
	}
	if s.db != nil {
		s.db.Close()
	}
}
