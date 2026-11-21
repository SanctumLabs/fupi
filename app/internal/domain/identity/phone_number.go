package identity

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/sanctumlabs/curtz/app/pkg/validators"
)

// PhoneNumber is a value object representing a phone number in the system.
type PhoneNumber struct {
	value string
}

// NewPhone is a factory method that creates a Phone number
func NewPhone(value string) (PhoneNumber, error) {
	if err := validators.ValidatePhoneNumber(value); err != nil {
		return PhoneNumber{}, fmt.Errorf("phone number %s is invalid", value)
	}

	return PhoneNumber{
		value: value,
	}, nil
}

// Amount retrieves
func (pn *PhoneNumber) Value() string {
	return pn.value
}

// SetPhone sets the phone number
func (pn *PhoneNumber) SetPhone(phoneNumber string) error {
	if err := validators.ValidatePhoneNumber(phoneNumber); err != nil {
		return fmt.Errorf("phone number %s is invalid", phoneNumber)
	}
	pn.value = phoneNumber
	return nil
}

func (pn *PhoneNumber) String() string {
	return pn.value
}

// redactPhoneNumber formats a phone number to show the country code, first 3 digits, and last 3 digits, with the rest redacted.
func (pn PhoneNumber) redactPhoneNumber() (string, error) {
	// Regex to match the phone number pattern
	regex := regexp.MustCompile(`^(\+\d{1,3})(\d{3})(\d+)(\d{3})$`)
	matches := regex.FindStringSubmatch(pn.value)

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
func (pn PhoneNumber) LogValue() slog.Value {
	// Just show the first few digits
	redactedPhoneNumber, err := pn.redactPhoneNumber()
	if err != nil {
		// in this case don't want to error out when logging, so, instead we simply log [REDACTED]
		redactedPhoneNumber = "[REDACTED]"
	}

	return slog.StringValue(redactedPhoneNumber)
}
