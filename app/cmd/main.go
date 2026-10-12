// Command fupi runs the Fupi HTTP API: the Identity and URL bounded contexts.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	apimiddleware "github.com/sanctumlabs/fupi/app/api/middleware"
	"github.com/sanctumlabs/fupi/app/api/probes"
	identityapi "github.com/sanctumlabs/fupi/app/api/v1/identity"
	urlapi "github.com/sanctumlabs/fupi/app/api/v1/url"
	"github.com/sanctumlabs/fupi/app/config"
	"github.com/sanctumlabs/fupi/app/internal/adapters/geolocation"
	"github.com/sanctumlabs/fupi/app/internal/adapters/jwtauth"
	kafkaadapter "github.com/sanctumlabs/fupi/app/internal/adapters/kafka"
	"github.com/sanctumlabs/fupi/app/internal/adapters/notifications"
	postgresrepo "github.com/sanctumlabs/fupi/app/internal/adapters/postgres"
	identitydatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/identity"
	urldatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/url"
	identityapp "github.com/sanctumlabs/fupi/app/internal/application/identity"
	urlapp "github.com/sanctumlabs/fupi/app/internal/application/url"
	"github.com/sanctumlabs/fupi/app/pkg"
	cacheredis "github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/sanctumlabs/fupi/app/pkg/jwt"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)

const (
	baseURI = "/api/v1/fupi"

	// redisStartupPingTimeout bounds the one ping that only decides which startup line is logged.
	redisStartupPingTimeout = 2 * time.Second

	// urlAccessedTopic is where redirects publish URLAccessed; urlAccessedBuffer is how many visits may wait for the
	// publisher before new ones are dropped.
	urlAccessedTopic  = "url.accessed"
	urlAccessedBuffer = 1024
)

// healthcheckTimeout bounds the container health probe. It is a variable so a test can shorten it.
var healthcheckTimeout = 2 * time.Second

// @title                      Fupi API
// @version                    2.0
// @description                Shortens urls, redirects visitors to them and manages the accounts that own them.
// @BasePath                   /
// @securityDefinitions.apikey BearerAuth
// @in                         header
// @name                       Authorization
// @description                Send "Bearer <access token>", the token returned by login.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(os.LookupEnv, os.Stdout))
	}

	dotenvErr := godotenv.Load()

	cfg, err := config.Load(os.LookupEnv)
	// The logger is installed once the configuration is read, so LOG_LEVEL and LOG_FORMAT apply. On a configuration error
	// Load still returns the default logging settings, so the error itself is logged as JSON like everything else.
	slog.SetDefault(telemetry.NewLogger(os.Stdout, cfg.Logging.Format, cfg.Logging.Level, telemetry.ServiceName(telemetry.DefaultServiceName)))
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
		// stuck drain immediately instead of being swallowed.
		stop()
	}()

	err = run(ctx, cfg)
	stop()
	if err != nil {
		slog.Error("fupi stopped", "error", err)
		os.Exit(1)
	}
}

// healthcheck probes the API running on this host and returns the process exit code: 0 when GET /health answers 200,
// 1 otherwise. It reads only the server settings, so the container's HEALTHCHECK needs no database or Redis settings and
// works in an image that has no shell, curl or wget. It does not read .env: the container's environment is the contract.
func healthcheck(lookup config.Lookup, out io.Writer) int {
	server, err := config.LoadServer(lookup)
	if err != nil {
		_, _ = fmt.Fprintf(out, "healthcheck: invalid configuration: %v\n", err)
		return 1
	}

	target := "http://" + net.JoinHostPort(probeHost(server.Host), strconv.Itoa(server.Port)) + probes.LivePath
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

// probeHost turns a wildcard bind address, which cannot be dialed, into the loopback address that reaches it. Server
// binds exactly the configured host, so any other host is dialed as given.
func probeHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}

