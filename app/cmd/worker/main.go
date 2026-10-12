// Command worker runs the Fupi background worker: the outbox relay, one leader-elected process that delivers the
// transactional outbox (ADR-0011) to Kafka with at-least-once delivery, and the URL expiry job, also leader-elected. The
// API never talks to Kafka (ADR-0015).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/sanctumlabs/fupi/app/api/probes"
	"github.com/sanctumlabs/fupi/app/config"
	kafkaadapter "github.com/sanctumlabs/fupi/app/internal/adapters/kafka"
	outboxdatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/outbox"
	urldatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/url"
	"github.com/sanctumlabs/fupi/app/internal/application/outbox"
	"github.com/sanctumlabs/fupi/app/internal/application/urlexpiry"
	"github.com/sanctumlabs/fupi/app/pkg"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)

const (
	// serviceName is the worker's service name unless OTEL_SERVICE_NAME says otherwise; the API's is "fupi".
	serviceName = "fupi-worker"

	// livenessMaxAge is how long the relay loop may go without completing a cycle before liveness fails.
	livenessMaxAge = 30 * time.Second

	// The relay's own pacing; the settings in the environment are the ones an operator is expected to change.
	backlogInterval = 5 * time.Second
	purgeInterval   = 10 * time.Minute
	purgeBatch      = 1000
	backoffMin      = 200 * time.Millisecond
	backoffMax      = 10 * time.Second

	// The expiry job's advisory lock and the name of its lease connection in pg_stat_activity.
	expiryLeaseKey             = "fupi.url.expiry"
	expiryLeaseApplicationName = "fupi-url-expiry-lease"
)

// healthcheckTimeout bounds the container health probe. It is a variable so a test can shorten it.
var healthcheckTimeout = 2 * time.Second

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(os.LookupEnv, os.Stdout))
	}

	dotenvErr := godotenv.Load()

	cfg, err := config.LoadWorker(os.LookupEnv)
	// The logger is installed once the configuration is read, so LOG_LEVEL and LOG_FORMAT apply. On a configuration error
	// LoadWorker still returns the default logging settings, so the error itself is logged as JSON like everything else.
	slog.SetDefault(telemetry.NewLogger(os.Stdout, cfg.Logging.Format, cfg.Logging.Level, telemetry.ServiceName(serviceName)))
	if dotenvErr != nil {
		slog.Warn("no .env file found, relying on the environment", "error", dotenvErr)
	}
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	for _, warning := range cfg.Warnings {
		slog.Warn(warning)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		// Hand signal handling back to the runtime as soon as the first signal arrives, so a second Ctrl-C ends a
		// stuck shutdown immediately instead of being swallowed.
		stop()
	}()

	err = run(ctx, cfg)
	stop()
	if err != nil {
		slog.Error("the worker stopped", "error", err)
		os.Exit(1)
	}
}

// healthcheck probes the worker running on this host and returns the process exit code: 0 when GET /health answers 200,
// 1 otherwise. It reads only the health settings, so the container's health check needs no database or Kafka settings and
// works in an image that has no shell, curl or wget.
func healthcheck(lookup config.Lookup, out io.Writer) int {
	settings, err := config.LoadWorkerHealth(lookup)
	if err != nil {
		_, _ = fmt.Fprintf(out, "healthcheck: invalid configuration: %v\n", err)
		return 1
	}

	target := "http://" + net.JoinHostPort(probeHost(settings.Host), strconv.Itoa(settings.Port)) + probes.LivePath
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		_, _ = fmt.Fprintf(out, "healthcheck: %v\n", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(out, "healthcheck: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(out, "healthcheck: %s answered %d\n", target, resp.StatusCode)
		return 1
	}
	_, _ = fmt.Fprintln(out, "healthcheck: ok")
	return 0
}

// probeHost turns a wildcard bind address, which cannot be dialed, into the loopback address that reaches it.
func probeHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}

func relayConfig(settings config.OutboxSettings) outbox.Config {
	return outbox.Config{
		PollInterval:    settings.PollInterval,
		BatchSize:       settings.BatchSize,
		MaxAttempts:     settings.MaxAttempts,
		StandbyInterval: settings.StandbyInterval,
		Retention:       settings.Retention,
		BacklogInterval: backlogInterval,
		PurgeInterval:   purgeInterval,
		PurgeBatch:      purgeBatch,
		BackoffMin:      backoffMin,
		BackoffMax:      backoffMax,
	}
}

func expiryConfig(cfg config.Worker) urlexpiry.Config {
	return urlexpiry.Config{
		Interval:        cfg.Expiry.Interval,
		StandbyInterval: cfg.Outbox.StandbyInterval,
		BatchSize:       cfg.Expiry.BatchSize,
	}
}

