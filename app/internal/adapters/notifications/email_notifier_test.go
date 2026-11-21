package notifications

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSender struct {
	recipient string
	subject   string
	body      string
	calls     int
	err       error
}

func (s *recordingSender) SendEmail(_ context.Context, recipient, subject, body string) error {
	s.calls++
	s.recipient, s.subject, s.body = recipient, subject, body
	return s.err
}

func TestEmailNotifier_SendEmailVerification(t *testing.T) {
	t.Run("emails a verification link carrying the token", func(t *testing.T) {
		sender := &recordingSender{}
		notifier := NewEmailNotifier("https://fupi.io", sender)

		err := notifier.SendEmailVerification(context.Background(), "user@fupi.com", "tok-123")
		require.NoError(t, err)

		assert.Equal(t, 1, sender.calls)
		assert.Equal(t, "user@fupi.com", sender.recipient)
		assert.Contains(t, sender.subject, "verify")
		assert.Contains(t, sender.body, "https://fupi.io/auth/verify?v=tok-123",
			"the link must point at the configured base URL and carry the token verbatim")
	})

	t.Run("uses the configured base URL, not the machine hostname", func(t *testing.T) {
		sender := &recordingSender{}
		notifier := NewEmailNotifier("https://short.example", sender)

		require.NoError(t, notifier.SendEmailVerification(context.Background(), "user@fupi.com", "tok"))

		assert.True(t, strings.Contains(sender.body, "https://short.example"),
			"body %q should contain the configured base URL", sender.body)
	})

	t.Run("propagates a transport failure", func(t *testing.T) {
		sender := &recordingSender{err: errors.New("smtp down")}
		notifier := NewEmailNotifier("https://fupi.io", sender)

		err := notifier.SendEmailVerification(context.Background(), "user@fupi.com", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "smtp down")
	})
}

func TestLoggingEmailSender_DoesNotFail(t *testing.T) {
	// The logging sender stands in for a real transport; registration must not break because
	// no transport is configured.
	err := NewLoggingEmailSender(false).SendEmail(context.Background(), "user@fupi.com", "subject", "body")
	assert.NoError(t, err)
}

// captureDefaultLog sends the default logger to a buffer for the rest of the test.
func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestLoggingEmailSender_LogsTheBodyOnlyWhenAsked(t *testing.T) {
	t.Run("logs the body, so a developer can follow the verification link", func(t *testing.T) {
		buf := captureDefaultLog(t)

		require.NoError(t, NewLoggingEmailSender(true).SendEmail(context.Background(), "user@fupi.com", "subject", "the link"))

		assert.Contains(t, buf.String(), `"body":"the link"`)
	})

	t.Run("leaves the body out, because it carries a live verification token", func(t *testing.T) {
		buf := captureDefaultLog(t)

		require.NoError(t, NewLoggingEmailSender(false).SendEmail(context.Background(), "user@fupi.com", "subject", "the link"))

		assert.Contains(t, buf.String(), "user@fupi.com")
		assert.NotContains(t, buf.String(), "the link")
	})
}
