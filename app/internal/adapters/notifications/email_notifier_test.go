package notifications

import (
	"context"
	"errors"
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
		notifier := NewEmailNotifier("https://curtz.io", sender)

		err := notifier.SendEmailVerification(context.Background(), "user@curtz.com", "tok-123")
		require.NoError(t, err)

		assert.Equal(t, 1, sender.calls)
		assert.Equal(t, "user@curtz.com", sender.recipient)
		assert.Contains(t, sender.subject, "verify")
		assert.Contains(t, sender.body, "https://curtz.io/auth/verify?v=tok-123",
			"the link must point at the configured base URL and carry the token verbatim")
	})

	t.Run("uses the configured base URL, not the machine hostname", func(t *testing.T) {
		sender := &recordingSender{}
		notifier := NewEmailNotifier("https://short.example", sender)

		require.NoError(t, notifier.SendEmailVerification(context.Background(), "user@curtz.com", "tok"))

		assert.True(t, strings.Contains(sender.body, "https://short.example"),
			"body %q should contain the configured base URL", sender.body)
	})

	t.Run("propagates a transport failure", func(t *testing.T) {
		sender := &recordingSender{err: errors.New("smtp down")}
		notifier := NewEmailNotifier("https://curtz.io", sender)

		err := notifier.SendEmailVerification(context.Background(), "user@curtz.com", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "smtp down")
	})
}

func TestLoggingEmailSender_DoesNotFail(t *testing.T) {
	// The logging sender stands in for a real transport; registration must not break because
	// no transport is configured.
	err := NewLoggingEmailSender().SendEmail(context.Background(), "user@curtz.com", "subject", "body")
	assert.NoError(t, err)
}
