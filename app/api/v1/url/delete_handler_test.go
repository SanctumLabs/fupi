//go:build legacy

package url

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/stretchr/testify/assert"
)

func TestDeleteUrlReturnsErrorWhenFailureToDeleteUrl(t *testing.T) {
	urlRouter, _, _, mockUrlWriteSvc := createUrlRouter(t)

	urlID := identifier.New()
	httpRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/urls", baseURI), nil)
	responseRecorder := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})

	mockUrlWriteSvc.
		EXPECT().
		Remove(urlID.String()).
		Return(errors.New("Failed to delete url for user"))

	urlRouter.deleteUrl(ctx)

	assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
}

func TestDeleteUrlReturnsOkWhenSuccessDeletingUrl(t *testing.T) {
	urlRouter, _, _, mockUrlWriteSvc := createUrlRouter(t)
	urlID := identifier.New()

	httpRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/urls", baseURI), nil)
	responseRecorder := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(responseRecorder)
	ctx.Request = httpRequest
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: urlID.String()})

	mockUrlWriteSvc.
		EXPECT().
		Remove(urlID.String()).
		Return(nil)

	urlRouter.deleteUrl(ctx)

	assert.Equal(t, http.StatusOK, responseRecorder.Code)
}
