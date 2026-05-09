// Package otelmetrics emits the same metric names the Prometheus
// /metrics endpoint exposes, but pushed via the OTLP wire protocol so
// any OTLP-compatible backend (Honeycomb, New Relic, Tempo+Mimir,
// Grafana Cloud OTLP, Datadog OTLP intake) can ingest gateway-llm
// telemetry with one config knob and no scrape job.
//
// The exporter runs entirely inside the gateway process. The lifecycle
// is owned by server.go (Start during boot, Shutdown during graceful
// stop) so a misconfigured collector can't take down the hot path —
// failures are logged and the in-process Prometheus surface still
// serves as a fallback.
package otelmetrics

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"
)

// Config configures the OTLP metric exporter. Endpoint follows the
// usual OTel conventions: "https://otel.example.com" for HTTP,
// "otel-collector:4317" for gRPC. Headers are merged into the request
// (e.g. {"x-honeycomb-team":"..."}). Insecure flips off TLS for the
// gRPC transport when talking to a sidecar collector.
type Config struct {
	Endpoint string
	Protocol string
	Headers  map[string]string
	Insecure bool
	Interval time.Duration

	ServiceName    string
	ServiceVersion string
}

// Exporter holds the meter provider + shutdown hook so server.go can
// flush+close on graceful stop.
type Exporter struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
	logger   *zap.Logger

	requests       metric.Int64Counter
	costUSD        metric.Float64Counter
	tokens         metric.Int64Counter
	latencyMS      metric.Float64Histogram
	moatRequests   metric.Int64Counter
	moatBaseline   metric.Float64Counter
	moatActual     metric.Float64Counter
	moatSavings    metric.Float64Counter
	moatQuality    metric.Float64Histogram
	moatJudge      metric.Int64Counter
	moatDecisions  metric.Int64Counter
}

// Start spins up the OTLP exporter + periodic reader. Returns nil
// (and a no-op Exporter) when cfg.Endpoint is empty so callers can
// unconditionally invoke Start without branching on config.
func Start(ctx context.Context, cfg Config, logger *zap.Logger) (*Exporter, error) {
	if cfg.Endpoint == "" {
		return nil, nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "gateway-llm"
	}

	exp, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, err
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp,
			sdkmetric.WithInterval(cfg.Interval),
		)),
	)
	meter := provider.Meter("github.com/gateway-llm/gateway-llm")

	e := &Exporter{provider: provider, meter: meter, logger: logger}
	if err := e.bindInstruments(); err != nil {
		_ = provider.Shutdown(ctx)
		return nil, err
	}
	logger.Info("otlp metrics exporter started",
		zap.String("endpoint", cfg.Endpoint),
		zap.String("protocol", cfg.Protocol),
		zap.Duration("interval", cfg.Interval))
	return e, nil
}

func newExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Protocol)) {
	case "", "http", "http/protobuf":
		opts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(cleanEndpoint(cfg.Endpoint)),
		}
		if cfg.Insecure || strings.HasPrefix(cfg.Endpoint, "http://") {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
		}
		return otlpmetrichttp.New(ctx, opts...)
	case "grpc":
		opts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(cleanEndpoint(cfg.Endpoint)),
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
		}
		return otlpmetricgrpc.New(ctx, opts...)
	default:
		return nil, errors.New("otelmetrics: unknown protocol (want http or grpc)")
	}
}

// cleanEndpoint trims scheme prefixes since the OTLP exporters expect
// host:port style endpoints.
func cleanEndpoint(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.TrimRight(s, "/")
}

func (e *Exporter) bindInstruments() error {
	var err error
	if e.requests, err = e.meter.Int64Counter("gatewayllm.requests",
		metric.WithDescription("Total LLM requests served")); err != nil {
		return err
	}
	if e.costUSD, err = e.meter.Float64Counter("gatewayllm.cost_usd",
		metric.WithDescription("Total upstream spend in USD"),
		metric.WithUnit("USD")); err != nil {
		return err
	}
	if e.tokens, err = e.meter.Int64Counter("gatewayllm.tokens",
		metric.WithDescription("Tokens billed by upstream providers")); err != nil {
		return err
	}
	if e.latencyMS, err = e.meter.Float64Histogram("gatewayllm.request_duration_ms",
		metric.WithDescription("Request latency in milliseconds"),
		metric.WithUnit("ms")); err != nil {
		return err
	}
	if e.moatRequests, err = e.meter.Int64Counter("gatewayllm.moat.requests",
		metric.WithDescription("Smart-routing-aware request counter")); err != nil {
		return err
	}
	if e.moatBaseline, err = e.meter.Float64Counter("gatewayllm.moat.baseline_cost_usd",
		metric.WithDescription("Cost in USD that would have been billed at the baseline model"),
		metric.WithUnit("USD")); err != nil {
		return err
	}
	if e.moatActual, err = e.meter.Float64Counter("gatewayllm.moat.cost_usd",
		metric.WithDescription("Actual cost in USD after smart routing + discounts"),
		metric.WithUnit("USD")); err != nil {
		return err
	}
	if e.moatSavings, err = e.meter.Float64Counter("gatewayllm.moat.savings_usd",
		metric.WithDescription("Per-request savings (baseline - actual). The moat KPI."),
		metric.WithUnit("USD")); err != nil {
		return err
	}
	if e.moatQuality, err = e.meter.Float64Histogram("gatewayllm.moat.quality_score",
		metric.WithDescription("Judge-assigned quality scores for routed requests")); err != nil {
		return err
	}
	if e.moatJudge, err = e.meter.Int64Counter("gatewayllm.moat.judge_passes",
		metric.WithDescription("Judge verdicts (result = pass | fail)")); err != nil {
		return err
	}
	if e.moatDecisions, err = e.meter.Int64Counter("gatewayllm.moat.routing_decisions",
		metric.WithDescription("Routing decisions: passthrough | overridden | retried")); err != nil {
		return err
	}
	return nil
}

