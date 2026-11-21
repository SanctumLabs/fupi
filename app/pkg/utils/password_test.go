package utils

import (
	"testing"

	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
)

type hashPasswordTestCase struct {
	name  string
	input string
	err   error
}

var hashPasswordTestCases = []hashPasswordTestCase{
	{
		name:  "should return error when password is empty",
		input: "",
		err:   errdefs.ErrInvalidPasswordLen,
	},
	{
		name:  "should correctly hash password and return hash with nil error",
		input: "some-password",
		err:   nil,
	},
}

func TestHashPassword(t *testing.T) {
	for _, tc := range hashPasswordTestCases {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := HashPassword(tc.input)
			if err != tc.err {
				t.Errorf("HashPassword(%s) = (%s, %v), expected error %v, got %v", tc.input, hash, err, tc.err, err)
			}
		})
	}
}

func BenchmarkHashPassword(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range hashPasswordTestCases {
			_, _ = HashPassword(tc.input)
		}
	}
}

type comparePasswordTestCase struct {
	name  string
	input string
}

var comparePasswordTestCases = []comparePasswordTestCase{
	{
		name:  "should return error when password is empty",
		input: "another-password",
	},
	{
		name:  "should return true for password input and nil error",
		input: "some-password",
	},
}

func TestComparePasswords(t *testing.T) {
	for _, tc := range comparePasswordTestCases {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := HashPassword(tc.input)
			if err != nil {
				t.Errorf("HashPassword(%s) = (%s, %v), expected no error", tc.input, hash, err)
			}

			actual, err := CompareHashAndPassword(hash, tc.input)

			if err != nil {
				t.Errorf("Compare(%s, %s) = (%v, %v), expected no error", hash, tc.input, actual, err)
			}

			assert.True(t, actual)
		})
	}
}

func BenchmarkComparePassword(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range comparePasswordTestCases {
			hash, _ := HashPassword(tc.input)
			_, _ = CompareHashAndPassword(hash, tc.input)
		}
	}
}
