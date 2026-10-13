package platform

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	testTraceID     = "4bf92f3577b34da6a3ce929d0e0e4736"
	testParentID    = "00f067aa0ba902b7"
	testTraceParent = "00-" + testTraceID + "-" + testParentID + "-01"
)

func TestTraceSpansHTTPAndGRPCCalls(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewGRPCServer(logger, TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := Dial(listener.Addr().String(), TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	app := NewApp(logger)
	app.Get("/trace-check", func(c fiber.Ctx) error {
		response, callErr := healthpb.NewHealthClient(connection).Check(
			c.Context(),
			&healthpb.HealthCheckRequest{},
		)
		if callErr != nil {
			return callErr
		}
		return c.SendString(response.GetStatus().String())
	})

	request := httptest.NewRequest(http.MethodGet, "/trace-check", nil)
	request.Header.Set("traceparent", testTraceParent)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("http status: %d", response.StatusCode)
	}

	traceID, err := trace.TraceIDFromHex(testTraceID)
	if err != nil {
		t.Fatal(err)
	}
	parentID, err := trace.SpanIDFromHex(testParentID)
	if err != nil {
		t.Fatal(err)
	}
	clientSpan := findSpan(t, recorder.Ended(), traceID, "grpc.health.v1.Health/Check", trace.SpanKindClient)
	serverSpan := findSpan(t, recorder.Ended(), traceID, "grpc.health.v1.Health/Check", trace.SpanKindServer)
	httpSpan := findSpan(t, recorder.Ended(), traceID, "GET /trace-check", trace.SpanKindServer)

	if httpSpan.SpanContext().TraceID() != traceID || httpSpan.Parent().SpanID() != parentID {
		t.Fatalf("http span parent: %s", httpSpan.Parent().SpanID())
	}
	if clientSpan.Parent().SpanID() != httpSpan.SpanContext().SpanID() {
		t.Fatalf("grpc client parent: %s", clientSpan.Parent().SpanID())
	}
	if serverSpan.Parent().SpanID() != clientSpan.SpanContext().SpanID() {
		t.Fatalf("grpc server parent: %s", serverSpan.Parent().SpanID())
	}
}

func TestSetupTracingDisabledByDefault(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "")
	shutdown, err := SetupTracing(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	shutdown()
}

func TestSetupTracingRejectsUnknownExporter(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "carrier-pigeon")
	if _, err := SetupTracing(context.Background(), "test"); err == nil {
		t.Fatal("unknown exporter accepted")
	}
}

func findSpan(
	t *testing.T,
	spans []sdktrace.ReadOnlySpan,
	traceID trace.TraceID,
	name string,
	kind trace.SpanKind,
) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.SpanContext().TraceID() == traceID && span.Name() == name && span.SpanKind() == kind {
			return span
		}
	}
	t.Fatalf("span %q with kind %s not recorded in trace %s: %v", name, kind, traceID, spanNames(spans))
	return nil
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}
