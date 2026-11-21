package url

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gin "github.com/gin-gonic/gin"
	"github.com/golang/mock/gomock"
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/pkg/encoding"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/server/router"
	"github.com/sanctumlabs/curtz/app/test/mocks"
	"github.com/sanctumlabs/curtz/app/test/utils"
	"github.com/stretchr/testify/assert"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUpdateUrlHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Update URL Handler Test Suite")
}

var _ = Describe("Update URL Handler", func() {
	var (
		mockCtrl        *gomock.Controller
		mockUrlSvc      *mocks.MockUrlService
		mockUrlReadSvc  *mocks.MockUrlReadService
		mockUrlWriteSvc *mocks.MockUrlWriteService
	)
	urlRouter := &urlRouter{}

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockCtrl = gomock.NewController(GinkgoT())
		mockUrlSvc = mocks.NewMockUrlService(mockCtrl)
		mockUrlReadSvc = mocks.NewMockUrlReadService(mockCtrl)
		mockUrlWriteSvc = mocks.NewMockUrlWriteService(mockCtrl)

		urlRouter.urlSvc = mockUrlSvc
		urlRouter.urlReadSvc = mockUrlReadSvc
		urlRouter.urlWriteSvc = mockUrlWriteSvc
		urlRouter.baseUri = baseURI
		urlRouter.routes = []router.Route{}

		routes := []router.Route{
			router.NewPostRoute(fmt.Sprintf("%s/urls", baseURI), urlRouter.createShortUrl),
			router.NewGetRoute(fmt.Sprintf("%s/urls", baseURI), urlRouter.getAllUrls),
			router.NewGetRoute(fmt.Sprintf("%s/urls/:id", baseURI), urlRouter.getUrlById),
			router.NewDeleteRoute(fmt.Sprintf("%s/urls/:id", baseURI), urlRouter.deleteUrl),
			router.NewPatchRoute(fmt.Sprintf("%s/urls/:id", baseURI), urlRouter.updateUrl),
		}

		urlRouter.routes = append(urlRouter.routes, routes...)
	})

	httpRequest := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("%s/urls", baseURI), nil)

	It("Missing UserId should return unauthorized status code", func() {
		responseRecorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(responseRecorder)
		ctx.Request = httpRequest

		urlID := identifier.New()

		ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})

		urlRouter.updateUrl(ctx)

		assert.Equal(GinkgoT(), http.StatusUnauthorized, responseRecorder.Code)
	})

	It("Should return NotFound status code if there is an error getting url given it's id", func() {
		responseRecorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(responseRecorder)
		ctx.Request = httpRequest

		urlID := identifier.New()
		userId := "userId"

		ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})
		ctx.Set("userId", userId)

		mockUrlReadSvc.
			EXPECT().
			GetById(urlID.String()).
			Return(entities.URL{}, errors.New("some error"))

		urlRouter.updateUrl(ctx)

		assert.Equal(GinkgoT(), http.StatusNotFound, responseRecorder.Code)
	})

	Context("Should return BadRequest status code if there is invalid data in the payload", func() {
		urlID := identifier.New()
		userId := "userId"
		originalUrl := "http://google.com/some-long-url"
		customAlias := ""
		expiresOn := time.Now().Add(time.Hour * 1)
		shortCode, _ := encoding.GetUniqueShortCode()

		mockUrl, err := entities.NewUrl(identifier.New().FromString(userId), originalUrl, customAlias, expiresOn, []string{})
		assert.NoError(GinkgoT(), err)

		err = mockUrl.SetShortCode(shortCode)
		assert.NoError(GinkgoT(), err)

		It("Invalid CustomAlias field", func() {
			mockUrlReadSvc.
				EXPECT().
				GetById(urlID.String()).
				Return(*mockUrl, nil)

			expiresOnField := time.Now().Add(time.Hour + 1)
			requestBody := updateShortUrlDto{
				CustomAlias: "bofeoubnojnoauear",
				ExpiresOn:   &expiresOnField,
				Keywords:    []string{},
			}

			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)
			ctx.Request = httpRequest
			ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})
			ctx.Set("userId", userId)

			utils.MockRequestBody(ctx, requestBody)

			urlRouter.updateUrl(ctx)

			var actualResponse map[string]any
			err := json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
			assert.NoError(GinkgoT(), err)

			assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)

			expectedResponseBody := map[string]any{
				"message": "custom alias is invalid",
			}

			if message, ok := actualResponse["message"]; ok {
				assert.True(GinkgoT(), ok)
				assert.Equal(GinkgoT(), expectedResponseBody["message"], message)
			}
		})

		It("Invalid ExpiresOn field", func() {
			mockUrlReadSvc.
				EXPECT().
				GetById(urlID.String()).
				Return(*mockUrl, nil)

			expiresOnField := time.Now().Add(-10)
			requestBody := updateShortUrlDto{
				CustomAlias: "123456",
				ExpiresOn:   &expiresOnField,
				Keywords:    []string{},
			}

			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)
			ctx.Request = httpRequest
			ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})
			ctx.Set("userId", userId)

			utils.MockRequestBody(ctx, requestBody)

			urlRouter.updateUrl(ctx)

			var actualResponse map[string]any
			err := json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
			assert.NoError(GinkgoT(), err)

			assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)

			expectedResponseBody := map[string]any{
				"message": "expires_on can not be a date in the past",
			}

			if message, ok := actualResponse["message"]; ok {
				assert.True(GinkgoT(), ok)
				assert.Equal(GinkgoT(), expectedResponseBody["message"], message)
			}
		})
	})

	It("Should return BadRequest when there is an error updating url", func() {
		urlID := identifier.New()
		userId := "userId"
		originalUrl := "http://google.com/some-long-url"
		customAlias := ""
		expiresOn := time.Now().Add(time.Hour * 1)
		shortCode, _ := encoding.GetUniqueShortCode()

		mockUrl, err := entities.NewUrl(identifier.New().FromString(userId), originalUrl, customAlias, expiresOn, []string{})
		assert.NoError(GinkgoT(), err)

		err = mockUrl.SetShortCode(shortCode)
		assert.NoError(GinkgoT(), err)

		mockUrlReadSvc.
			EXPECT().
			GetById(urlID.String()).
			Return(*mockUrl, nil)

		expiresOnField := time.Now().Add(time.Hour + 10)

		requestBody := updateShortUrlDto{
			CustomAlias: "123456",
			ExpiresOn:   &expiresOnField,
			Keywords:    []string{},
		}

		mockUrlWriteSvc.
			EXPECT().
			UpdateUrl(gomock.Any()).
			Return(entities.URL{}, errors.New("Error updating url"))

		responseRecorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(responseRecorder)
		ctx.Request = httpRequest
		ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})
		ctx.Set("userId", userId)

		utils.MockRequestBody(ctx, requestBody)

		urlRouter.updateUrl(ctx)

		var actualResponse map[string]any
		err = json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
		assert.NoError(GinkgoT(), err)

		assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)

		expectedResponseBody := map[string]any{
			"message": "Error updating url",
		}

		if message, ok := actualResponse["message"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["message"], message)
		}
	})

	It("Should return StatusAccepted when there is a success updating url", func() {
		urlID := identifier.New()
		userId := "userId"
		originalUrl := "http://google.com/some-long-url"
		customAlias := ""
		expiresOn := time.Now().Add(time.Hour * 1)
		shortCode, _ := encoding.GetUniqueShortCode()

		mockUrl, err := entities.NewUrl(identifier.New().FromString(userId), originalUrl, customAlias, expiresOn, []string{})
		assert.NoError(GinkgoT(), err)

		err = mockUrl.SetShortCode(shortCode)
		assert.NoError(GinkgoT(), err)

		mockUrlReadSvc.
			EXPECT().
			GetById(urlID.String()).
			Return(*mockUrl, nil)

		expiresOnField := time.Now().Add(time.Hour + 10)
		requestBody := updateShortUrlDto{
			CustomAlias: "123456",
			ExpiresOn:   &expiresOnField,
			Keywords:    []string{},
		}

		mockUpdatedUrl, err := entities.NewUrl(identifier.New().FromString(userId), originalUrl, requestBody.CustomAlias, *requestBody.ExpiresOn, []string{})
		assert.NoError(GinkgoT(), err)

		err = mockUpdatedUrl.SetShortCode(shortCode)
		assert.NoError(GinkgoT(), err)

		mockUrlWriteSvc.
			EXPECT().
			UpdateUrl(gomock.Any()).
			Return(*mockUpdatedUrl, nil)

		responseRecorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(responseRecorder)
		ctx.Request = httpRequest
		ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})
		ctx.Set("userId", userId)

		utils.MockRequestBody(ctx, requestBody)

		urlRouter.updateUrl(ctx)

		var actualResponse map[string]any
		err = json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
		assert.NoError(GinkgoT(), err)

		assert.Equal(GinkgoT(), http.StatusAccepted, responseRecorder.Code)

		expectedResponseBody := map[string]any{
			"id":           mockUpdatedUrl.ID.String(),
			"user_id":      mockUpdatedUrl.UserId.String(),
			"original_url": mockUpdatedUrl.GetOriginalURL(),
			"custom_alias": mockUpdatedUrl.GetCustomAlias(),
			"short_code":   mockUpdatedUrl.GetShortCode(),
			"keywords":     requestBody.Keywords,
			"expires_on":   mockUpdatedUrl.GetExpiresOn().Format(time.RFC3339Nano),
			"created_at":   mockUpdatedUrl.CreatedAt.Format(time.RFC3339Nano),
			"updated_at":   mockUpdatedUrl.UpdatedAt.Format(time.RFC3339Nano),
			"hits":         mockUpdatedUrl.GetHits(),
		}

		if id, ok := actualResponse["id"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["id"], id)
		}

		if actualUserId, ok := actualResponse["user_id"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["user_id"], actualUserId)
		}

		if actualOriginalUrl, ok := actualResponse["original_url"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["original_url"], actualOriginalUrl)
		}

		if actualCustomAlias, ok := actualResponse["custom_alias"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["custom_alias"], actualCustomAlias)
		}

		if actualShortCode, ok := actualResponse["short_code"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["short_code"], actualShortCode)
		}

		if actualHits, ok := actualResponse["hits"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["hits"], uint(actualHits.(float64)))
		}

		if actualUpdatedAt, ok := actualResponse["updated_at"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["updated_at"], actualUpdatedAt)
		}

		if actualCreatedAt, ok := actualResponse["created_at"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["created_at"], actualCreatedAt)
		}

		if actualExpiresOn, ok := actualResponse["expires_on"]; ok {
			assert.True(GinkgoT(), ok)
			assert.Equal(GinkgoT(), expectedResponseBody["expires_on"], actualExpiresOn)
		}
	})

})
