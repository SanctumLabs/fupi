package logger

const (
	// LogTypeLog is normal log type
	LogTypeLog = "log"
	// LogTypeRequest is Request log type
	LogTypeRequest = "request"

	// Field names that defines log schema
	logFieldTimeStamp = "time"
	logFieldLevel     = "level"
	logFieldType      = "type"
	logFieldScope     = "scope"
	logFieldMessage   = "msg"
	logFieldInstance  = "instance"
	logFieldVer       = "ver"
	logFieldAppID     = "app_id"

	reset = "\033[0m"

	black        = 30
	red          = 31
	green        = 32
	yellow       = 33
	blue         = 34
	magenta      = 35
	cyan         = 36
	lightGray    = 37
	darkGray     = 90
	lightRed     = 91
	lightGreen   = 92
	lightYellow  = 93
	lightBlue    = 94
	lightMagenta = 95
	lightCyan    = 96
	white        = 97

	defaultLoggerDevelopment      = false
	defaultLoggerEnableCaller     = false
	defaultLoggerEnableStacktrace = false
	defaultLoggerFormat           = "json"
	defaultLoggerLevel            = InfoLevel
)

// LogFormat represents the log format to use
type LogFormat int8

const (
	LogFormatJson LogFormat = iota
	LogFormatText
)

// String returns a human readable string of the status of an account
func (lf LogFormat) String() string {
	switch lf {
	case LogFormatJson:
		return "json"
	case LogFormatText:
		return "text"
	default:
		return "unknown"
	}
}
