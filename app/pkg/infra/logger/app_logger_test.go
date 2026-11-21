package logger

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStderr redirects os.Stderr to a temp file for the duration of the test. The logger resolves its "stderr"
// output path to os.Stderr when it is built, so this must be called before New.
func captureStderr(t *testing.T) *os.File {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "stderr")
	require.NoError(t, err)

	orig := os.Stderr
	os.Stderr = f
	t.Cleanup(func() {
		os.Stderr = orig
		_ = f.Close()
	})

	return f
}

func readLines(t *testing.T, f *os.File) []string {
	t.Helper()

	b, err := os.ReadFile(f.Name())
	require.NoError(t, err)

	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

func TestNewLoggerEmitsLogs(t *testing.T) {
	t.Run("default config logs info lines to stderr", func(t *testing.T) {
		stderr := captureStderr(t)

		l := New(nil)
		l.Infow("Listening on port", "port", 8080)

		lines := readLines(t, stderr)
		require.Len(t, lines, 1, "an Infow call must produce exactly one log line")

		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(lines[0]), &entry))
		assert.Equal(t, "Listening on port", entry["message"])
		assert.Equal(t, "INFO", entry["level"])
		assert.EqualValues(t, 8080, entry["port"])
	})

	t.Run("repeated identical lines are not all dropped", func(t *testing.T) {
		stderr := captureStderr(t)

		l := New(nil)
		for i := 0; i < 5; i++ {
			l.Infow("shutting down server", "port", 8080)
		}

		assert.Len(t, readLines(t, stderr), 5)
	})

	t.Run("lines below the configured level are filtered", func(t *testing.T) {
		stderr := captureStderr(t)

		l := New(&LogConfig{Level: "error"})
		l.Infow("should be filtered")
		l.Errorw("should be logged")

		lines := readLines(t, stderr)
		require.Len(t, lines, 1)
		assert.Contains(t, lines[0], "should be logged")
	})
}

func TestNewLoggerCallerAndStacktraceFlags(t *testing.T) {
	logError := func(t *testing.T, cfg *LogConfig) map[string]interface{} {
		t.Helper()

		stderr := captureStderr(t)
		New(cfg).Errorw("boom")

		lines := readLines(t, stderr)
		require.Len(t, lines, 1)

		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(lines[0]), &entry))
		return entry
	}

	t.Run("caller and stacktrace are omitted by default", func(t *testing.T) {
		entry := logError(t, nil)

		assert.NotContains(t, entry, "caller")
		assert.NotContains(t, entry, "stacktrace")
	})

	t.Run("EnableCaller adds the caller field", func(t *testing.T) {
		entry := logError(t, &LogConfig{EnableCaller: true})

		assert.Contains(t, entry, "caller")
		assert.NotContains(t, entry, "stacktrace")
	})

	t.Run("EnableStackTrace adds the stacktrace field", func(t *testing.T) {
		entry := logError(t, &LogConfig{EnableStackTrace: true})

		assert.Contains(t, entry, "stacktrace")
		assert.NotContains(t, entry, "caller")
	})
}

func TestNewTestLogger(t *testing.T) {
	l, logs := NewTestLogger()

	l.Infow("hello", "k", "v")
	l.Debugw("below info, not observed")

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, "hello", entries[0].Message)
	assert.Equal(t, "v", entries[0].ContextMap()["k"])
}
