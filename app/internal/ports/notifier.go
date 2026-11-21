package ports

import "context"

// Notifier delivers messages to users out of band. The Identity context uses it to send
// verification links; the Notification bounded context owns the delivery mechanics.
type Notifier interface {
	// SendEmailVerification sends the recipient a link carrying their verification token
	SendEmailVerification(ctx context.Context, recipient, token string) error
}
