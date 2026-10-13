package platform

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/m1chlcz/microservices-reference/internal/platform"
	tracingShutdownTime = 5 * time.Second
)

// SetupTracing configures OpenTelemetry from the environment. Spans of the
// REST edge and of the internal gRPC calls are exported when
// OTEL_TRACES_EXPORTER names an exporter: "none" (the default), "stdout" or
// "otlp". The OTLP exporter reads its endpoint and transport settings from the
// standard OTEL_EXPORTER_OTLP_* variables. The service name defaults to the
// service argument and can be overridden with OTEL_SERVICE_NAME. Call
// SetupTracing before the HTTP app and the gRPC servers and clients are
// created, so their instrumentations use the configured provider. The
// returned function flushes the pending spans.
func SetupTracing(ctx context.Context, serviceName string) (func(), error) {
	exporterName := strings.ToLower(EnvOr("OTEL_TRACES_EXPORTER", "none"))
	switch exporterName {
	case "none":
		return func() {}, nil
	case "stdout":
		exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("stdout trace exporter: %w", err)
		}
		return installTracerProvider(exporter, serviceName)
	case "otlp":
		exporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otlp trace exporter: %w", err)
		}
		return installTracerProvider(exporter, serviceName)
	default:
		return nil, fmt.Errorf("unsupported OTEL_TRACES_EXPORTER %q: use none, stdout or otlp", exporterName)
	}
}

func installTracerProvider(exporter sdktrace.SpanExporter, serviceName string) (func(), error) {
	traceResource, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(EnvOr("OTEL_SERVICE_NAME", serviceName))),
	)
	if err != nil {
		return nil, fmt.Errorf("trace resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(traceResource),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tracingShutdownTime)
		defer cancel()
		_ = provider.Shutdown(shutdownCtx)
	}, nil
}

// tracingMiddleware starts a server span for every incoming HTTP request. It
// reads the caller trace context from the request headers and stores the
// tracing context in the Fiber context, so outgoing gRPC calls continue the
// same trace.
func tracingMiddleware() fiber.Handler {
	tracer := otel.Tracer(instrumentationName)
	return func(c fiber.Ctx) error {
		ctx := otel.GetTextMapPropagator().Extract(c.Context(), requestCarrier{ctx: c})
		ctx, span := tracer.Start(ctx, c.Method()+" "+c.Path(),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Method()),
				semconv.URLPath(c.Path()),
			),
		)
		defer span.End()
		c.SetContext(ctx)
		err := c.Next()
		status := c.Response().StatusCode()
		span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		switch {
		case err != nil:
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		case status >= http.StatusInternalServerError:
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		return err
	}
}

// requestCarrier adapts the Fiber request and response headers to the
// OpenTelemetry text map carrier.
type requestCarrier struct {
	ctx fiber.Ctx
}

// Get returns the value of the request header key.
func (carrier requestCarrier) Get(key string) string {
	return carrier.ctx.Get(key)
}

// Set writes a response header.
func (carrier requestCarrier) Set(key, value string) {
	carrier.ctx.Set(key, value)
}

// Keys returns every request header name.
func (carrier requestCarrier) Keys() []string {
	headers := carrier.ctx.GetReqHeaders()
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	return keys
}
