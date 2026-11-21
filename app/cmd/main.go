//go:build legacy

package main

import (
	"fmt"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/sanctumlabs/curtz/app/api/client"
	"github.com/sanctumlabs/curtz/app/api/health"
	authApi "github.com/sanctumlabs/curtz/app/api/v1/auth"
	"github.com/sanctumlabs/curtz/app/api/v1/url"
	"github.com/sanctumlabs/curtz/app/config"
	cache "github.com/sanctumlabs/curtz/app/internal/adapters/redis"
	"github.com/sanctumlabs/curtz/app/internal/core/urlsvc"
	urlReadSvc "github.com/sanctumlabs/curtz/app/internal/core/urlsvc/read"
	urlWriteSvc "github.com/sanctumlabs/curtz/app/internal/core/urlsvc/write"
	"github.com/sanctumlabs/curtz/app/internal/core/usersvc"
	"github.com/sanctumlabs/curtz/app/internal/repositories"
	"github.com/sanctumlabs/curtz/app/internal/services/auth"
	"github.com/sanctumlabs/curtz/app/internal/services/notifications"
	"github.com/sanctumlabs/curtz/app/internal/services/notifications/email"
	"github.com/sanctumlabs/curtz/app/pkg/jwt"
	"github.com/sanctumlabs/curtz/app/server"
	"github.com/sanctumlabs/curtz/app/server/middleware"
	"github.com/sanctumlabs/curtz/app/server/router"
	"github.com/sanctumlabs/curtz/app/tools/env"
	"github.com/sanctumlabs/curtz/app/tools/logger"
	"github.com/sanctumlabs/curtz/app/tools/monitoring"
)

const (
	Env                       = "ENV"
	EnvDocsEnabled            = "DOCS_ENABLED"
	EnvLogLevel               = "LOG_LEVEL"
	EnvLogJsonOutput          = "LOG_JSON_OUTPUT"
	EnvPort                   = "PORT"
	EnvDatabaseHost           = "DATABASE_HOST"
	EnvDatabase               = "DATABASE"
	EnvDatabaseUsername       = "DATABASE_USERNAME"
	EnvDatabasePassword       = "DATABASE_PASSWORD"
	EnvDatabaseUsesSRV        = "DATABASE_USES_SRV"
	EnvDatabasePort           = "DATABASE_PORT"
	EnvAuthSecret             = "AUTH_SECRET"
	EnvAuthExpireDelta        = "AUTH_EXPIRE_DELTA"
	EnvAuthRefreshExpireDelta = "AUTH_REFRESH_EXPIRE_DELTA"
	EnvAuthIssuer             = "AUTH_ISSUER"
	EnvCacheHost              = "CACHE_HOST"
	EnvCacheUsername          = "CACHE_USERNAME"
	EnvCachePassword          = "CACHE_PASSWORD"
	EnvCachePort              = "CACHE_PORT"
	EnvCacheRequireAuth       = "CACHE_REQUIRE_AUTH"
	EnvSentryDsn              = "SENTRY_DSN"
	EnvSentryEnvironment      = "SENTRY_ENV"
	EnvSentrySampleRate       = "SENTRY_SAMPLE_RATE"
	EnvSentryEnabled          = "SENTRY_ENABLED"
)

