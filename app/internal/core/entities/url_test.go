package entities

import (
	"testing"
	"time"

	"bou.ke/monkey"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/stretchr/testify/assert"
)

type urlTestCase struct {
	name          string
	userId        identifier.ID
	url           string
	alias         string
	expiresOn     time.Time
	keywords      []string
	expectedError error
}

var urlTestCases = []urlTestCase{
	{
		name:          "original url with invalid length should return error",
		userId:        identifier.New(),
		url:           "http://google",
		alias:         "",
		expiresOn:     time.Now().Add(time.Hour * 2),
		keywords:      []string{},
		expectedError: errdefs.ErrURLLength,
	},
	{
		name:          "original url with invalid localhost as host should return error",
		userId:        identifier.New(),
		url:           "http://localhost:8080",
		alias:         "",
		expiresOn:     time.Now().Add(time.Hour * 2),
		keywords:      []string{},
		expectedError: errdefs.ErrURLFiltered,
	},
	{
		name:          "original url with invalid 127.0.0.1 as host should return error",
		userId:        identifier.New(),
		url:           "http://127.0.0.1:8080",
		alias:         "",
		expiresOn:     time.Now().Add(time.Hour * 2),
		keywords:      []string{},
		expectedError: errdefs.ErrURLFiltered,
	},
	{
		name:          "original url with invalid scheme should return error",
		userId:        identifier.New(),
		url:           "xyz://xyx/xyz/z",
		alias:         "",
		expiresOn:     time.Now().Add(time.Hour * 2),
		keywords:      []string{},
		expectedError: errdefs.ErrURLInvalid,
	},
	{
		name:          "original url with filtered url should return error",
		userId:        identifier.New(),
		url:           "http://localhost/xxx",
		alias:         "",
		expiresOn:     time.Now().Add(time.Hour * 2),
		keywords:      []string{},
		expectedError: errdefs.ErrURLFiltered,
	},
	{
		name:          "should not return error with valid expires on date",
		userId:        identifier.New(),
		url:           "http://example.com",
		alias:         "",
		expiresOn:     time.Now().Add(time.Hour * 1),
		keywords:      []string{},
		expectedError: nil,
	},
}

func TestNewUrl(t *testing.T) {
	for _, tc := range urlTestCases {
		t.Run(tc.name, func(t *testing.T) {
			url, err := NewUrl(tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords)
			if err != tc.expectedError {
				t.Errorf("NewUrl(%v, %s, %s, %s, %v) = (%v, %v) expected error: %v, got: %v", tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords, url, err, tc.expectedError, err)
			}
		})
	}
}

func BenchmarkNewUrl(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for _, tc := range urlTestCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = NewUrl(tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords)
			}
		})
	}
}

type urlExpiryTestCase struct {
	urlTestCase
	duration time.Duration
}

func TestGetExpiryDuration(t *testing.T) {
	monkey.Patch(time.Now, func() time.Time {
		return time.Date(2022, 9, 19, 16, 20, 00, 651387237, time.UTC)
	})

	urlExpiryTestCases := []urlExpiryTestCase{
		{
			urlTestCase: urlTestCase{
				name:          "should return duration expiry for given URL",
				userId:        identifier.New(),
				url:           "http://example.com",
				alias:         "",
				expiresOn:     time.Now().Add(time.Hour * 1),
				keywords:      []string{},
				expectedError: nil,
			},
			duration: time.Until(time.Now().Add(time.Hour * 1)),
		},
	}

	for _, tc := range urlExpiryTestCases {
		t.Run(tc.name, func(t *testing.T) {
			url, err := NewUrl(tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords)
			if err != tc.expectedError {
				t.Errorf("NewUrl(%v, %s, %s, %s, %v) = (%v, %v) expected error: %v, got: %v", tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords, url, err, tc.expectedError, err)
			}
			actualExpiry := url.GetExpiryDuration()
			if tc.duration != actualExpiry {
				t.Errorf("url.GetExpiryDuration() = %d expected = %d", actualExpiry, tc.duration)
			}
		})
	}
}

type urlShortCodeTestCase struct {
	urlTestCase
	shortCode   string
	expectedErr error
}

var urlShortCodeTestCases = []urlShortCodeTestCase{
	{
		urlTestCase: urlTestCase{
			name:          "should return unique short code and set to new short code without error",
			userId:        identifier.New(),
			url:           "http://example.com",
			alias:         "",
			expiresOn:     time.Now().Add(time.Hour * 1),
			keywords:      []string{},
			expectedError: nil,
		},
		shortCode:   "abcdef",
		expectedErr: nil,
	},
	{
		urlTestCase: urlTestCase{
			name:          "should return unique short code and return error when setting to a new invalid short code",
			userId:        identifier.New(),
			url:           "http://example.com",
			alias:         "",
			expiresOn:     time.Now().Add(time.Hour * 1),
			keywords:      []string{},
			expectedError: nil,
		},
		shortCode:   "abcdefghij",
		expectedErr: errdefs.ErrURLInvalid,
	},
}

func TestUrlShortCode(t *testing.T) {
	for _, tc := range urlShortCodeTestCases {
		t.Run(tc.name, func(t *testing.T) {
			url, err := NewUrl(tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords)
			if err != tc.expectedError {
				t.Errorf("NewUrl(%v, %s, %s, %s, %v) = (%v, %v) expected error: %v, got: %v", tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords, url, err, tc.expectedError, err)
			}

			initialUniqueShortCode := url.GetShortCode()
			assert.NotEmpty(t, initialUniqueShortCode)

			actualErr := url.SetShortCode(tc.shortCode)

			if tc.expectedErr != nil {
				assert.Error(t, actualErr)
			} else {
				assert.NoError(t, actualErr)

				newUniqueShortCode := url.GetShortCode()

				if initialUniqueShortCode == newUniqueShortCode {
					t.Errorf("url.GetShortCode() = %s expected = %s", newUniqueShortCode, tc.shortCode)
				}
			}
		})
	}
}

func BenchmarkUrlShortCode(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for _, tc := range urlShortCodeTestCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				url, _ := NewUrl(tc.userId, tc.url, tc.alias, tc.expiresOn, tc.keywords)
				_ = url.SetShortCode(tc.shortCode)
			}
		})
	}
}
