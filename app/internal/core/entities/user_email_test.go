package entities

import (
	"testing"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
)

type userEmailTestCase struct {
	name  string
	input string
	err   error
}

var userEmailTestCases = []userEmailTestCase{
	{
		name:  "empty email should return nil and error",
		input: "",
		err:   errdefs.ErrEmailInvalid,
	},
	{
		name:  "invalid email should return nil and error",
		input: "johndoe",
		err:   errdefs.ErrEmailInvalid,
	},
	{
		name:  "invalid email should return nil and error",
		input: "johndoe@",
		err:   errdefs.ErrEmailInvalid,
	},
	{
		name:  "valid email should return email and nil error",
		input: "johndoe@example.com",
		err:   nil,
	},
}

func TestNewEmail(t *testing.T) {
	for _, tc := range userEmailTestCases {
		t.Run(tc.name, func(t *testing.T) {
			email, err := NewEmail(tc.input)
			if err != tc.err {
				t.Errorf("NewEmail(%s) = (%v, %v), expected error %v, got %v", tc.input, email, err, tc.err, err)
			}
		})
	}
}

func BenchmarkNewEmail(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range userEmailTestCases {
			_, _ = NewEmail(tc.input)
		}
	}
}

type userSetEmailTestCase struct {
	userEmailTestCase
	initialValue string
}

var userSetEmailTestCases = []userSetEmailTestCase{
	{
		userEmailTestCase: userEmailTestCase{
			name:  "empty email should return nil and error",
			input: "",
			err:   errdefs.ErrEmailInvalid,
		},
		initialValue: "johndoe@example.com",
	},
	{
		userEmailTestCase: userEmailTestCase{name: "invalid email should return nil and error",
			input: "johndoe",
			err:   errdefs.ErrEmailInvalid,
		},
		initialValue: "johndoe@example.com",
	},
	{
		userEmailTestCase: userEmailTestCase{
			name:  "invalid email should return nil and error",
			input: "johndoe@",
			err:   errdefs.ErrEmailInvalid,
		},
		initialValue: "jondoe@example.com",
	},
	{
		userEmailTestCase: userEmailTestCase{
			name:  "valid email should return email and nil error",
			input: "johndoe@example.com",
			err:   nil,
		},
		initialValue: "janedoe@example.com",
	},
}

func TestSetEmail(t *testing.T) {
	for _, tc := range userSetEmailTestCases {
		t.Run(tc.name, func(t *testing.T) {
			email, err := NewEmail(tc.initialValue)
			assert.NoError(t, err)

			actualErr := email.SetValue(tc.input)

			if actualErr != tc.err {
				t.Errorf("SetValue(%s) = %v, expected error %v, got %v", tc.input, actualErr, tc.err, actualErr)
			}

			if tc.err == nil {
				actualValue := email.GetValue()
				assert.Equal(t, tc.input, actualValue)
			}
		})
	}
}

func BenchmarkSetEmail(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range userSetEmailTestCases {
			email, _ := NewEmail(tc.initialValue)
			_ = email.SetValue(tc.input)
		}
	}
}
