// Command curtz runs the Curtz HTTP API.
//
// Only the Identity bounded context is wired up so far; the URL context follows once its
// application layer exists.
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/joho/godotenv"
	apimiddleware "github.com/sanctumlabs/curtz/app/api/middleware"
	identityapi "github.com/sanctumlabs/curtz/app/api/v1/identity"
	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/internal/adapters/jwtauth"
	"github.com/sanctumlabs/curtz/app/internal/adapters/notifications"
	identitydatastore "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/identity"
	identityapp "github.com/sanctumlabs/curtz/app/internal/application/identity"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
	"github.com/sanctumlabs/curtz/app/pkg/infra/env"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/router"
	"github.com/sanctumlabs/curtz/app/pkg/jwt"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
)

const baseURI = "/api/v1/curtz"

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, relying on the environment", "error", err)
	}

	envConfig := env.NewEnvConfig()

	dbClient, dbErr := postgres.NewPostgresClient(postgres.PostgresDatabaseConfig{
		Host:            envConfig.EnvOr("DATABASE_HOST", "localhost"),
		Username:        envConfig.EnvOr("DATABASE_USERNAME", "curtz-user"),
		Password:        envConfig.EnvOr("DATABASE_PASSWORD", "curtz-pass"),
		Name:            envConfig.EnvOr("DATABASE_NAME", "curtzdb"),
		Port:            envConfig.EnvOr("DATABASE_PORT", "5433"),
		Url:             envConfig.EnvOr("DATABASE_URL", ""),
		SslMode:         envConfig.EnvOr("DATABASE_SSL_MODE", "disable"),
		MaxConns:        envConfig.EnvInt32Or("DATABASE_MAX_CONNS", 30),
		MinConns:        envConfig.EnvInt32Or("DATABASE_MIN_CONNS", 5),
		MaxConnLifetime: envConfig.EnvDurationOr("DATABASE_MAX_CONN_LIFETIME", 1, time.Hour),
		MaxConnIdleTime: envConfig.EnvDurationOr("DATABASE_MAX_CONN_IDLE_TIME", 30, time.Minute),
		ConnTimeout:     envConfig.EnvDurationOr("DATABASE_CONN_TIMEOUT", 30, time.Second),
		QueryTimeout:    envConfig.EnvDurationOr("DATABASE_QUERY_TIMEOUT", 10, time.Second),
	})
	if dbErr != nil {
		slog.Error("failed to connect to the database", "error", dbErr)
		os.Exit(1)
	}
	defer dbClient.Close()

	dbConfig := database.Config{
		OperationTimeout: envConfig.EnvDurationOr("DATABASE_OPERATION_TIMEOUT", 30, time.Second),
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}

	authConfig := config.AuthConfig{
		Jwt: config.Jwt{
			Secret:             envConfig.EnvOr("AUTH_SECRET", "curtz-secret"),
			Issuer:             envConfig.EnvOr("AUTH_ISSUER", "curtz"),
			ExpireDelta:        envConfig.EnvIntOr("AUTH_EXPIRE_DELTA", 15),
			RefreshExpireDelta: envConfig.EnvIntOr("AUTH_REFRESH_EXPIRE_DELTA", 24),
		},
	}

	tokenService := jwtauth.NewTokenService(authConfig, jwt.New())

	// No email transport is configured yet, so verification links are logged rather than sent.
	notifier := notifications.NewEmailNotifier(
		envConfig.EnvOr("APP_BASE_URL", "http://localhost:8085"),
		notifications.NewLoggingEmailSender(),
	)

	identityService := identityapp.NewService(
		identitydatastore.NewUserDatastoreAdapter(dbClient, dbConfig),
		tokenService,
		notifier,
	)

	srv := server.NewServer(server.ServerConfig{
		Header:      envConfig.EnvOr("SERVER_HEADER", "Curtz"),
		Host:        envConfig.EnvOr("SERVER_HOST", "0.0.0.0"),
		Port:        envConfig.EnvIntOr("HTTP_PORT", 8085),
		AppName:     envConfig.EnvOr("SERVER_NAME", "Curtz"),
		Version:     envConfig.EnvOr("SERVER_VERSION", "1.0.0"),
		Environment: envConfig.EnvOr("ENVIRONMENT", "development"),
	})

	// Everything is authenticated unless it is listed here. Registration, login, token refresh
	// and email verification must be reachable without a token, by definition.
	srv.Use(apimiddleware.AuthMiddleware(apimiddleware.AuthConfig{
		TokenService: tokenService,
		PublicPaths: []string{
			baseURI + "/auth/register",
			baseURI + "/auth/login",
			baseURI + "/auth/oauth/token",
			baseURI + "/auth/verify",
			"/health",
			"/metrics",
		},
		PublicPrefixes: []string{"/docs/"},
	}))

	srv.RegisterHandlers([]router.Router{
		identityapi.NewRouter(baseURI, identityService),
	})

	if err := srv.Listen(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
