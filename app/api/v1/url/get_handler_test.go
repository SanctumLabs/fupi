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
	"github.com/stretchr/testify/assert"
)

func TestGetUrlByIdReturnsNotFoundForMissingUrl(t *testing.T) {
	urlRouter, _, mockUrlReadSvc, _ := createUrlRouter(t)

	urlID := identifier.New()
	httpRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/urls/%s", baseURI, urlID.String()), nil)
	responseRecorder := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})

	mockUrlReadSvc.
		EXPECT().
		GetById(urlID.String()).
		Return(entities.URL{}, errors.New("Failed to find url"))

	urlRouter.getUrlById(ctx)

	assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
}

func TestGetByIdReturnsStatusOkForFoundUrl(t *testing.T) {
	urlRouter, _, mockUrlReadSvc, _ := createUrlRouter(t)

	userId := identifier.New()
	urlID := identifier.New()
	originalUrl := "http://google.com/some-long-url"
	customAlias := ""
	expiresOn := time.Now().Add(time.Hour * 1)

	httpRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/urls", baseURI), nil)
	responseRecorder := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})

	mockUrl, err := entities.NewUrl(userId, originalUrl, customAlias, expiresOn, []string{})
	assert.NoError(t, err)

	mockUrlReadSvc.
		EXPECT().
		GetById(urlID.String()).
		Return(*mockUrl, nil)

	urlRouter.getUrlById(ctx)

	var actualResponse map[string]any
	err = json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, responseRecorder.Code)

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
