package auth

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
	"github.com/sanctumlabs/curtz/app/server/router"
	"github.com/sanctumlabs/curtz/app/test/data"
	"github.com/sanctumlabs/curtz/app/test/mocks"
	"github.com/sanctumlabs/curtz/app/test/utils"
	"github.com/stretchr/testify/assert"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var baseURI = "/api/v1/curtz"

func TestAuthHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Auth Handler Suite")
}

var _ = Describe("Auth Handler", func() {
	var (
		mockCtrl    *gomock.Controller
		mockUserSvc *mocks.MockUserService
		mockAuthSvc *mocks.MockAuthService
	)
	authRouter := &authRouter{}

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)

		mockCtrl = gomock.NewController(GinkgoT())
		mockUserSvc = mocks.NewMockUserService(mockCtrl)
		mockAuthSvc = mocks.NewMockAuthService(mockCtrl)
		authRouter.baseUri = baseURI
		authRouter.svc = mockUserSvc
		authRouter.authSvc = mockAuthSvc
		authRouter.routes = []router.Route{}

		routes := []router.Route{
			router.NewPostRoute(fmt.Sprintf("%s/auth/register", baseURI), authRouter.register),
			router.NewPostRoute(fmt.Sprintf("%s/auth/login", baseURI), authRouter.login),
			router.NewPostRoute(fmt.Sprintf("%s/auth/oauth/token", baseURI), authRouter.oauthToken),
		}

		authRouter.routes = append(authRouter.routes, routes...)
	})

	When("registering", func() {
		httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/auth/register", baseURI), nil)

		email := "johndoe@example.com"
		password := "password"

		requestBody := registerRequestDto{
			Email:    email,
			Password: password,
		}

		Context("and payload is empty", func() {
			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)
			ctx.Request = httpRequest

			It("should return 400 response code", func() {
				authRouter.register(ctx)

				assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)
			})
		})

		Context("and payload exists", func() {
			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)
			ctx.Request = httpRequest

			It("but there is an error creating a user should return a 400 response code", func() {
				mockUserSvc.
					EXPECT().
					CreateUser(email, password).
					Return(entities.User{}, errors.New("user already exists"))

				utils.MockRequestBody(ctx, requestBody)

				authRouter.register(ctx)

				assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)
			})

			It("and there is a success creating a user, should return a 201 response code", func() {
				responseRecorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(responseRecorder)
				ctx.Request = httpRequest

				mockUser, err := data.MockUser(email, password)
				assert.NoError(GinkgoT(), err)

				mockUserSvc.
					EXPECT().
					CreateUser(email, password).
					Return(mockUser, nil)

				utils.MockRequestBody(ctx, requestBody)

				authRouter.register(ctx)

				expectedRespBody := gin.H{
					"id":         mockUser.ID.String(),
					"email":      mockUser.Email.GetValue(),
					"created_at": mockUser.CreatedAt.Format(time.RFC3339Nano),
					"updated_at": mockUser.UpdatedAt.Format(time.RFC3339Nano),
				}

				var actualResponse map[string]string
				err = json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
				assert.NoError(GinkgoT(), err)

				assert.Equal(GinkgoT(), http.StatusCreated, responseRecorder.Code)

				if id, ok := actualResponse["id"]; ok {
					assert.True(GinkgoT(), ok)
					assert.Equal(GinkgoT(), expectedRespBody["id"], id)
				}

				if id, ok := actualResponse["email"]; ok {
					assert.True(GinkgoT(), ok)
					assert.Equal(GinkgoT(), expectedRespBody["email"], id)
				}

				if id, ok := actualResponse["created_at"]; ok {
					assert.True(GinkgoT(), ok)
					assert.Equal(GinkgoT(), expectedRespBody["created_at"], id)
				}

				if id, ok := actualResponse["updated_at"]; ok {
					assert.True(GinkgoT(), ok)
					assert.Equal(GinkgoT(), expectedRespBody["updated_at"], id)
				}
			})
		})
	})

	When("logging in", func() {
		httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/auth/login", baseURI), nil)

		Context("and payload is empty", func() {
			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)
			ctx.Request = httpRequest

			It("should return 400 response code", func() {
				authRouter.login(ctx)

				assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)
			})
		})

		Context("and the user does not exist", func() {
			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)
			ctx.Request = httpRequest

			It("should return a 401 response code", func() {
				email := "johndoe@example.com"
				password := "password"

				requestBody := loginRequestDto{
					Email:    email,
					Password: password,
				}

				mockUserSvc.
					EXPECT().
					GetUserByEmail(email).
					Return(entities.User{}, errors.New("user does not exist"))

				utils.MockRequestBody(ctx, requestBody)

				authRouter.login(ctx)

				expectedRespBody, err := json.Marshal(gin.H{
					"message": "Invalid Email or Password",
				})

				assert.NoError(GinkgoT(), err)
				assert.Equal(GinkgoT(), http.StatusUnauthorized, responseRecorder.Code)
				assert.Equal(GinkgoT(), expectedRespBody, responseRecorder.Body.Bytes())
			})
		})

		Context("and the user exists", func() {
			It("but the password supplied is invalid, should return a 401 response code", func() {
				responseRecorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(responseRecorder)
				ctx.Request = httpRequest

				email := "johndoe@example.com"
				password := "wrong-password"
				correctPassword := "correct-password"

				requestBody := loginRequestDto{
					Email:    email,
					Password: password,
				}

				user, err := data.MockUser(email, correctPassword)
				assert.NoError(GinkgoT(), err)

				mockUserSvc.
					EXPECT().
					GetUserByEmail(email).
					Return(user, nil)

				utils.MockRequestBody(ctx, requestBody)

				authRouter.login(ctx)

				expectedRespBody, err := json.Marshal(gin.H{
					"message": "Invalid Email or Password",
				})

				assert.NoError(GinkgoT(), err)
				assert.Equal(GinkgoT(), http.StatusUnauthorized, responseRecorder.Code)
				assert.Equal(GinkgoT(), expectedRespBody, responseRecorder.Body.Bytes())
			})

			Context("and the password supplied is valid", func() {
				email := "johndoe@example.com"
				password := "password"
				accessToken := "header.payload.signature"
				refreshToken := "header.payload.signature"

				requestBody := loginRequestDto{
					Email:    email,
					Password: password,
				}

				user, _ := data.MockUser(email, password)

				When("there is a failure to generate", func() {

					It("an access token, should return a 400 response code", func() {
						responseRecorder := httptest.NewRecorder()
						ctx, _ := gin.CreateTestContext(responseRecorder)
						ctx.Request = httpRequest

						err := errors.New("failed to supply access token")

						mockUserSvc.
							EXPECT().
							GetUserByEmail(email).
							Return(user, nil)

						mockAuthSvc.
							EXPECT().
							GenerateToken(user.ID.String()).
							Return("", err)

						utils.MockRequestBody(ctx, requestBody)

						authRouter.login(ctx)

						expectedRespBody, err := json.Marshal(gin.H{
							"message": err.Error(),
						})
						assert.NoError(GinkgoT(), err)

						assert.Equal(GinkgoT(), expectedRespBody, responseRecorder.Body.Bytes())
						assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)
					})

					It("refresh token should return a 400 response code", func() {
						responseRecorder := httptest.NewRecorder()
						ctx, _ := gin.CreateTestContext(responseRecorder)
						ctx.Request = httpRequest

						err := errors.New("failed to supply refresh token")

						mockUserSvc.
							EXPECT().
							GetUserByEmail(email).
							Return(user, nil)

						mockAuthSvc.
							EXPECT().
							GenerateToken(user.ID.String()).
							Return(accessToken, nil)

						mockAuthSvc.
							EXPECT().
							GenerateRefreshToken(user.ID.String()).
							Return(refreshToken, err)

						utils.MockRequestBody(ctx, requestBody)

						authRouter.login(ctx)

						expectedRespBody, err := json.Marshal(gin.H{
							"message": err.Error(),
						})

						assert.NoError(GinkgoT(), err)
						assert.Equal(GinkgoT(), expectedRespBody, responseRecorder.Body.Bytes())
						assert.Equal(GinkgoT(), http.StatusBadRequest, responseRecorder.Code)
					})
				})

				When("there is success generating both access and refresh tokens", func() {
					It("should return 200 response code with user information and tokens", func() {
						responseRecorder := httptest.NewRecorder()
						ctx, _ := gin.CreateTestContext(responseRecorder)
						ctx.Request = httpRequest
						mockUserSvc.
							EXPECT().
							GetUserByEmail(email).
							Return(user, nil)

						mockAuthSvc.
							EXPECT().
							GenerateToken(user.ID.String()).
							Return(accessToken, nil)

						mockAuthSvc.
							EXPECT().
							GenerateRefreshToken(user.ID.String()).
							Return(refreshToken, nil)

						utils.MockRequestBody(ctx, requestBody)

						authRouter.login(ctx)

						expectedRespBody := gin.H{
							"id":            user.ID.String(),
							"email":         user.Email.GetValue(),
							"created_at":    user.CreatedAt.Format(time.RFC3339Nano),
							"updated_at":    user.UpdatedAt.Format(time.RFC3339Nano),
							"access_token":  accessToken,
							"refresh_token": refreshToken,
						}

						var actualResponse map[string]string
						err := json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
						assert.NoError(GinkgoT(), err)

						assert.Equal(GinkgoT(), http.StatusOK, responseRecorder.Code)

						if id, ok := actualResponse["id"]; ok {
							assert.True(GinkgoT(), ok)
							assert.Equal(GinkgoT(), expectedRespBody["id"], id)
						}

						if id, ok := actualResponse["email"]; ok {
							assert.True(GinkgoT(), ok)
							assert.Equal(GinkgoT(), expectedRespBody["email"], id)
						}

						if id, ok := actualResponse["created_at"]; ok {
							assert.True(GinkgoT(), ok)
							assert.Equal(GinkgoT(), expectedRespBody["created_at"], id)
						}

						if id, ok := actualResponse["updated_at"]; ok {
							assert.True(GinkgoT(), ok)
							assert.Equal(GinkgoT(), expectedRespBody["updated_at"], id)
						}
						if id, ok := actualResponse["access_token"]; ok {
							assert.True(GinkgoT(), ok)
							assert.Equal(GinkgoT(), expectedRespBody["access_token"], id)
						}
						if id, ok := actualResponse["refresh_token"]; ok {
							assert.True(GinkgoT(), ok)
							assert.Equal(GinkgoT(), expectedRespBody["refresh_token"], id)
						}
					})
				})
			})
		})
	})

	When("refreshing a token", func() {

		Context("and grant_type is not refresh token", func() {
			httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/auth/oauth/token", baseURI), nil)
			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)
			ctx.Request = httpRequest

			It("should return 401 response code", func() {
				authRouter.oauthToken(ctx)

				assert.Equal(GinkgoT(), http.StatusUnauthorized, responseRecorder.Code)
			})
		})

		Context("and refresh_token is missing from query params", func() {
			httpRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/auth/oauth/token", baseURI), nil)
			responseRecorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(responseRecorder)

			ctx.Params = append(ctx.Params, gin.Param{Key: "grant_type", Value: "refresh_token"})
			ctx.Request = httpRequest

			It("should return 401 response code", func() {
				authRouter.oauthToken(ctx)

				assert.Equal(GinkgoT(), http.StatusUnauthorized, responseRecorder.Code)
			})
		})

		Context("and both grant_type & refresh_token are in the query params", func() {
			refreshToken := "header.payload.signature"
			userID := "userId"
			mockUser, err := data.MockUser("johndoe@curtz.com", "password")
			assert.NoError(GinkgoT(), err)

			httpRequest := httptest.NewRequest(http.MethodPost,
				fmt.Sprintf("%s/auth/oauth/token?grant_type=%s&refresh_token=%s", baseURI, "refresh_token", refreshToken),
				nil,
			)

			It("should return 401 response code when refresh token has expired or is invalid", func() {
				responseRecorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(responseRecorder)

				ctx.Request = httpRequest

				err := errors.New("invalid refresh token")

				mockAuthSvc.
					EXPECT().
					Authenticate(refreshToken).
					Return("", time.Time{}, err)

				authRouter.oauthToken(ctx)

				assert.Equal(GinkgoT(), http.StatusUnauthorized, responseRecorder.Code)
			})

			It("should return 401 response code when user does not exist", func() {
				responseRecorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(responseRecorder)

				ctx.Request = httpRequest

				err := errors.New("user does not exist")

				mockAuthSvc.
					EXPECT().
					Authenticate(refreshToken).
					Return(userID, time.Time{}, nil)

				mockUserSvc.
					EXPECT().
					GetUserByID(userID).
					Return(entities.User{}, err)

				authRouter.oauthToken(ctx)

				assert.Equal(GinkgoT(), http.StatusUnauthorized, responseRecorder.Code)
			})

			It("should return 500 response code when there is a failure to generate access token", func() {
				responseRecorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(responseRecorder)

				ctx.Request = httpRequest

				err := errors.New("failed to generate access token")

				mockAuthSvc.
					EXPECT().
					Authenticate(refreshToken).
					Return(userID, time.Time{}, nil)

				mockUserSvc.
					EXPECT().
					GetUserByID(userID).
					Return(mockUser, nil)

				mockAuthSvc.
					EXPECT().
					GenerateToken(userID).
					Return("", err)

				authRouter.oauthToken(ctx)

				assert.Equal(GinkgoT(), http.StatusInternalServerError, responseRecorder.Code)
			})

			It("should return 500 response code when there is a failure to generate new refresh token", func() {
				responseRecorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(responseRecorder)

				ctx.Request = httpRequest

				err := errors.New("failed to generate refresh token")

				mockAuthSvc.
					EXPECT().
					Authenticate(refreshToken).
					Return(userID, time.Time{}, nil)

				mockUserSvc.
					EXPECT().
					GetUserByID(userID).
					Return(mockUser, nil)

				mockAuthSvc.
					EXPECT().
					GenerateToken(userID).
					Return("header.payload.signature", nil)

				mockAuthSvc.
					EXPECT().
					GenerateRefreshToken(userID).
					Return("", err)

				authRouter.oauthToken(ctx)

				assert.Equal(GinkgoT(), http.StatusInternalServerError, responseRecorder.Code)
			})

			It("should return 200 response code when there is a success generating a new access token", func() {
				newAccessToken := "header2.payload2.signature2"
				newRefreshToken := "header2.payload2.signature2"

				responseRecorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(responseRecorder)

				ctx.Request = httpRequest

				mockAuthSvc.
					EXPECT().
					Authenticate(refreshToken).
					Return(userID, time.Time{}, nil)

				mockUserSvc.
					EXPECT().
					GetUserByID(userID).
					Return(mockUser, nil)

				mockAuthSvc.
					EXPECT().
					GenerateToken(userID).
					Return(newAccessToken, nil)

				mockAuthSvc.
					EXPECT().
					GenerateRefreshToken(userID).
					Return(newRefreshToken, nil)

				authRouter.oauthToken(ctx)

				expectedRespBody := gin.H{
					"access_token":  newAccessToken,
					"refresh_token": newRefreshToken,
				}

				var actualResponse map[string]string
				err := json.Unmarshal([]byte(responseRecorder.Body.Bytes()), &actualResponse)
				assert.NoError(GinkgoT(), err)

				if accessToken, ok := actualResponse["access_token"]; ok {
					assert.True(GinkgoT(), ok)
					assert.Equal(GinkgoT(), expectedRespBody["access_token"], accessToken)
				}

				if refreshToken, ok := actualResponse["refresh_token"]; ok {
					assert.True(GinkgoT(), ok)
					assert.Equal(GinkgoT(), expectedRespBody["refresh_token"], refreshToken)
				}

				assert.Equal(GinkgoT(), http.StatusOK, responseRecorder.Code)
			})
		})
	})
})
