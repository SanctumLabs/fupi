//go:build legacy

package contracts

import (
	"time"
)

type AuthService interface {
	// Authenticate a user given the token. Returns user id if authenticated, error otherwise.
	Authenticate(token string) (string, time.Time, error)
	// GenerateToken generates a user token provided the user id
	GenerateToken(userID string) (string, error)
	// GenerateRefreshToken generates a new refresh token
	GenerateRefreshToken(userID string) (string, error)
}

type NotificationService interface {
	SendEmailNotification(recipient, subject, message string) error
	SendEmailVerificationNotification(recipient, token string) error
}

type EmailService interface {
	SendEmail(recipient, subject, body string) error
}

type SmsService interface {
	SendSms(recipient, message string) error
}

// CacheService interface to be used by services that implement cache like functionality
type CacheService interface {
	LookupUrl(shortCode string) (string, error)
	SaveURL(shortCode, originalUrl string, expiryTime time.Duration) (string, error)
}
