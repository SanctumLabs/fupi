package identity

import (
	"fmt"
	"time"
)

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
	return fmt.Sprintf("%s %s", ufn.verificationToken, ufn.verificationExpires)
}
