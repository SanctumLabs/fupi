package validators

import (
	"testing"
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

type testCase struct {
	name        string
	input       string
	expectedErr error
}

var urlTestCases = []testCase{
	{
		name:        "valid url with protocol",
		input:       "https://www.google.com",
		expectedErr: nil,
	},
	{
		name:        "valid url without protocol",
		input:       "www.google.com",
		expectedErr: nil,
	},
	{
		name:        "invalid url with invalid protocol",
		input:       "htt://www.google.com",
		expectedErr: errdefs.ErrInvalidURL,
	},
}

func TestIsValidUrl(t *testing.T) {
	for _, tc := range urlTestCases {
		t.Run(tc.name, func(t *testing.T) {
			err := IsValidUrl(tc.input)
			if err != tc.expectedErr {
				t.Errorf("IsValidUrl(%s) = %v expected error %v, got %v", tc.input, err, tc.expectedErr, err)
			}
		})
	}
}

func BenchmarkIsValidUrl(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range urlTestCases {
			_ = IsValidUrl(tc.input)
		}
	}
}

var userIdTestCases = []testCase{
	{
		name:        "valid userId",
		input:       "820h08naera",
		expectedErr: nil,
	},
	{
		name:        "invalid userId",
		input:       "",
		expectedErr: errdefs.ErrInvalidUserId,
	},
}

func TestIsValidUserId(t *testing.T) {
	for _, tc := range userIdTestCases {
		t.Run(tc.name, func(t *testing.T) {
			err := IsValidUserId(tc.input)
			if err != tc.expectedErr {
				t.Errorf("IsValidUserId(%s) = %v expected error %v, got %v", tc.input, err, tc.expectedErr, err)
			}
		})
	}
}

func BenchmarkIsValidUserId(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range userIdTestCases {
			_ = IsValidUserId(tc.input)
		}
	}
}

var urlIdTestCases = []testCase{
	{
		name:        "valid urlId",
		input:       "820h08naera",
		expectedErr: nil,
	},
	{
		name:        "invalid urlId",
		input:       "",
		expectedErr: errdefs.ErrInvalidUrlId,
	},
}

func TestIsValidUrlId(t *testing.T) {
	for _, tc := range urlIdTestCases {
		t.Run(tc.name, func(t *testing.T) {
			err := IsValidUrlId(tc.input)
			if err != tc.expectedErr {
				t.Errorf("IsValidUrlId(%s) = %v expected error %v, got %v", tc.input, err, tc.expectedErr, err)
			}
		})
	}
}

func BenchmarkIsValidUrlId(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range urlIdTestCases {
			_ = IsValidUrlId(tc.input)
		}
	}
}

var customAliasTestCases = []testCase{
	{
		name:        "valid custom alias",
		input:       "123456",
		expectedErr: nil,
	},
	{
		name:        "invalid custom alias",
		input:       "3foaunepfueapinfpwae",
		expectedErr: errdefs.ErrInvalidCustomAlias,
	},
	{
		name:        "empty valid custom alias",
		input:       "",
		expectedErr: nil,
	},
}

func TestIsValidCustomAlias(t *testing.T) {
	for _, tc := range customAliasTestCases {
		t.Run(tc.name, func(t *testing.T) {
			err := IsValidCustomAlias(tc.input)
			if err != tc.expectedErr {
				t.Errorf("IsValidCustomAlias(%s) = %v expected error %v, got %v", tc.input, err, tc.expectedErr, err)
			}
		})
	}
}

func BenchmarkIsValidCustomAlias(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range customAliasTestCases {
			_ = IsValidCustomAlias(tc.input)
		}
	}
}

type expirationTimeTestCase struct {
	name        string
	input       time.Time
	expectedErr error
}

var expiresTimestampTestCases = []expirationTimeTestCase{
	{
		name:        "valid timestamp in the future",
		input:       time.Now().Add(time.Hour + 2),
		expectedErr: nil,
	},
	{
		name:        "invalid timestamp in the past",
		input:       time.Now().Add(-2),
		expectedErr: errdefs.ErrPastExpiration,
	},
}

func TestIsValidExpirationTime(t *testing.T) {
	for _, tc := range expiresTimestampTestCases {
		t.Run(tc.name, func(t *testing.T) {
			err := IsValidExpirationTime(tc.input)
			if err != tc.expectedErr {
				t.Errorf("IsValidExpirationTime(%s) = %v expected error %v, got %v", tc.input, err, tc.expectedErr, err)
			}
		})
	}
}

func BenchmarkIsValidExpirationTime(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range expiresTimestampTestCases {
			_ = IsValidExpirationTime(tc.input)
		}
	}
}
