package telemetry

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
)

// TestMain clears the variables a developer's shell may export, which would turn Setup off or point it away from the
// collectors these tests start. A test that needs one sets it with t.Setenv.
func TestMain(m *testing.M) {
	for _, name := range []string{
		"OTEL_SDK_DISABLED",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	} {
		_ = os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

// collector records what is exported to it.
type collector struct {
	mu sync.Mutex
	// holdTraces, when set, makes the trace Export block until it is closed.
	holdTraces chan struct{}
	traces     []*coltracepb.ExportTraceServiceRequest
	metrics    []*colmetricspb.ExportMetricsServiceRequest
}

func (c *collector) traceRequests() []*coltracepb.ExportTraceServiceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*coltracepb.ExportTraceServiceRequest(nil), c.traces...)
}

func (c *collector) metricRequests() []*colmetricspb.ExportMetricsServiceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*colmetricspb.ExportMetricsServiceRequest(nil), c.metrics...)
}

type traceService struct {
	coltracepb.UnimplementedTraceServiceServer
	c *collector
}

func (s *traceService) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	if s.c.holdTraces != nil {
		select {
		case <-s.c.holdTraces:
		case <-ctx.Done():
		}
	}
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	s.c.traces = append(s.c.traces, req)
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

type metricsService struct {
	colmetricspb.UnimplementedMetricsServiceServer
	c *collector
}

func (s *metricsService) Export(_ context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	s.c.metrics = append(s.c.metrics, req)
	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}

// startCollector serves OTLP/gRPC on a free port and returns the collector and the endpoint URL to export to.
func startCollector(t *testing.T) (*collector, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	c := &collector{}
	server := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(server, &traceService{c: c})
	colmetricspb.RegisterMetricsServiceServer(server, &metricsService{c: c})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return c, "http://" + listener.Addr().String()
}

// resetGlobals puts the global providers, propagator and error handler back to no-ops when the test ends, because Setup
// replaces them for the whole process.
func resetGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))
	})
}

func resourceAttribute(attrs []*commonpb.KeyValue, key string) (string, bool) {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue(), true
		}
	}
	return "", false
}

func shutdownWithin(t *testing.T, shutdown func(context.Context) error, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return shutdown(ctx)
}

func TestSetup_ExportsSpansAndMetricsOnShutdownWithTheServiceResource(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{ServiceVersion: "1.2.3", Environment: "test"})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(context.Background(), "unit-of-work")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("test.requests")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	require.NoError(t, shutdownWithin(t, shutdown, 5*time.Second), "shutdown flushes both providers")

	traces := c.traceRequests()
	require.Len(t, traces, 1)
	resourceSpans := traces[0].GetResourceSpans()
	require.Len(t, resourceSpans, 1)
	attrs := resourceSpans[0].GetResource().GetAttributes()
	name, _ := resourceAttribute(attrs, "service.name")
	version, _ := resourceAttribute(attrs, "service.version")
	environment, _ := resourceAttribute(attrs, "deployment.environment.name")
	assert.Equal(t, "fupi", name, "the service name defaults to fupi: the dashboard filters on it")
	assert.Equal(t, "1.2.3", version)
	assert.Equal(t, "test", environment)
	for _, key := range []string{"process.pid", "process.command_args", "process.owner"} {
		_, found := resourceAttribute(attrs, key)
		assert.False(t, found, "%s would become a label on every metric series", key)
	}
	assert.Equal(t, "unit-of-work", resourceSpans[0].GetScopeSpans()[0].GetSpans()[0].GetName())

	var metricNames []string
	for _, request := range c.metricRequests() {
		for _, resourceMetrics := range request.GetResourceMetrics() {
			metricName, _ := resourceAttribute(resourceMetrics.GetResource().GetAttributes(), "service.name")
			assert.Equal(t, "fupi", metricName)
			for _, scope := range resourceMetrics.GetScopeMetrics() {
				for _, m := range scope.GetMetrics() {
					metricNames = append(metricNames, m.GetName())
				}
			}
		}
	}
	assert.Contains(t, metricNames, "test.requests")
}

