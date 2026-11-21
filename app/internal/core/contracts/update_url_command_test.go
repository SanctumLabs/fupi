package contracts

import (
	"testing"
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
)

type testCase struct {
	name        string
	userId      string
	urlId       string
	customAlias string
	keywords    []string
	expiresOn   time.Time
	expectedErr error
}

var testCases = []testCase{
	{
		name:        "invalid userId should return error",
		userId:      "",
		urlId:       "323-2mdad3ad",
		customAlias: "",
		keywords:    []string{},
		expiresOn:   time.Now().Add(time.Hour + 1),
		expectedErr: errdefs.ErrInvalidUserId,
	},
	{
		name:        "valid urlId should return error",
		userId:      "ndoeaauneaf3a",
		urlId:       "",
		customAlias: "",
		keywords:    []string{},
		expiresOn:   time.Now().Add(time.Hour + 1),
		expectedErr: errdefs.ErrURLIdInvalid,
	},
	{
		name:        "invalid custom alias should return error",
		userId:      "3ipimpinpiea",
		urlId:       "323-2mdad3ad",
		customAlias: "foaufbneoanepnfeoufbefeefef",
		keywords:    []string{},
		expiresOn:   time.Now().Add(time.Hour + 1),
		expectedErr: errdefs.ErrInvalidCustomAlias,
	},
	{
		name:        "invalid expiration time should return error",
		userId:      "3ipimpinpiea",
		urlId:       "323-2mdad3ad",
		customAlias: "123456",
		keywords:    []string{},
		expiresOn:   time.Now().Add(-10),
		expectedErr: errdefs.ErrPastExpiration,
	},
}

func TestNewUpdateUrlRequest(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := NewUpdateUrlRequest(tc.userId, tc.urlId, tc.customAlias, tc.keywords, &tc.expiresOn)
			if err != tc.expectedErr {
				t.Errorf("NewUpdateUrlRequest(%s, %s, %s, %s, %v) = %v expected error %v, got %v", tc.userId, tc.urlId, tc.customAlias, tc.keywords, tc.expiresOn, tc.expectedErr, tc.expectedErr, err)
			} else if tc.expectedErr == nil {
				assert.Equal(t, tc.userId, actual.UserId)
				assert.Equal(t, tc.urlId, actual.UrlId)
				assert.Equal(t, tc.customAlias, actual.CustomAlias)
				assert.ElementsMatch(t, tc.keywords, actual.Keywords)
				assert.Equal(t, tc.expiresOn, actual.ExpiresOn)
			}
		})
	}
}

func BenchmarkNewUpdateUrlRequest(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			_, _ = NewUpdateUrlRequest(tc.userId, tc.urlId, tc.customAlias, tc.keywords, &tc.expiresOn)
		}
	}
}
