package url

import (
	"errors"
	"testing"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

type customAliasTestCase struct {
	name  string
	input string
	err   error
}

var customAliasTestCases = []customAliasTestCase{
	{
		name:  "empty custom alias should return error",
		input: "",
		err:   errdefs.ErrCustomAliasInvalidLength,
	},
	{
		name:  "custom alias too short should return error",
		input: "ab",
		err:   errdefs.ErrCustomAliasInvalidLength,
	},
	{
		name:  "custom alias too long should return error",
		input: "abcdefghijklm",
		err:   errdefs.ErrCustomAliasInvalidLength,
	},
	{
		name:  "custom alias with invalid characters should return error",
		input: "test@alias",
		err:   errdefs.ErrCustomAliasInvalidCharacters,
	},
	{
		name:  "custom alias with spaces should return error",
		input: "test alias",
		err:   errdefs.ErrCustomAliasInvalidCharacters,
	},
	{
		name:  "valid custom alias should return no error",
		input: "test-alias",
		err:   nil,
	},
	{
		name:  "valid custom alias with numbers should return no error",
		input: "test123",
		err:   nil,
	},
	{
		name:  "valid custom alias with dashes and numbers should return no error",
		input: "test-123-al",
		err:   nil,
	},
	{
		name:  "custom alias at max length should return no error",
		input: "abcdefghijkl",
		err:   nil,
	},
}

func TestNewCustomAlias(t *testing.T) {
	for _, tc := range customAliasTestCases {
		t.Run(tc.name, func(t *testing.T) {
			customAlias, err := NewCustomAlias(tc.input)
			if !errors.Is(err, tc.err) {
				t.Errorf("NewCustomAlias(%s) = (%v, %v), expected error %v, got %v", tc.input, customAlias, err, tc.err, err)
			}
			if err == nil && customAlias.Value() != tc.input {
				t.Errorf("NewCustomAlias(%s) = %s, expected %s", tc.input, customAlias.Value(), tc.input)
			}
		})
	}
}

func BenchmarkNewCustomAlias(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range customAliasTestCases {
			_, _ = NewCustomAlias(tc.input)
		}
	}
}