// The standard variables win over the defaults the application passes in.
func TestSetup_TheEnvironmentOverridesTheDefaults(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_SERVICE_NAME", "billing")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=staging,custom.team=payments")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{ServiceVersion: "1.2.3", Environment: "test"})
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(context.Background(), "work")
	span.End()
	require.NoError(t, shutdownWithin(t, shutdown, 5*time.Second))

	traces := c.traceRequests()
	require.Len(t, traces, 1)
	attrs := traces[0].GetResourceSpans()[0].GetResource().GetAttributes()
	name, _ := resourceAttribute(attrs, "service.name")
	environment, _ := resourceAttribute(attrs, "deployment.environment.name")
	team, _ := resourceAttribute(attrs, "custom.team")
	version, _ := resourceAttribute(attrs, "service.version")
	assert.Equal(t, "billing", name)
	assert.Equal(t, "staging", environment)
	assert.Equal(t, "payments", team)
	assert.Equal(t, "1.2.3", version, "what the environment does not set keeps the application's value")
}

// The worker passes its own service name; the API passes none and keeps "fupi". The variable still wins over both.
func TestSetup_TheApplicationsServiceNameIsTheDefaultAndTheEnvironmentStillWins(t *testing.T) {
	for name, tc := range map[string]struct {
		env, option, want string
	}{
		"no option, no variable": {"", "", "fupi"},
		"option only":            {"", "fupi-worker", "fupi-worker"},
		"variable beats option":  {"billing", "fupi-worker", "billing"},
	} {
		t.Run(name, func(t *testing.T) {
			c, endpoint := startCollector(t)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
			t.Setenv("OTEL_SERVICE_NAME", tc.env)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
			resetGlobals(t)

			shutdown, err := Setup(context.Background(), Options{ServiceName: tc.option})
			require.NoError(t, err)
			_, span := otel.Tracer("test").Start(context.Background(), "work")
			span.End()
			require.NoError(t, shutdownWithin(t, shutdown, 5*time.Second))

			traces := c.traceRequests()
			require.Len(t, traces, 1)
			got, _ := resourceAttribute(traces[0].GetResourceSpans()[0].GetResource().GetAttributes(), "service.name")
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSetup_DisabledInstallsNothing(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_SDK_DISABLED", "true")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)

	_, isSDK := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	assert.False(t, isSDK, "the global provider stays the no-op")
	_, span := otel.Tracer("test").Start(context.Background(), "work")
	assert.False(t, span.IsRecording())
	span.End()
	require.NoError(t, shutdownWithin(t, shutdown, time.Second))
	assert.Empty(t, c.traceRequests())
	assert.Empty(t, c.metricRequests())
}

// A collector that is not there must not fail startup, slow the work being traced, or hold shutdown past its deadline.
func TestSetup_DoesNotFailOrBlockWithoutACollector(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	resetGlobals(t)
	captureDefaultLogger(t)

	start := time.Now()
	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second, "Setup must not dial the collector")

	tracer := otel.Tracer("test")
	begin := time.Now()
	for range 2000 {
		_, span := tracer.Start(context.Background(), "work")
		span.End()
	}
	assert.Less(t, time.Since(begin), time.Second, "recording spans never waits for the exporter")

	done := make(chan struct{})
	go func() {
		_ = shutdownWithin(t, shutdown, 500*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown must honour its deadline when the collector is unreachable")
	}
}

func TestSetup_PropagatesW3CTraceContextAndBaggage(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdownWithin(t, shutdown, 200*time.Millisecond) })

	fields := otel.GetTextMapPropagator().Fields()
	assert.Contains(t, fields, "traceparent")
	assert.Contains(t, fields, "baggage")
}

func TestSetup_InstallsTheRateLimitedErrorHandler(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	resetGlobals(t)
	buf := captureDefaultLogger(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdownWithin(t, shutdown, 200*time.Millisecond) })

	for range 5 {
		otel.Handle(errors.New("export failed: connection refused"))
	}

	assert.Equal(t, 1, strings.Count(buf.String(), "opentelemetry error"))
}

func TestMetricReaderOptions_DefaultsToTheStacksScrapeInterval(t *testing.T) {
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "")
	assert.Len(t, metricReaderOptions(), 1, "unset (or empty) means the 15 second default")

	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "1000")
	assert.Empty(t, metricReaderOptions(), "a set variable is left to the SDK")
}