// Shutdown flushes pending metrics and closes the exporter. Safe to
// call on a nil receiver so callers can defer it without checking.
func (e *Exporter) Shutdown(ctx context.Context) error {
	if e == nil || e.provider == nil {
		return nil
	}
	return e.provider.Shutdown(ctx)
}

// RecordRequest mirrors observability.RecordRequest so every LLM
// request emits OTLP metrics in lockstep with Prometheus.
func (e *Exporter) RecordRequest(ctx context.Context, model, provider string, statusCode int, latencyMS float64, promptTokens, completionTokens int, costUSD float64) {
	if e == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("model", model),
		attribute.String("provider", provider),
		attribute.String("status", statusClass(statusCode)),
	)
	e.requests.Add(ctx, 1, attrs)
	if latencyMS >= 0 {
		e.latencyMS.Record(ctx, latencyMS, metric.WithAttributes(
			attribute.String("model", model),
			attribute.String("provider", provider),
		))
	}
	if promptTokens > 0 {
		e.tokens.Add(ctx, int64(promptTokens), metric.WithAttributes(
			attribute.String("model", model),
			attribute.String("provider", provider),
			attribute.String("kind", "prompt"),
		))
	}
	if completionTokens > 0 {
		e.tokens.Add(ctx, int64(completionTokens), metric.WithAttributes(
			attribute.String("model", model),
			attribute.String("provider", provider),
			attribute.String("kind", "completion"),
		))
	}
	if costUSD > 0 {
		e.costUSD.Add(ctx, costUSD, metric.WithAttributes(
			attribute.String("model", model),
			attribute.String("provider", provider),
		))
	}
}

// MoatSample mirrors observability.MoatSample (kept independent so we
// don't take a backwards dependency).
type MoatSample struct {
	Alias        string
	ServedAlias  string
	Strategy     string
	Retried      bool
	OrgID        string
	BaselineUSD  float64
	ActualUSD    float64
	SavingsUSD   float64
	QualityScore *float64
	QualityPass  *bool
	Overridden   bool
}

// RecordMoat emits the smart-routing moat metrics. Same label set as
// Prometheus so OTLP and Prometheus dashboards stay 1:1.
func (e *Exporter) RecordMoat(ctx context.Context, s MoatSample) {
	if e == nil {
		return
	}
	alias := defaultLabel(s.Alias)
	served := defaultLabel(s.ServedAlias)
	if served == "unknown" {
		served = alias
	}
	strategy := defaultLabel(s.Strategy)
	org := s.OrgID
	if org == "" {
		org = "global"
	}
	retried := "false"
	if s.Retried {
		retried = "true"
	}

	full := metric.WithAttributes(
		attribute.String("alias", alias),
		attribute.String("served_alias", served),
		attribute.String("strategy", strategy),
		attribute.String("retried", retried),
		attribute.String("org_id", org),
	)
	costAttrs := metric.WithAttributes(
		attribute.String("alias", alias),
		attribute.String("served_alias", served),
		attribute.String("org_id", org),
	)

	e.moatRequests.Add(ctx, 1, full)
	if s.ActualUSD > 0 {
		e.moatActual.Add(ctx, s.ActualUSD, costAttrs)
	}
	if s.BaselineUSD > 0 {
		e.moatBaseline.Add(ctx, s.BaselineUSD, costAttrs)
	}
	if s.SavingsUSD > 0 {
		e.moatSavings.Add(ctx, s.SavingsUSD, costAttrs)
	}
	if s.QualityScore != nil {
		e.moatQuality.Record(ctx, *s.QualityScore, metric.WithAttributes(
			attribute.String("alias", alias),
		))
	}
	if s.QualityPass != nil {
		result := "fail"
		if *s.QualityPass {
			result = "pass"
		}
		e.moatJudge.Add(ctx, 1, metric.WithAttributes(
			attribute.String("alias", alias),
			attribute.String("result", result),
		))
	}
	decision := "passthrough"
	switch {
	case s.Retried:
		decision = "retried"
	case s.Overridden:
		decision = "overridden"
	}
	e.moatDecisions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("decision", decision),
	))
}

func statusClass(code int) string {
	switch {
	case code <= 0:
		return "unknown"
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

func defaultLabel(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
