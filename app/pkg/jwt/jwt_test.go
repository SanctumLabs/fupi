package jwt

import (
	"log"
	"testing"
)

func TestJwtEncode(t *testing.T) {
	type testCase struct {
		uid         string
		signingKey  string
		issuer      string
		expireDelta int
		expectedErr error
	}

	testCases := []testCase{
		{
			uid:         "cbr8mmkbcv45shdhmeig",
			signingKey:  "aHR0cHM6Ly9jdXJ0ei5zYW5jdH",
			issuer:      "http:fupi-test.com",
			expireDelta: 1,
			expectedErr: nil,
		},
	}

	jwt := New()

	for _, tc := range testCases {
		actualToken, actualErr := jwt.Encode(tc.uid, tc.signingKey, tc.issuer, tc.expireDelta)

		if actualErr != nil && tc.expectedErr == nil {
			log.Fatalf("Encode(%s, %s, %s, %d) = (%s, %v), expected no error", tc.uid, tc.signingKey, tc.issuer, tc.expireDelta, actualToken, actualErr)
		}

		if actualErr == nil && tc.expectedErr != nil {
			log.Fatalf("Encode(%s, %s, %s, %d) = (%s, %v), expected error: %v", tc.uid, tc.signingKey, tc.issuer, tc.expireDelta, actualToken, actualErr, tc.expectedErr)
		}

		uid, _, err := jwt.Decode(actualToken, tc.issuer, tc.signingKey)
		if uid != tc.uid {
			log.Fatalf("expected user id to match %s != %s", uid, tc.uid)
		}

		if err != nil {
			log.Fatalf("expected decode error to be nil, found %v", err)
		}
	}
}

func TestJwtEncodeRefreshToken(t *testing.T) {
	type testCase struct {
		uid         string
		signingKey  string
		issuer      string
		expireDelta int
		expectedErr error
	}

	testCases := []testCase{
		{
			uid:         "cbr8mmkbcv45shdhmeig",
			signingKey:  "aHR0cHM6Ly9jdXJ0ei5zYW5jdH",
			issuer:      "http:fupi-test.com",
			expireDelta: 1,
			expectedErr: nil,
		},
	}

	jwt := New()

	for _, tc := range testCases {
		actualToken, actualErr := jwt.EncodeRefreshToken(tc.uid, tc.signingKey, tc.issuer, tc.expireDelta)

		if actualErr != nil && tc.expectedErr == nil {
			log.Fatalf("EncodeRefreshToken(%s, %s, %s, %d) = (%s, %v), expected no error", tc.uid, tc.signingKey, tc.issuer, tc.expireDelta, actualToken, actualErr)
		}

		if actualErr == nil && tc.expectedErr != nil {
			log.Fatalf("EncodeRefreshToken(%s, %s, %s, %d) = (%s, %v), expected error: %v", tc.uid, tc.signingKey, tc.issuer, tc.expireDelta, actualToken, actualErr, tc.expectedErr)
		}

		uid, _, err := jwt.Decode(actualToken, tc.issuer, tc.signingKey)
		if uid != tc.uid {
			log.Fatalf("expected user id to match %s != %s", uid, tc.uid)
		}

		if err != nil {
			log.Fatalf("expected decode error to be nil, found %v", err)
		}
	}
}
