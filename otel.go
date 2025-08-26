package pantryagent

import (
	"context"
	"errors"

	"github.com/joeshaw/envdecode"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	TracerNameMock    = "mock-coordinator"
	TracerNameBedrock = "bedrock-coordinator"
	TracerNameOllama  = "ollama-coordinator"
)

// OtelConfig is a configuration struct for the OpenTelemetry providers.
type OtelConfig struct {
	Endpoint       string `env:"OTEL_EXPORTER_OTLP_ENDPOINT,default=set-me"`
	Headers        string `env:"OTEL_EXPORTER_OTLP_HEADERS,default=set-me"`
	ServiceVersion string `env:"OTEL_SERVICE_VERSION,default=0.1.0"`
	ServiceName    string `env:"OTEL_SERVICE_NAME,default=pantry-agent"`
	DeployEnv      string `env:"OTEL_DEPLOY_ENV,default=development"`
}

type otelShutdown func(ctx context.Context) error

// Init initializes the OpenTelemetry SDK and returns a TracerProvider, MeterProvider, and shutdown function.
func InitOtel(ctx context.Context) (*trace.TracerProvider, *metric.MeterProvider, otelShutdown, error) {
	var cfg OtelConfig
	if err := envdecode.Decode(&cfg); err != nil {
		return nil, nil, nil, err
	}

	// Configure a new OTLP trace exporter using environment variables for sending data over gRPC
	traceExporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient())
	if err != nil {
		return nil, nil, nil, err
	}

	// Configure a new OTLP metric exporter using environment variables for sending data over gRPC
	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	// Create a new tracer provider with a batch span processor and the otlp exporter
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter))

	// Create a new meter provider with a periodic reader and the otlp exporter
	meterProvider := metric.NewMeterProvider(metric.WithReader(metric.NewPeriodicReader(metricExporter)))

	// Register the global Tracer and Meter providers
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	// Register the W3C trace context and baggage propagators so data is propagated across services/processes
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	shutdown := func(ctx context.Context) error {
		// Shutdown providers - they will handle shutting down their associated exporters
		err := errors.Join(
			tracerProvider.Shutdown(ctx),
			meterProvider.Shutdown(ctx),
		)

		if err != nil && err.Error() == "gRPC exporter is shutdown" {
			return nil
		}

		return err
	}

	return tracerProvider, meterProvider, shutdown, nil
}