func main() {
	log := logger.NewLogger("curtz-api")

	err := godotenv.Load()
	if err != nil {
		log.Warn("Error loading .env file. Using defaults")
	}

	environment := env.EnvOr(Env, "development")
	docsEnabled := env.EnvOr(EnvDocsEnabled, "true")
	logLevel := env.EnvOr(EnvLogLevel, "debug")
	logJsonOutput := env.EnvOr(EnvLogJsonOutput, "true")
	port := env.EnvOr(EnvPort, "8085")
	databaseHost := env.EnvOr(EnvDatabaseHost, "localhost")
	database := env.EnvOr(EnvDatabase, "curtzdb")
	databaseUser := env.EnvOr(EnvDatabaseUsername, "curtzUser")
	databasePass := env.EnvOr(EnvDatabasePassword, "curtzPassword")
	databasePort := env.EnvOr(EnvDatabasePort, "27017")
	databaseUsesSRV := env.EnvOr(EnvDatabaseUsesSRV, "true")
	authSecret := env.EnvOr(EnvAuthSecret, "curtz-secret")
	authExpireDelta := env.EnvOr(EnvAuthExpireDelta, "15")
	authRefreshExpireDelta := env.EnvOr(EnvAuthRefreshExpireDelta, "1")
	authIssuer := env.EnvOr(EnvAuthIssuer, "curtz")
	cacheHost := env.EnvOr(EnvCacheHost, "localhost")
	cachePort := env.EnvOr(EnvCachePort, "6379")
	cacheUsername := env.EnvOr(EnvCacheUsername, "curtzUser")
	cachePassword := env.EnvOr(EnvCachePassword, "curtzPassword")
	cacheRequireAuth := env.EnvOr(EnvCacheRequireAuth, "false")
	sentryEnabled := env.EnvOr(EnvSentryEnabled, "false")
	sentryDsn := env.EnvOr(EnvSentryDsn, "")
	sentryEnvironment := env.EnvOr(EnvSentryEnvironment, "development")
	sentrySampleRate := env.EnvOr(EnvSentrySampleRate, "0.5")

	enableApiDocs, err := strconv.ParseBool(docsEnabled)
	if err != nil {
		enableApiDocs = true
	}

	expireDelta, err := strconv.Atoi(authExpireDelta)
	if err != nil {
		expireDelta = 15
	}

	refreshExpireDelta, err := strconv.Atoi(authRefreshExpireDelta)
	if err != nil {
		refreshExpireDelta = 1
	}

	cacheNeedsAuth, err := strconv.ParseBool(cacheRequireAuth)
	if err != nil {
		cacheNeedsAuth = false
	}

	databaseUsesSrv, err := strconv.ParseBool(databaseUsesSRV)
	if err != nil {
		databaseUsesSrv = true
	}

	enableJsonOutput, err := strconv.ParseBool(logJsonOutput)
	if err != nil {
		enableJsonOutput = true
	}

	enableSentry, err := strconv.ParseBool(sentryEnabled)
	if err != nil {
		enableSentry = false
	}

	sentryRate, err := strconv.ParseFloat(sentrySampleRate, 64)
	if err != nil {
		sentryRate = 0.5
	}

	configuration := config.Config{
		Env:         environment,
		DocsEnabled: enableApiDocs,
		Port:        port,
		Logging: config.LoggingConfig{
			Level:            logLevel,
			EnableJSONOutput: enableJsonOutput,
		},
		Auth: config.AuthConfig{
			Jwt: config.Jwt{
				Secret:             authSecret,
				ExpireDelta:        expireDelta,
				RefreshExpireDelta: refreshExpireDelta,
				Issuer:             authIssuer,
			},
		},
		Database: config.DatabaseConfig{
			Host:     databaseHost,
			Database: database,
			User:     databaseUser,
			Password: databasePass,
			Port:     databasePort,
			IsSRV:    databaseUsesSrv,
		},
		Cache: config.CacheConfig{
			Host:        cacheHost,
			Port:        cachePort,
			Username:    cacheUsername,
			Password:    cachePassword,
			RequireAuth: cacheNeedsAuth,
		},
		Monitoring: config.MonitoringConfig{
			Sentry: config.Sentry{
				DSN:              sentryDsn,
				Environment:      sentryEnvironment,
				Enabled:          enableSentry,
				TracesSampleRate: sentryRate,
			},
		},
	}

	srv := server.NewServer(&configuration)

	if _, err := monitoring.New(configuration.Monitoring); err != nil {
		log.Fatalf("Failed to configure Monitoring: %s", err)
	}

	authService := auth.NewService(configuration.Auth, jwt.New())
	corsMiddleware := middleware.NewCORSMiddleware(configuration.CorsHeaders)
	loggingMiddleware := middleware.NewLoggingMiddleware(configuration.Logging)
	recoveryMiddleware := middleware.NewRecoveryMiddleware()
	authMiddleware := middleware.NewAuthMiddleware(configuration.Auth, authService)
	monitoringMiddleware := middleware.NewMonitoringMiddleware(configuration.Monitoring)

	repository := repositories.NewRepository(configuration.Database)
	emailSvc := email.NewEmailSvc()
	notificationSvc := notifications.NewNotificationSvc(configuration.Host, emailSvc)
	cache := cache.New(configuration.Cache)

	userService := usersvc.NewUserSvc(repository.GetUserRepo(), notificationSvc)
	urlService := urlsvc.NewUrlSvc(repository.GetUrlReadRepo(), repository.GetUrlWriteRepo(), userService, cache)
	urlReadService := urlReadSvc.NewUrlReadSvc(repository.GetUrlReadRepo(), userService, cache)
	urlWriteService := urlWriteSvc.NewUrlWriteSvc(repository.GetUrlWriteRepo(), userService)

	baseUri := "/api/v1/curtz"

	routers := []router.Router{
		url.NewUrlRouter(baseUri, urlService, urlReadService, urlWriteService),
		authApi.NewRouter(baseUri, userService, authService),
		health.NewHealthRouter(),
		client.NewClientRouter(urlService, userService),
	}

	srv.InitRouter(routers...)

	srv.UseMiddleware(monitoringMiddleware)
	srv.UseMiddleware(loggingMiddleware)
	srv.UseMiddleware(corsMiddleware)
	srv.UseMiddleware(recoveryMiddleware)
	srv.UseMiddleware(authMiddleware)

	appServer := srv.CreateServer()

	err = appServer.Run(fmt.Sprintf(":%s", port))
	if err != nil {
		_, msg := fmt.Printf("Failed to start Server %s", err)
		log.Error(msg)
		panic(msg)
	}
}
