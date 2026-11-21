package logger

const (
	EnvLoggerLevel            = "LOG_LEVEL"
	EnvLoggerFormat           = "LOG_FORMAT"
	EnvLoggerDevelopment      = "LOG_DEVELOPMENT_MODE"
	EnvLoggerEnableCaller     = "LOG_ENABLE_CALLER"
	EnvLoggerEnableStackTrace = "LOG_ENABLE_STACK_TRACE"
)

type LogConfig struct {
	Level            string `env:"LOG_LEVEL" env-required:"false" env-default:"info"`
	Format           string `env:"LOG_FORMAT" env-required:"false" env-default:"json" env-description:"The log format to use"`
	Development      bool   `env:"LOG_DEVELOPMENT_MODE" env-required:"false" env-default:"false"`
	Environment      string `env:"ENVIRONMENT" env-required:"false" env-default:"production"`
	EnableCaller     bool   `env:"LOG_ENABLE_CALLER" env-required:"false" env-default:"false" env-description:"Whether to disable the call site where the log originates from"`
	EnableStackTrace bool   `env:"LOG_ENABLE_STACKTRACE" env-required:"false" env-default:"false" env-description:"Whether to disable stacktrace from being output by the log"`
}
