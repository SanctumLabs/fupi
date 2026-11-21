package identity

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/sanctumlabs/curtz/app/pkg/validators"
)

// Email is a value object representing an email address in the system.
type Email struct {
	value string
}

// NewEmail is a factory method that creates an email number
func NewEmail(value string) (Email, error) {
	if err := validators.ValidateEmail(value); err != nil {
		return Email{}, fmt.Errorf("phone number %s is invalid", value)
	}

	return Email{
		value: value,
	}, nil
}

// Amount retrieves
func (e *Email) Value() string {
	return e.value
}

// SetPhone sets the phone number
func (e *Email) SetPhone(phoneNumber string) error {
	if err := validators.ValidateEmail(phoneNumber); err != nil {
		return fmt.Errorf("phone number %s is invalid", phoneNumber)
	}
	e.value = phoneNumber
	return nil
}

func (e *Email) String() string {
	return e.value
}

// redactEmail formats a phone number to show the country code, first 3 digits, and last 3 digits, with the rest redacted.
func (e Email) redactEmail() (string, error) {
	// Regex to match the phone number pattern
	regex := regexp.MustCompile(`^(\+\d{1,3})(\d{3})(\d+)(\d{3})$`)
	matches := regex.FindStringSubmatch(e.value)

	if len(matches) != 5 {
		return "", fmt.Errorf("invalid phone number format")
	}

	// Extract the country code, first 3 digits, and last 3 digits
	countryCode := matches[1]
	firstThree := matches[2]
	lastThree := matches[4]

	// Return the formatted phone number
	return fmt.Sprintf("%s%s***%s", countryCode, firstThree, lastThree), nil
}

// LogValue implements slog.LogValuer and returns a grouped value
// with fields redacted. See https://pkg.go.dev/log/slog#LogValuer
func (e Email) LogValue() slog.Value {
	// Just show the first few digits
	redactedEmail, err := e.redactEmail()
	if err != nil {
		// in this case don't want to error out when logging, so, instead we simply log [REDACTED]
		redactedEmail = "[REDACTED]"
	}

	return slog.StringValue(redactedEmail)
}
