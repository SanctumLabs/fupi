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
	"github.com/golang/mock/gomock"
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/test/utils"
	"github.com/stretchr/testify/assert"
)

func TestCreateShortUrlReturnsBadRequestForInvalidJson(t *testing.T) {
	urlRouter, _, _, _ := createUrlRouter(t)

	httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/urls", baseURI), nil)

	responseRecorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest

	urlRouter.createShortUrl(ctx)

	assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
}

func TestCreateShortUrlReturnsUnauthorizedRequestForMissingUserInCtx(t *testing.T) {
	urlRouter, _, _, _ := createUrlRouter(t)

	httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/urls", baseURI), nil)

	responseRecorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest

	requestBody := createShortUrlDto{
		urlDto: urlDto{
			OriginalUrl: "http://localhost:9000/some-long-url",
			CustomAlias: "",
			ExpiresOn:   time.Now(),
			Keywords:    []string{},
		},
	}

	utils.MockRequestBody(ctx, requestBody)

	urlRouter.createShortUrl(ctx)

	assert.Equal(t, http.StatusUnauthorized, responseRecorder.Code)
}

func TestCreateShortUrlReturnsBadRequestForInvalidUrl(t *testing.T) {
	userId := "user-id"
	urlRouter, _, _, _ := createUrlRouter(t)

	httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/urls", baseURI), nil)

	responseRecorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest

	requestBody := createShortUrlDto{
		urlDto: urlDto{
			OriginalUrl: "long-long-url",
			CustomAlias: "",
			ExpiresOn:   time.Now(),
			Keywords:    []string{},
		},
	}

	ctx.Set("userId", userId)

	utils.MockRequestBody(ctx, requestBody)

	urlRouter.createShortUrl(ctx)

	assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
}

func TestCreateShortUrlReturnsBadRequestWhenCreateUrlReturnsError(t *testing.T) {
	userId := "user-id"
	urlRouter, _, _, mockUrlWriteSvc := createUrlRouter(t)

	httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/urls", baseURI), nil)

	responseRecorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest

	originalUrl := "http://example.com/some-long-url"
	customAlias := ""
	expiresOn := time.Now()
	keywords := []string{}

	requestBody := createShortUrlDto{
		urlDto: urlDto{
			OriginalUrl: originalUrl,
			CustomAlias: customAlias,
			ExpiresOn:   expiresOn,
			Keywords:    keywords,
		},
	}

	ctx.Set("userId", userId)

	mockUrlWriteSvc.
		EXPECT().
		CreateUrl(userId, originalUrl, customAlias, gomock.Any(), keywords).
		Return(entities.URL{}, errors.New("failed to shorten url"))

	utils.MockRequestBody(ctx, requestBody)

	urlRouter.createShortUrl(ctx)

	assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
}

func TestCreateShortUrlReturnsStatusCreatedWhenCreateUrlReturnsUrl(t *testing.T) {
	userId := identifier.New()
	urlRouter, _, _, mockUrlWriteSvc := createUrlRouter(t)

	httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/urls", baseURI), nil)

	responseRecorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest

	originalUrl := "http://google.com/some-long-url"
	customAlias := ""
	expiresOn := time.Now().Add(time.Hour * 1)
	keywords := []string{}

	requestBody := createShortUrlDto{
		urlDto: urlDto{
			OriginalUrl: originalUrl,
			CustomAlias: customAlias,
			ExpiresOn:   expiresOn,
			Keywords:    keywords,
		},
	}

	ctx.Set("userId", userId.String())

	mockUrl, err := entities.NewUrl(userId, originalUrl, customAlias, expiresOn, []string{})
	assert.NoError(t, err)

	mockUrlWriteSvc.
		EXPECT().
		CreateUrl(userId.String(), originalUrl, customAlias, gomock.Any(), keywords).
		Return(*mockUrl, nil)

	utils.MockRequestBody(ctx, requestBody)

	urlRouter.createShortUrl(ctx)

	var actualResponse map[string]any
	err = json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusCreated, responseRecorder.Code)

	expectedRespBody := gin.H{
		"id":           mockUrl.ID.String(),
		"user_id":      mockUrl.UserId.String(),
		"original_url": mockUrl.GetOriginalURL(),
		"custom_alias": mockUrl.GetCustomAlias(),
		"short_code":   mockUrl.GetShortCode(),
		"keywords":     mockUrl.GetKeywords(),
		"expires_on":   mockUrl.GetExpiresOn().Format(time.RFC3339Nano),
		"created_at":   mockUrl.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":   mockUrl.UpdatedAt.Format(time.RFC3339Nano),
		"hits":         mockUrl.GetHits(),
	}

	if _, ok := actualResponse["id"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["id"], mockUrl.ID.String())
	}

	if _, ok := actualResponse["user_id"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["user_id"], userId.String())
	}

	if _, ok := actualResponse["original_url"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["original_url"], originalUrl)
	}

	if _, ok := actualResponse["custom_alias"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["custom_alias"], customAlias)
	}

	if _, ok := actualResponse["short_code"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["short_code"], mockUrl.GetShortCode())
	}

	if _, ok := actualResponse["keywords"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["keywords"], mockUrl.GetKeywords())
	}

	if _, ok := actualResponse["hits"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["hits"], mockUrl.GetHits())
	}

	if _, ok := actualResponse["updated_at"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["updated_at"], mockUrl.UpdatedAt.Format(time.RFC3339Nano))
	}
	if _, ok := actualResponse["created_at"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["created_at"], mockUrl.CreatedAt.Format(time.RFC3339Nano))
	}

	if _, ok := actualResponse["expires_on"]; ok {
		assert.True(t, ok)
		assert.Equal(t, expectedRespBody["expires_on"], expiresOn.Format(time.RFC3339Nano))
	}
}
