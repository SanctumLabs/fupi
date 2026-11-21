package test

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// containerReadyTimeout returns the configured ready-wait duration,
// preferring the environment variable over the built-in default.
func containerReadyTimeout() time.Duration {
	if raw := os.Getenv(envContainerReadyTimeout); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			return d
		}
		slog.Warn("invalid TEST_CONTAINER_READY_TIMEOUT value; using default",
			slog.String("value", raw),
			slog.Duration("default", defaultContainerReadyTimeout),
		)
	}
	return defaultContainerReadyTimeout
}

// migrationsDir returns the absolute path to the SQL migrations directory.
//
// It anchors to the location of this source file at compile time via
// runtime.Caller, so the result is correct regardless of which package
// directory `go test` sets as the working directory. The previous
// approach (os.Getwd + filepath.Dir) produced a doubled path because it
// only climbed one level out of the test-package directory and then
// re-appended the full repo-relative migration path.
func migrationsDir() string {
	// runtime.Caller(0) always returns the absolute path of this file
	// (app/test/test_database.go) as baked in at compile time.
	_, thisFile, _, _ := runtime.Caller(0)
	// app/test → app → repo root  (two levels up)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
	return filepath.Join(repoRoot, migrationRelPath)
}
