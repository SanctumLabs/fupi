// Package notifications adapts message delivery to the ports.Notifier port.
package notifications

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sanctumlabs/curtz/app/internal/ports"
)

// EmailSender sends a single email. Splitting it out keeps the verification-link wording in the
// notifier and the transport (SMTP, SES, a test double) swappable behind this one method.
type EmailSender interface {
	SendEmail(ctx context.Context, recipient, subject, body string) error
}

type emailNotifier struct {
	baseURL string
	sender  EmailSender
}

var _ ports.Notifier = (*emailNotifier)(nil)

// NewEmailNotifier creates a Notifier that emails verification links pointing at baseURL.
func NewEmailNotifier(baseURL string, sender EmailSender) ports.Notifier {
	return &emailNotifier{baseURL: baseURL, sender: sender}
}

func (n *emailNotifier) SendEmailVerification(ctx context.Context, recipient, token string) error {
	subject := "Welcome to Curtz, kindly verify your account"
	body := fmt.Sprintf("Click on link %s/auth/verify?v=%s to verify your account", n.baseURL, token)

	if err := n.sender.SendEmail(ctx, recipient, subject, body); err != nil {
		return fmt.Errorf("failed to send verification email: %w", err)
	}
	return nil
}

// LoggingEmailSender writes emails to the log instead of delivering them. It carries over the
// behaviour of the pre-v2 email service, which was a no-op stub, so the registration flow works
// end to end. Replace it with a real transport (SMTP, SES) before going to production.
type LoggingEmailSender struct{}

func NewLoggingEmailSender() EmailSender {
	return &LoggingEmailSender{}
}

func (s *LoggingEmailSender) SendEmail(ctx context.Context, recipient, subject, body string) error {
	slog.WarnContext(ctx, "email not delivered: no email transport is configured",
		"recipient", recipient,
		"subject", subject,
	)
	return nil
}
