package identity

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type phoneNumberTestCase struct {
	name          string
	value         string
	expectedError error
}

var phoneNumberTestCases = []phoneNumberTestCase{
	{
		name:          "Returns error when creating an invalid phone number",
		value:         "123",
		expectedError: errors.New("phone number 123 is invalid"),
	},
	{
		name:          "No error is returned when creating a valid phone number",
		value:         "+254723000000",
		expectedError: nil,
	},
}

func TestNewPhoneNumber(t *testing.T) {
	t.Parallel()

	for _, tc := range phoneNumberTestCases {
		t.Run(tc.name, func(t *testing.T) {
			phone, err := NewPhone(tc.value)
			if tc.expectedError != nil && err == nil {
				t.Errorf("NewPhone(%s) = (%v, %v) expected error: %v, got: %v", tc.value, phone, err, tc.expectedError, err)
			}
		})
	}
}

func BenchmarkNewPhone(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for _, tc := range phoneNumberTestCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = NewPhone(tc.value)
			}
		})
	}
}

type redactPhoneNumberTestCase struct {
	phoneNumber   string
	expected      string
	expectedError error
}

var redactPhoneNumberTestCases = []redactPhoneNumberTestCase{
	{
		phoneNumber:   "+254700000000",
		expected:      "+254700***000", // +2547000***000
		expectedError: nil,
	},
	{
		phoneNumber:   "+254780000000",
		expected:      "+254780***000",
		expectedError: nil,
	},
}

func TestRedactPhoneNumber(t *testing.T) {
	t.Parallel()

	for _, tc := range redactPhoneNumberTestCases {
		t.Run(fmt.Sprintf("redactPhoneNumber(%s)", tc.phoneNumber), func(t *testing.T) {
			phone, err := NewPhone(tc.phoneNumber)
			assert.NoError(t, err)

			actualRedactedPhoneNumber, actualErr := phone.redactPhoneNumber()

			if tc.expectedError != nil && actualErr == nil {
				t.Errorf("redactPhoneNumber(%s) = (%v, %v) expected error: %v, got: %v", tc.phoneNumber, actualRedactedPhoneNumber, actualErr, tc.expectedError, err)
			}

			if actualRedactedPhoneNumber != tc.expected {
				t.Errorf("redactPhoneNumber(%s) = (%v, %v) expected: %v, got: %v", tc.phoneNumber, actualRedactedPhoneNumber, actualErr, tc.expected, actualRedactedPhoneNumber)
			}
		})
	}
}
