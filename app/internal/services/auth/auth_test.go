//go:build legacy

package auth

import (
	"errors"
	"log"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/test/mocks"
)

type testCase struct {
	userID        string
	expectedToken string
	expectedErr   error
}

var authConfig = config.AuthConfig{
	Jwt: config.Jwt{
		Secret:             "jwt-secret",
		ExpireDelta:        5,
		RefreshExpireDelta: 1,
		Issuer:             "curtz-test",
	},
}

func TestGenerateToken(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockJwtGen := mocks.NewMockJwtGen(mockCtrl)

	testCases := []testCase{
		{
			userID:        identifier.New().String(),
			expectedToken: "header.body.signature",
			expectedErr:   nil,
		},
		{
			userID:        identifier.New().String(),
			expectedToken: "",
			expectedErr:   errors.New("failed to create access token"),
		},
	}

	for _, tc := range testCases {
		mockJwtGen.
			EXPECT().
			Encode(gomock.Eq(tc.userID), authConfig.Secret, authConfig.Issuer, authConfig.ExpireDelta).
			Return(tc.expectedToken, tc.expectedErr)

		svc := NewService(authConfig, mockJwtGen)

		actualToken, actualErr := svc.GenerateToken(tc.userID)

		if tc.expectedErr != nil && actualErr == nil {
			log.Fatalf("GenerateToken(%s) = (%s, %v), expected error %v", tc.userID, actualToken, actualErr, tc.expectedErr)
		}

		if tc.expectedErr == nil && actualErr != nil {
			log.Fatalf("GenerateToken(%s) = (%s, %v), expected no error", tc.userID, actualToken, actualErr)
		}

		if tc.expectedToken != actualToken {
			log.Fatalf("GenerateToken(%s) = (%s, %v), expected token %s", tc.userID, actualToken, actualErr, tc.expectedToken)
		}
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockJwtGen := mocks.NewMockJwtGen(mockCtrl)

	testCases := []testCase{
		{
			userID:        identifier.New().String(),
			expectedToken: "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJleHAiOjE2NjAzMjgzMTIsImlhdCI6MTY2MDMyNDcxMiwiaXNzIjoiY3VydHoiLCJzdWIiOiJjYnI4bW1rYmN2NDVzaGRobWVpZyIsImlkIjoiY2JyOG1ta2JjdjQ1c2hkaG1laWcifQ.XmMzGatF8J6x9ocrUv_l1HD3czCgy2_lFYPl2SZwYk8",
			expectedErr:   nil,
		},
		{
			userID:        identifier.New().String(),
			expectedToken: "",
			expectedErr:   errors.New("failed to create refresh token"),
		},
	}

	for _, tc := range testCases {
		mockJwtGen.
			EXPECT().
			EncodeRefreshToken(gomock.Eq(tc.userID), authConfig.Secret, authConfig.Issuer, authConfig.RefreshExpireDelta).
			Return(tc.expectedToken, tc.expectedErr)

		svc := NewService(authConfig, mockJwtGen)

		actualToken, actualErr := svc.GenerateRefreshToken(tc.userID)

		if tc.expectedErr != nil && actualErr == nil {
			log.Fatalf("GenerateRefreshToken(%s) = (%s, %v), expected error %v", tc.userID, actualToken, actualErr, tc.expectedErr)
		}

		if tc.expectedErr == nil && actualErr != nil {
			log.Fatalf("GenerateRefreshToken(%s) = (%s, %v), expected no error", tc.userID, actualToken, actualErr)
		}

		if tc.expectedToken != actualToken {
			log.Fatalf("GenerateRefreshToken(%s) = (%s, %v), expected token %s", tc.userID, actualToken, actualErr, tc.expectedToken)
		}
	}
}
