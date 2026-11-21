package url

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/test/data"
	"github.com/stretchr/testify/assert"
)

func TestGetAllUrlsReturnsUnauthorizedForMissingUserId(t *testing.T) {
	urlRouter, _, _, _ := createUrlRouter(t)

	httpRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/urls", baseURI), nil)
	responseRecorder := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest

	urlRouter.getAllUrls(ctx)

	assert.Equal(t, http.StatusUnauthorized, responseRecorder.Code)
}

func TestGetAllUrlsReturnsErrorWhenFailureToGetUrls(t *testing.T) {
	urlRouter, _, mockUrlReadSvc, _ := createUrlRouter(t)

	userID := identifier.New()
	httpRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/urls", baseURI), nil)
	responseRecorder := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest
	ctx.Set("userId", userID.String())

	mockUrlReadSvc.
		EXPECT().
		GetByUserId(userID.String()).
		Return([]entities.URL{}, errors.New("Failed to find all urls for user"))

	urlRouter.getAllUrls(ctx)

	assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
}

func TestGetAllUrlsReturnsOkWhenSuccessGettingUrls(t *testing.T) {
	urlRouter, _, mockUrlReadSvc, _ := createUrlRouter(t)

	httpRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/urls", baseURI), nil)
	responseRecorder := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest

	userID := identifier.New()
	ctx.Set("userId", userID.String())

	originalUrlOne := "http://google.com/some-long-url"
	originalUrlTwo := "http://bing.com/some-long-url"
	customAliasOne := ""
	customAliasTwo := ""
	expiresOnOne := time.Now().Add(time.Hour * 1)
	expiresOnTwo := time.Now().Add(time.Hour * 1)
	shortCodeOne := "342312"
	shortCodeTwo := "9hfaun"

	mockUrlOne := data.MockUrl(userID.String(), originalUrlOne, customAliasOne, shortCodeOne, expiresOnOne, []string{})
	mockUrlTwo := data.MockUrl(userID.String(), originalUrlTwo, customAliasTwo, shortCodeTwo, expiresOnTwo, []string{})

	mockUrls := []entities.URL{
		*mockUrlOne,
		*mockUrlTwo,
	}

	mockUrlReadSvc.
		EXPECT().
		GetByUserId(userID.String()).
		Return(mockUrls, nil)

	urlRouter.getAllUrls(ctx)

	var actualResponse []map[string]any

	err := json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, responseRecorder.Code)

	expectedRespBody := []gin.H{
		{
			"id":           mockUrlOne.ID.String(),
			"user_id":      mockUrlOne.UserId.String(),
			"original_url": mockUrlOne.GetOriginalURL(),
			"custom_alias": mockUrlOne.GetCustomAlias(),
			"short_code":   mockUrlOne.GetShortCode(),
			"keywords":     []any{},
			"expires_on":   mockUrlOne.GetExpiresOn().Format(time.RFC3339Nano),
			"created_at":   mockUrlOne.CreatedAt.Format(time.RFC3339Nano),
			"updated_at":   mockUrlOne.UpdatedAt.Format(time.RFC3339Nano),
			"hits":         mockUrlOne.GetHits(),
		},
		{
			"id":           mockUrlTwo.ID.String(),
			"user_id":      mockUrlTwo.UserId.String(),
			"original_url": mockUrlTwo.GetOriginalURL(),
			"custom_alias": mockUrlTwo.GetCustomAlias(),
			"short_code":   mockUrlTwo.GetShortCode(),
			"keywords":     []any{},
			"expires_on":   mockUrlTwo.GetExpiresOn().Format(time.RFC3339Nano),
			"created_at":   mockUrlTwo.CreatedAt.Format(time.RFC3339Nano),
			"updated_at":   mockUrlTwo.UpdatedAt.Format(time.RFC3339Nano),
			"hits":         mockUrlTwo.GetHits(),
		},
	}

	assert.Len(t, actualResponse, len(expectedRespBody))

	for idx, ar := range actualResponse {
		if _, ok := ar["id"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["id"], ar["id"])
		}

		if _, ok := ar["user_id"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["user_id"], ar["user_id"])
		}

		if _, ok := ar["original_url"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["original_url"], ar["original_url"])
		}

		if _, ok := ar["custom_alias"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["custom_alias"], ar["custom_alias"])
		}

		if _, ok := ar["short_code"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["short_code"], ar["short_code"])
		}

		if _, ok := ar["keywords"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["keywords"], ar["keywords"])
		}

		if _, ok := ar["updated_at"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["updated_at"], ar["updated_at"])
		}

		if _, ok := ar["created_at"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["created_at"], ar["created_at"])
		}

		if _, ok := ar["expires_on"]; ok {
			assert.True(t, ok)
			assert.Equal(t, expectedRespBody[idx]["expires_on"], ar["expires_on"])
		}
	}
}
