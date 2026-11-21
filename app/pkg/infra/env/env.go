package env

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/exp/slog"
)

// EnvConfig is a struct for environment configuration
type EnvConfig struct{}

// NewEnvConfig returns a new EnvConfig
func NewEnvConfig() *EnvConfig {
	return &EnvConfig{}
}

// EnvOr gets an environment variable by the given key, if not found def is returned
func (ec *EnvConfig) EnvOr(key, def string) string {
	if value, ok := os.LookupEnv(key); ok {
		slog.Debug("EnvConfig> env key found", "key", key, "value", value)
		return value
	}
	return def
}

// EnvOr gets an environment variable by the given key, if not found def is returned
func (ec *EnvConfig) EnvIntOr(key string, def int) int {
	if value, ok := os.LookupEnv(key); ok {
		slog.Debug("EnvConfig> env key found", "key", key, "value", value)
		v, err := strconv.Atoi(value)
		if err != nil {
			slog.Error(
				"Failed to convert env to int. Defaulting env key",
				"key", key,
				"value", value,
				"def", def,
				"error", err,
			)
			return def
		}
		return v
	}
	return def
}

// EnvInt32Or gets an environment variable by the given key as int32,
// if not found or invalid def is returned.
func (ec *EnvConfig) EnvInt32Or(key string, def int32) int32 {
	if value, ok := os.LookupEnv(key); ok {
		slog.Debug("EnvConfig> env key found", "key", key, "value", value)
		v, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			slog.Error(
				"Failed to convert env to int32. Defaulting env key",
				"key", key,
				"value", value,
				"def", def,
				"error", err,
			)
			return def
		}
		return int32(v)
	}
	return def
}

func (ec *EnvConfig) EnvFloatOr(key string, def float32) float32 {
	if value, ok := os.LookupEnv(key); ok {
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			slog.Error(
				"Failed to convert env to float. Defaulting env key",
				"key", key,
				"value", value,
				"def", def,
				"error", err,
			)
			return def
		}
		return float32(v)
	}
	return def
}

// EnvOr gets an environment variable by the given key, if not found def is returned
func (ec *EnvConfig) EnvDurationOr(key string, def int, duration time.Duration) time.Duration {
	if value, ok := os.LookupEnv(key); ok {
		slog.Debug("EnvConfig> env key found", "key", key, "value", value)
		v, err := strconv.Atoi(value)
		if err != nil {
			slog.Error(
				"Failed to convert env to int. Defaulting env key",
				"key", key,
				"value", value,
				"def", def,
				"error", err,
			)
			return time.Duration(def) * duration
		}
		return time.Duration(v) * duration
	}
	return time.Duration(def) * duration
}

// ReadEnvToMap returns a map of environment variables
func (ec *EnvConfig) ReadEnvToMap() map[string]string {
	env, err := godotenv.Read()
	if err != nil {
		slog.Error("Failed to read env to map", "error", err)
	}

	return env
}

// EnvOr gets an environment variable by the given key, if not found def is returned
func (ec *EnvConfig) EnvBoolOr(key string, def bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		v, err := strconv.ParseBool(value)
		if err != nil {
			slog.Error(
				"Failed to convert env to bool. Defaulting env key",
				"key", key,
				"value", value,
				"def", def,
				"error", err,
			)
			return def
		}
		return v
	}
	return def
}
