package telemetry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	// errorLogInterval is how often the same export error may be logged.
	errorLogInterval = time.Minute
	// defaultMetricInterval is the export interval when OTEL_METRIC_EXPORT_INTERVAL is not set: the stack's scrape interval.
	defaultMetricInterval = 15 * time.Second
)

// Options are the settings Setup takes from the application. Everything else comes from the standard OTEL_* variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES, OTEL_TRACES_SAMPLER, OTEL_SDK_DISABLED, ...).
type Options struct {
	// ServiceName is the service name when OTEL_SERVICE_NAME is not set; it defaults to DefaultServiceName.
	ServiceName string
	// ServiceVersion is recorded as service.version.
	ServiceVersion string
	// Environment is recorded as deployment.environment.name.
	Environment string
}

// defaultEndpoint is where Setup exports when no variable names an endpoint: the local collector, in plaintext. The SDK's own
// default is the same address over TLS, which a plaintext collector rejects, so a process started without an endpoint
// variable would export nothing. It is a variable so a test can point it at a collector it started.
var defaultEndpoint = "http://localhost:4317"

// endpointOrDefault returns the endpoint option to give an exporter whose per-signal variable is signalVariable: the default
// when neither OTEL_EXPORTER_OTLP_ENDPOINT nor the per-signal variable names an endpoint (the SDK ignores empty and blank
// values, so does this), and nothing otherwise, so the variables are read by the SDK as usual. Options override variables
// in the SDK, which is why the default is only passed when no variable is set.
func endpointOrDefault(signalVariable string) (endpoint string, useDefault bool) {
	for _, name := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", signalVariable} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return "", false
		}
	}
	return defaultEndpoint, true
}

// Setup installs the global tracer provider, meter provider and propagators (W3C trace context and baggage) and returns
// the function that flushes and stops them. Traces and metrics go to the OTLP/gRPC endpoint the standard variables name
// (default http://localhost:4317, plaintext). Setup does not dial: the exporters connect lazily and export in the background, so a missing
// collector never fails startup or slows a request; the SDK's errors are logged at most once a minute per distinct error.
// With OTEL_SDK_DISABLED=true it installs nothing and the returned function does nothing.
func Setup(ctx context.Context, opts Options) (shutdown func(context.Context) error, err error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		return func(context.Context) error { return nil }, nil
	}

	otel.SetErrorHandler(newErrorHandler(errorLogInterval, time.Now))
	res := newResource(ctx, opts)

	var traceOptions []otlptracegrpc.Option
	if endpoint, ok := endpointOrDefault("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); ok {
		traceOptions = append(traceOptions, otlptracegrpc.WithEndpointURL(endpoint))
	}
	traceExporter, err := otlptracegrpc.New(ctx, traceOptions...)
	if err != nil {
		return nil, fmt.Errorf("create the trace exporter: %w", err)
	}
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res))

	var metricOptions []otlpmetricgrpc.Option
	if endpoint, ok := endpointOrDefault("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"); ok {
		metricOptions = append(metricOptions, otlpmetricgrpc.WithEndpointURL(endpoint))
	}
	metricExporter, err := otlpmetricgrpc.New(ctx, metricOptions...)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create the metric exporter: %w", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, metricReaderOptions()...)),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	// The two flushes run side by side, so a slow trace flush cannot use up the deadline the metric flush needs.
	return func(ctx context.Context) error {
		var traceErr, metricErr error
		var wg sync.WaitGroup
		wg.Go(func() { traceErr = tracerProvider.Shutdown(ctx) })
		wg.Go(func() { metricErr = meterProvider.Shutdown(ctx) })
		wg.Wait()
		return errors.Join(traceErr, metricErr)
	}, nil
}

// newResource describes this process. The application's values are the defaults and the standard variables
// (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES) override them, because the detectors are applied in this order and a later
// one wins. There is no process detector: the collector turns every resource attribute into a label on every metric series,
// and process.command_args or process.pid are not labels anyone wants.
func newResource(ctx context.Context, opts Options) *resource.Resource {
	defaults := []attribute.KeyValue{attribute.String("service.name", cmp.Or(opts.ServiceName, DefaultServiceName))}
	if opts.ServiceVersion != "" {
		defaults = append(defaults, attribute.String("service.version", opts.ServiceVersion))
	}
	if opts.Environment != "" {
		defaults = append(defaults, attribute.String("deployment.environment.name", opts.Environment))
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(defaults...),
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		// New always returns a resource: a partly detected one, or one whose schema URLs conflicted, is still usable.
		otel.Handle(err)
	}
	return res
}

// metricReaderOptions sets the export interval to defaultMetricInterval unless OTEL_METRIC_EXPORT_INTERVAL says otherwise;
// the SDK's own default is a minute, which would leave the dashboards behind the 15 second scrape.
func metricReaderOptions() []sdkmetric.PeriodicReaderOption {
	if strings.TrimSpace(os.Getenv("OTEL_METRIC_EXPORT_INTERVAL")) != "" {
		return nil
	}
	return []sdkmetric.PeriodicReaderOption{sdkmetric.WithInterval(defaultMetricInterval)}
}
