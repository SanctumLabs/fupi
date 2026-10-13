package server

import "github.com/gofiber/fiber/v2"

const (
	EnvServerHeader   = "SERVER_HEADER"
	EnvServerHost     = "SERVER_HOST"
	EnvServerPort     = "SERVER_PORT"
	EnvServerPassword = "SERVER_APP_PASSWORD"
	EnvServerName     = "SERVER_NAME"
	EnvServerVersion  = "SERVER_VERSION"
	EnvGrpcHost       = "GRPC_HOST"
	EnvGrpcPort       = "GRPC_PORT"
	EnvEnvironment    = "ENVIRONMENT"
)

type (
	ServerConfig struct {
		Header      string `env:"SERVER_HEADER" env-default:"BidSvc" env-description:"Server header sent in responses" env-required:"false"`
		Host        string `env:"SERVER_HOST" env-description:"Host address" env-required:"true" env-default:"0.0.0.0"`
		Port        int    `env:"HTTP_PORT" env-description:"Server HTTP Port" env-required:"true" env-default:"5001"`
		AppName     string `env:"SERVER_NAME" env-required:"false" env-description:"Application name" env-default:"Bids Service"`
		Version     string `env:"SERVER_VERSION" env-required:"false" env-description:"Application Version" env-default:"1.0.0"`
		Environment string `env:"ENVIRONMENT" env-required:"false" env-description:"Application Environment" env-default:"production"`
		// ProbePaths are the health probe routes. They get no span and no metric, and their access log line is debug level.
		ProbePaths []string
		// Guards run in order on every request, after the security headers and before idempotency, so the responses they
		// give still carry the headers and are never stored against an idempotency key.
		Guards []fiber.Handler
	}

	GrpcServerConfig struct {
		Host string `env:"GRPC_HOST" env-description:"gRPC Host" env-required:"true" env-default:"0.0.0.0"`
		Port int    `env:"GRPC_PORT" env-description:"gRPC Port" env-required:"true" env-default:"5002"`
	}
)
