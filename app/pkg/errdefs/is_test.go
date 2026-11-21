package errdefs

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsNotFound_SurvivesWrapping(t *testing.T) {
	base := NotFound(errors.New("no rows"))

	cases := map[string]error{
		"bare":           base,
		"wrapped once":   fmt.Errorf("op failed: %w", base),
		"wrapped twice":  fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", base)),
		"already causer": Conflict(base), // Conflict wraps NotFound; the innermost classification wins
	}

	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			if name == "already causer" {
				if !IsConflict(err) {
					t.Errorf("expected IsConflict to be true for %v", err)
				}
				return
			}
			if !IsNotFound(err) {
				t.Errorf("expected IsNotFound to be true for %v", err)
			}
			if IsConflict(err) {
				t.Errorf("expected IsConflict to be false for %v", err)
			}
		})
	}
}

func TestIsNotFound_PlainError(t *testing.T) {
	if IsNotFound(fmt.Errorf("plain: %w", errors.New("x"))) {
		t.Error("plain wrapped error should not be NotFound")
	}
}