func TestSetup_HonoursTheMetricExportIntervalVariable(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "100")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdownWithin(t, shutdown, 2*time.Second) })
	counter, err := otel.Meter("test").Int64Counter("test.interval")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	assert.Eventually(t, func() bool { return len(c.metricRequests()) > 0 }, 3*time.Second, 20*time.Millisecond,
		"metrics must arrive without a shutdown when the interval is 100 ms")
}

// useDefaultEndpoint points Setup's default at endpoint and clears the variables that would override it.
func useDefaultEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	previous := defaultEndpoint
	defaultEndpoint = endpoint
	t.Cleanup(func() { defaultEndpoint = previous })
	for _, name := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"} {
		t.Setenv(name, "")
	}
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
}

// exportOneSpanAndOneMetric runs Setup, records a span and a counter, and flushes.
func exportOneSpanAndOneMetric(t *testing.T) {
	t.Helper()
	resetGlobals(t)
	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(context.Background(), "unit-of-work")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("test.requests")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)
	require.NoError(t, shutdownWithin(t, shutdown, 5*time.Second))
}

// The SDK's own default is localhost:4317 over TLS, which the local collector (plaintext) rejects with "first record does
// not look like a TLS handshake": a process run on the host without OTEL_EXPORTER_OTLP_ENDPOINT would export nothing. The
// documented default is http://localhost:4317.
func TestSetup_WithNoEndpointVariableItExportsToTheDocumentedPlaintextDefault(t *testing.T) {
	c, endpoint := startCollector(t)
	useDefaultEndpoint(t, endpoint)

	exportOneSpanAndOneMetric(t)

	assert.Len(t, c.traceRequests(), 1, "spans reach the default endpoint")
	assert.NotEmpty(t, c.metricRequests(), "and so do metrics")
}

func TestSetup_TheEndpointVariablesStillWinOverTheDefault(t *testing.T) {
	c, endpoint := startCollector(t)
	useDefaultEndpoint(t, "http://127.0.0.1:1")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)

	exportOneSpanAndOneMetric(t)

	assert.Len(t, c.traceRequests(), 1)
	assert.NotEmpty(t, c.metricRequests())
}

// A per-signal variable names the endpoint of that signal only; the other signal has none and gets the default.
func TestSetup_AnEndpointForOneSignalLeavesTheOtherSignalOnTheDefault(t *testing.T) {
	tracesCollector, tracesEndpoint := startCollector(t)
	defaultCollector, defaultAddress := startCollector(t)
	useDefaultEndpoint(t, defaultAddress)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", tracesEndpoint)

	exportOneSpanAndOneMetric(t)

	assert.Len(t, tracesCollector.traceRequests(), 1, "traces go where their own variable says")
	assert.Empty(t, tracesCollector.metricRequests())
	assert.NotEmpty(t, defaultCollector.metricRequests(), "metrics have no variable and take the default")
	assert.Empty(t, defaultCollector.traceRequests())
}

// A trace flush that hangs must not use up the deadline the metric flush needs.
func TestSetup_ShutdownFlushesMetricsWhileTheTraceFlushHangs(t *testing.T) {
	c, endpoint := startCollector(t)
	release := make(chan struct{})
	c.holdTraces = release
	t.Cleanup(func() { close(release) })
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	resetGlobals(t)
	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(context.Background(), "unit-of-work")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("test.requests")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	_ = shutdownWithin(t, shutdown, time.Second)

	assert.NotEmpty(t, c.metricRequests(), "the metrics were flushed although the trace export never returned")
}