// run builds the relay from cfg and relays until ctx is cancelled, then finishes the batch in flight, flushes telemetry and
// closes its connections. Postgres is required: failing to reach it is an error. Kafka is not: the producer connects
// lazily, an unreachable Kafka only makes the relay back off, and readiness reports it as down.
func run(ctx context.Context, cfg config.Worker) error {
	// The deferred flush covers the early returns; the normal path flushes right after the relay stops (below).
	flushTelemetry := telemetry.Start(ctx, telemetry.Options{ServiceName: serviceName, ServiceVersion: pkg.Version, Environment: cfg.Environment})
	defer flushTelemetry()

	dbClient, err := postgres.NewPostgresClient(cfg.Database.Postgres)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer dbClient.Close()

	producer, err := kafka.NewProducer(kafka.Config{
		Brokers:        cfg.Kafka.Brokers,
		ClientID:       cfg.Kafka.ClientID,
		PublishTimeout: cfg.Kafka.PublishTimeout,
	})
	if err != nil {
		return fmt.Errorf("create the kafka producer: %w", err)
	}
	publisher := kafkaadapter.NewEventPublisher(producer)
	defer publisher.Close()

	store, err := outboxdatastore.NewAdapter(dbClient, postgres.ConnectionString(cfg.Database.Postgres), cfg.Database.OperationTimeout)
	if err != nil {
		return fmt.Errorf("create the outbox datastore: %w", err)
	}
	relay, err := outbox.NewRelay(store, publisher, relayConfig(cfg.Outbox))
	if err != nil {
		return fmt.Errorf("create the outbox relay: %w", err)
	}

	// The expiry job elects its own leader, on its own lock, so it can run on a different instance than the relay.
	expiryLeaser, err := outboxdatastore.NewLeaser(postgres.ConnectionString(cfg.Database.Postgres), expiryLeaseKey, expiryLeaseApplicationName,
		cfg.Database.OperationTimeout, outboxdatastore.DefaultLeaseIdleTimeout)
	if err != nil {
		return fmt.Errorf("create the url expiry leaser: %w", err)
	}
	urls := urldatastore.NewUrlDatastoreAdapter(dbClient, database.Config{
		OperationTimeout: cfg.Database.OperationTimeout,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	})
	expiry, err := urlexpiry.NewJob(expiryLeaser, urls, expiryConfig(cfg))
	if err != nil {
		return fmt.Errorf("create the url expiry job: %w", err)
	}

	registry := health.NewRegistry(health.DefaultCheckTimeout)
	registry.Add(health.Check{Name: "postgres", Required: true, Fn: dbClient.HealthCheck})
	registry.Add(health.Check{Name: "kafka", Required: true, Fn: publisher.Ping})

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Health.Host, strconv.Itoa(cfg.Health.Port)))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Health.Port, err)
	}
	healthServer := &http.Server{
		Handler:           probes.NewHTTPHandler(registry, func() bool { return relay.Healthy(livenessMaxAge) }),
		ReadHeaderTimeout: 5 * time.Second,
	}
	healthErr := make(chan error, 1)
	go func() { healthErr <- healthServer.Serve(listener) }()
	slog.Info("worker health listening", "port", cfg.Health.Port)

	runCtx, stopRelay := context.WithCancel(ctx)
	defer stopRelay()
	relayDone := make(chan error, 1)
	go func() { relayDone <- relay.Run(runCtx) }()
	expiryDone := make(chan error, 1)
	go func() { expiryDone <- expiry.Run(runCtx) }()

	var runErr error
	relayExited := false
	select {
	case <-ctx.Done():
	case err := <-healthErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("the health listener failed: %w", err)
		}
	case err := <-relayDone:
		relayExited = true
		runErr = fmt.Errorf("the relay stopped unexpectedly: %w", err)
	}

	// Shutdown: readiness turns to 503, then the relay finishes the batch it has in flight (bounded by the publish timeout).
	registry.SetDraining()
	slog.Info("shutting down the worker", "timeout", cfg.ShutdownTimeout.String())
	stopRelay()
	stopped, stopWaiting := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer stopWaiting()
	if !relayExited {
		select {
		case <-relayDone:
		case <-stopped.Done():
			runErr = errors.Join(runErr, fmt.Errorf("the relay did not stop within %s", cfg.ShutdownTimeout))
		}
	}
	select {
	case <-expiryDone:
	case <-stopped.Done():
		runErr = errors.Join(runErr, fmt.Errorf("the url expiry job did not stop within %s", cfg.ShutdownTimeout))
	}

	// The relay has stopped or given up: export the last spans and metrics now, before the connections close.
	flushTelemetry()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		slog.WarnContext(shutdownCtx, "closing the health listener", "error", err)
	}
	return runErr
}
