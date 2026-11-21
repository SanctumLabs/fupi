package logger

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLogConfigEnvNamesMatchConstants guards against the exported Env* constants (and the .env files that follow
// them) drifting from the env tags that a config loader reads from LogConfig.
func TestLogConfigEnvNamesMatchConstants(t *testing.T) {
	tests := []struct {
		field string
		env   string
	}{
		{"Level", EnvLoggerLevel},
		{"Format", EnvLoggerFormat},
		{"Development", EnvLoggerDevelopment},
		{"EnableCaller", EnvLoggerEnableCaller},
		{"EnableStackTrace", EnvLoggerEnableStackTrace},
	}

	typ := reflect.TypeOf(LogConfig{})
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			f, ok := typ.FieldByName(tt.field)
			assert.True(t, ok, "LogConfig has no field %s", tt.field)
			assert.Equal(t, tt.env, f.Tag.Get("env"))
		})
	}
}