// run builds the API from cfg and serves it until ctx is cancelled, then drains in-flight requests and closes the
// data clients. Postgres is required: failing to reach it is an error. Redis is optional: failing to reach it is
// logged and the API carries on (readiness reports it as down).
func run(ctx context.Context, cfg config.App) error {
	// The deferred flush covers the early returns; the normal path flushes right after the server drains (below).
	flushTelemetry := telemetry.Start(ctx, telemetry.Options{ServiceVersion: pkg.Version, Environment: cfg.Environment})
	defer flushTelemetry()

	dbClient, err := postgres.NewPostgresClient(cfg.Database.Postgres)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer dbClient.Close()

	cache, err := cacheredis.NewRedisClient(cfg.Redis)
	if err != nil {
		return fmt.Errorf("create redis client: %w", err)
	}
	defer func() {
		if closeErr := cache.Close(); closeErr != nil {
			slog.Warn("closing redis", "error", closeErr)
		}
	}()

	pingCtx, cancel := context.WithTimeout(ctx, redisStartupPingTimeout)
	if pingErr := cache.Ping(pingCtx); pingErr != nil {
		slog.Warn("redis is down, continuing without it", "addresses", cfg.Redis.Address, "error", pingErr)
	} else {
		slog.Info("redis is up", "addresses", cfg.Redis.Address)
	}
	cancel()

	registry := health.NewRegistry(health.DefaultCheckTimeout)
	registry.Add(health.Check{Name: "postgres", Required: true, Fn: dbClient.HealthCheck})
	registry.Add(health.Check{Name: "redis", Required: false, Fn: cache.Ping})

	dbConfig := database.Config{
		OperationTimeout: cfg.Database.OperationTimeout,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}

	tokenService := jwtauth.NewTokenService(cfg.Auth, jwt.New())

	// No email transport is configured yet, so verification links are logged rather than sent.
	notifier := notifications.NewEmailNotifier(cfg.Server.BaseURL, notifications.NewLoggingEmailSender())

	identityService := identityapp.NewService(
		identitydatastore.NewUserDatastoreAdapter(dbClient, dbConfig),
		tokenService,
		notifier,
	)

	// Best-effort analytics (ADR-0021): the producer connects lazily, so an unreachable Kafka never stops startup.
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
	accessRecorder := urlapp.NewAccessRecorder(geolocation.Unknown{}, publisher, urlAccessedTopic, urlAccessedBuffer)
	defer accessRecorder.Close()

	urlService := urlapp.NewService(
		urldatastore.NewUrlDatastoreAdapter(dbClient, dbConfig),
		postgresrepo.NewSequenceShortCodeGenerator(dbClient, []byte(cfg.ShortCode.Key)),
	).WithAccessRecorder(accessRecorder)

	srv := server.NewServer(server.ServerConfig{
		Header:      cfg.Server.Header,
		Host:        cfg.Server.Host,
		Port:        cfg.Server.Port,
		AppName:     cfg.Server.Name,
		Version:     cfg.Server.Version,
		Environment: cfg.Environment,
		ProbePaths:  []string{probes.LivePath, probes.ReadyPath},
	})

	// Everything is authenticated unless it is listed here. Registration, login, token refresh
	// and email verification must be reachable without a token, by definition, and so must the probes
	// and the short-url redirect.
	srv.Use(apimiddleware.AuthMiddleware(apimiddleware.AuthConfig{
		TokenService: tokenService,
		PublicPaths: []string{
			baseURI + "/auth/register",
			baseURI + "/auth/login",
			baseURI + "/auth/oauth/token",
			baseURI + "/auth/verify",
			probes.LivePath,
			probes.ReadyPath,
		},
		PublicPrefixes: []string{"/docs/"},
		PublicPatterns: []*regexp.Regexp{urlapi.RedirectPathPattern},
	}))

	srv.RegisterHandlers([]router.Router{
		probes.NewRouter(registry),
		identityapi.NewRouter(baseURI, identityService),
		// Last: its redirect matches any single-segment path.
		urlapi.NewRouter(baseURI, cfg.Server.BaseURL, urlService),
	})

	serveErr := srv.Serve(ctx, cfg.ShutdownTimeout, registry.SetDraining)

	// The server has drained or given up: export the last spans and metrics now, before the data clients close. Closing the
	// Postgres pool waits for every connection a stuck request still holds, which can outlast the container's stop grace
	// period and would cost the final export of exactly the incident worth diagnosing.
	flushTelemetry()
	return serveErr
}
