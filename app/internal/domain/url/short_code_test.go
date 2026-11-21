package url

import (
	"errors"
	"testing"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

type shortCodeTestCase struct {
	name  string
	input string
	err   error
}

var shortCodeTestCases = []shortCodeTestCase{
	{
		name:  "empty short code should return error",
		input: "",
		err:   errdefs.ErrShortCodeInvalidLength,
	},
	{
		name:  "short code too short should return error",
		input: "abcde",
		err:   errdefs.ErrShortCodeInvalidLength,
	},
	{
		name:  "short code too long should return error",
		input: "abcdefghijk",
		err:   errdefs.ErrShortCodeInvalidLength,
	},
	{
		name:  "short code with invalid characters should return error",
		input: "abc@def",
		err:   errdefs.ErrShortCodeInvalidCharacters,
	},
	{
		name:  "short code with spaces should return error",
		input: "abc def",
		err:   errdefs.ErrShortCodeInvalidCharacters,
	},
	{
		name:  "short code with lowercase letters should return no error",
		input: "abcdef",
		err:   nil,
	},
	{
		name:  "short code with uppercase letters should return no error",
		input: "ABCDEF",
		err:   nil,
	},
	{
		name:  "short code with numbers should return no error",
		input: "abc123",
		err:   nil,
	},
	{
		name:  "short code with mixed case and numbers should return no error",
		input: "AbC123",
		err:   nil,
	},
	{
		name:  "short code at minimum length should return no error",
		input: "abcdef",
		err:   nil,
	},
	{
		name:  "short code at maximum length should return no error",
		input: "abcdefghij",
		err:   nil,
	},
}

func TestNewShortCode(t *testing.T) {
	for _, tc := range shortCodeTestCases {
		t.Run(tc.name, func(t *testing.T) {
			shortCode, err := NewShortCode(tc.input)
			if !errors.Is(err, tc.err) {
				t.Errorf("NewShortCode(%s) = (%v, %v), expected error %v, got %v", tc.input, shortCode, err, tc.err, err)
			}
			if err == nil && shortCode.Value() != tc.input {
				t.Errorf("NewShortCode(%s) = %s, expected %s", tc.input, shortCode.Value(), tc.input)
			}
		})
	}
}

func BenchmarkNewShortCode(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range shortCodeTestCases {
			_, _ = NewShortCode(tc.input)
		}
	}
}
