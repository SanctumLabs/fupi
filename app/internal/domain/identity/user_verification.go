package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// VerificationTokenTTL is how long a freshly issued verification token stays valid.
const VerificationTokenTTL = 15 * time.Minute

// NewVerificationToken generates a crypto-random, URL-safe verification token.
// It is deliberately not derived from the user's ID: UUIDv7 ids encode a timestamp and would
// make tokens partially predictable.
func NewVerificationToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate verification token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// UserVerification is a value object representing a user's verification in the system.
type UserVerification struct {
	verificationToken   string
	verificationExpires time.Time
	verified            bool
}

// NewUserVerification is a factory method that creates a user's verification details
func NewUserVerification(verificationToken string, verificationExpires time.Time, verified bool) (UserVerification, error) {
	if len(verificationToken) == 0 {
		return UserVerification{}, fmt.Errorf("token %s is invalid", verificationToken)
	}

	return UserVerification{
		verificationToken:   verificationToken,
		verificationExpires: verificationExpires,
		verified:            verified,
	}, nil
}

func (ufn *UserVerification) Token() string {
	return ufn.verificationToken
}

func (ufn *UserVerification) Expires() time.Time {
	return ufn.verificationExpires
}

func (ufn *UserVerification) Verified() bool {
	return ufn.verified
}

// IsExpired reports whether the verification token is no longer valid at the given time.
func (ufn *UserVerification) IsExpired(now time.Time) bool {
	return !ufn.verificationExpires.After(now)
}

// SetVerified sets the user's verification to verified
func (ufn *UserVerification) SetVerified(verified bool) error {
	ufn.verified = verified
	return nil
}

// SetVerificationToken sets the user's verification token
func (ufn *UserVerification) SetVerificationToken(token string) error {
	if len(token) == 0 {
		return fmt.Errorf("token %s is invalid", token)
	}
	ufn.verificationToken = token
	return nil
}

func (ufn *UserVerification) String() string {
	return fmt.Sprintf("Verification(token=%s expires=%s verified=%t)", ufn.verificationToken, ufn.verificationExpires, ufn.verified)
}
