// SDK-backed OTLP exporter. Replaces the hand-rolled JSON-over-HTTP
// exporter in otel.go for any operator who wants real OpenTelemetry
// semantics (Datadog APM, Honeycomb, Tempo, Jaeger, etc.).
//
// We deliberately keep the legacy `otel` callback in place so existing
// configs don't break; it logs a deprecation warning and continues to
// work for the OTLP-HTTP collectors that already accept its hand-rolled
// payload.
package callbacks

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

// OTLPCallback wraps a tracer provider whose exporter is configured by
// `protocol` (http or grpc). One callback per gateway process is enough;
// multiple OTLP destinations can be configured with multiple entries.
type OTLPCallback struct {
	name    string
	tp      *sdktrace.TracerProvider
	tracer  trace.Tracer
	closeMu sync.Mutex
	closed  bool
}

// NewOTLP constructs an OTLPCallback. protocol may be "http" (default,
// uses otlptracehttp) or "grpc" (uses otlptracegrpc). headers are
// applied to every export request and are the canonical place to put
// vendor auth tokens (e.g. Honeycomb's "x-honeycomb-team" or Datadog's
// "dd-api-key" via the OTLP Datadog Agent intake).
func NewOTLP(endpoint, protocol, serviceName string, headers map[string]string, timeout time.Duration) (*OTLPCallback, error) {
	if endpoint == "" {
		return nil, errors.New("otel_otlp: endpoint is required")
	}
	if serviceName == "" {
		serviceName = "gateway-llm"
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var (
		exp otlptrace.Client
		err error
	)
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "http", "http/protobuf":
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpointURL(endpoint),
			otlptracehttp.WithTimeout(timeout),
		}
		if len(headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(headers))
		}
		exp = otlptracehttp.NewClient(opts...)
	case "grpc":
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpointURL(endpoint),
			otlptracegrpc.WithTimeout(timeout),
		}
		if len(headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(headers))
		}
		exp = otlptracegrpc.NewClient(opts...)
	default:
		return nil, fmt.Errorf("otel_otlp: unsupported protocol %q (expected http or grpc)", protocol)
	}

	exporter, err := otlptrace.New(ctx, exp)
	if err != nil {
		return nil, fmt.Errorf("otel_otlp: starting exporter: %w", err)
	}

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
		sdkresource.WithProcess(),
		sdkresource.WithOS(),
		sdkresource.WithHost(),
	)
	if err != nil {
		// Resource creation is best-effort; fall back to a minimal
		// resource rather than failing callback startup outright.
		res = sdkresource.NewSchemaless(semconv.ServiceName(serviceName))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res),
	)

	return &OTLPCallback{
		name:   "otel_otlp:" + endpoint,
		tp:     tp,
		tracer: tp.Tracer("github.com/gateway-llm/gateway-llm"),
	}, nil
}

func (o *OTLPCallback) Name() string { return o.name }

func (o *OTLPCallback) Send(ctx context.Context, event RequestEvent) error {
	if o == nil {
		return nil
	}
	o.closeMu.Lock()
	closed := o.closed
	o.closeMu.Unlock()
	if closed {
		return nil
	}

	// We synthesise a span around the already-completed request using
	// the trace/span IDs the gateway generated upstream. This way the
	// SDK's per-span sampling and OTLP encoding take over without us
	// touching the hot path.
	startCtx, sc := withRemoteContext(ctx, event.TraceID, event.SpanID)
	startTime := event.Timestamp
	if startTime.IsZero() {
		startTime = time.Now().Add(-time.Duration(event.DurationMS) * time.Millisecond)
	}
	endTime := startTime.Add(time.Duration(event.DurationMS) * time.Millisecond)

	spanName := event.Endpoint
	if spanName == "" {
		spanName = "llm.completion"
	}

	_, span := o.tracer.Start(startCtx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithTimestamp(startTime),
	)
	span.SetAttributes(
		attribute.String("gen_ai.system", event.Provider),
		attribute.String("gen_ai.request.model", event.ProviderModel),
		attribute.String("gateway_llm.model_alias", event.ModelAlias),
		attribute.Int("gen_ai.usage.prompt_tokens", event.PromptTokens),
		attribute.Int("gen_ai.usage.completion_tokens", event.CompletionTokens),
		attribute.Int("gen_ai.usage.total_tokens", event.TotalTokens),
		attribute.Float64("gateway_llm.cost_usd", event.CostUSD),
		attribute.Int("http.response.status_code", event.Status),
	)
	if event.APIKeyID != "" {
		span.SetAttributes(attribute.String("gateway_llm.api_key_id", event.APIKeyID))
	}
	if event.UserID != "" {
		span.SetAttributes(attribute.String("enduser.id", event.UserID))
	}
	if event.TeamID != "" {
		span.SetAttributes(attribute.String("gateway_llm.team_id", event.TeamID))
	}
	if event.Status >= 400 {
		span.SetStatus(codes.Error, http4xxOr5xx(event.Status))
		if event.ErrorMessage != "" {
			span.SetAttributes(attribute.String("error.message", event.ErrorMessage))
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End(trace.WithTimestamp(endTime))
	_ = sc
	return nil
}

func http4xxOr5xx(status int) string {
	if status >= 500 {
		return "upstream error"
	}
	return "client error"
}

func (o *OTLPCallback) Close() error {
	if o == nil {
		return nil
	}
	o.closeMu.Lock()
	defer o.closeMu.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return o.tp.Shutdown(ctx)
}

// withRemoteContext synthesises a SpanContext from the gateway's existing
// trace/span IDs so our exported spans link up with whatever caller-side
// instrumentation generated them. Falls back to ctx unchanged when the
// IDs are not parseable as 32/16 hex chars.
func withRemoteContext(ctx context.Context, traceID, spanID string) (context.Context, trace.SpanContext) {
	tid, err := decodeTraceID(traceID)
	if err != nil {
		return ctx, trace.SpanContext{}
	}
	sid, err := decodeSpanID(spanID)
	if err != nil {
		return ctx, trace.SpanContext{}
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	return trace.ContextWithSpanContext(ctx, sc), sc
}

func decodeTraceID(s string) (trace.TraceID, error) {
	if len(s) != 32 {
		return trace.TraceID{}, errors.New("invalid trace id length")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return trace.TraceID{}, err
	}
	var out trace.TraceID
	copy(out[:], b)
	return out, nil
}

func decodeSpanID(s string) (trace.SpanID, error) {
	if len(s) != 16 {
		return trace.SpanID{}, errors.New("invalid span id length")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return trace.SpanID{}, err
	}
	var out trace.SpanID
	copy(out[:], b)
	return out, nil
}
