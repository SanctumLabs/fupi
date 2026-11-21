package identityapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	identityapp "github.com/sanctumlabs/curtz/app/internal/application/identity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/curtz/app/internal/domain/identity/mocks"
	mockports "github.com/sanctumlabs/curtz/app/internal/ports/mocks"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/router"
	"github.com/sanctumlabs/curtz/app/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

const baseURI = "/api/v1/curtz"

type IdentityHandlersTestSuite struct {
	suite.Suite
	mockCtrl     *gomock.Controller
	mockUsers    *mockidentity.MockUserDatastore
	mockTokens   *mockports.MockTokenService
	mockNotifier *mockports.MockNotifier
	app          *fiber.App
}

func TestIdentityHandlersTestSuite(t *testing.T) {
	suite.Run(t, new(IdentityHandlersTestSuite))
}

func (suite *IdentityHandlersTestSuite) SetupTest() {
	suite.mockCtrl = gomock.NewController(suite.T())
	suite.mockUsers = mockidentity.NewMockUserDatastore(suite.mockCtrl)
	suite.mockTokens = mockports.NewMockTokenService(suite.mockCtrl)
	suite.mockNotifier = mockports.NewMockNotifier(suite.mockCtrl)

	svc := identityapp.NewService(suite.mockUsers, suite.mockTokens, suite.mockNotifier)

	app := fiber.New()
	for _, route := range NewRouter(baseURI, svc).Routes() {
		app.Add(route.Method(), route.Path(), route.Handler())
	}
	suite.app = app
}

func (suite *IdentityHandlersTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

func (suite *IdentityHandlersTestSuite) do(method, target, body string) (*fiber.Ctx, int, map[string]any) {
	suite.T().Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}

	resp, err := suite.app.Test(req)
	suite.Require().NoError(err)

	payload := map[string]any{}
	raw, readErr := io.ReadAll(resp.Body)
	suite.Require().NoError(readErr)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	return nil, resp.StatusCode, payload
}

// registeredUser builds a user whose stored hash really is the hash of the given password, so
// login exercises the genuine bcrypt comparison rather than a stub.
func (suite *IdentityHandlersTestSuite) registeredUser(password string) *identity.User {
	hash, hashErr := utils.HashPassword(password)
	suite.Require().NoError(hashErr)

	user, err := identity.Register(identity.RegisterUserParams{
		Username:     "johndoe",
		FirstName:    "John",
		LastName:     "Doe",
		Email:        "john.doe@curtz.com",
		PasswordHash: hash,
	})
	suite.Require().NoError(err)
	return user
}

// --- register ---------------------------------------------------------------------------------

func (suite *IdentityHandlersTestSuite) TestRegister_Returns201WithUserBody() {
	suite.mockUsers.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, user identity.User) (identity.User, error) { return user, nil }).
		Times(1)
	suite.mockNotifier.EXPECT().SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

	_, status, body := suite.do("POST", baseURI+"/auth/register",
		`{"username":"johndoe","first_name":"John","last_name":"Doe","email":"john.doe@curtz.com","password":"s3cret-password"}`)

	suite.Equal(fiber.StatusCreated, status)
	suite.Equal("johndoe", body["username"])
	suite.Equal("john.doe@curtz.com", body["email"])
	suite.Equal(string(identity.UserStatusInactive), body["status"])
	suite.Equal(false, body["verified"])
	suite.NotEmpty(body["id"])
}

// Neither the password hash nor the verification token may ever appear in a response.
func (suite *IdentityHandlersTestSuite) TestRegister_ResponseLeaksNoSecrets() {
	suite.mockUsers.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, user identity.User) (identity.User, error) { return user, nil }).
		Times(1)
	suite.mockNotifier.EXPECT().SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

	_, _, body := suite.do("POST", baseURI+"/auth/register",
		`{"username":"johndoe","first_name":"John","email":"john.doe@curtz.com","password":"s3cret-password"}`)

	for _, forbidden := range []string{"password", "password_hash", "passwordHash", "verification_token", "token"} {
		suite.NotContains(body, forbidden, "response must not expose %q", forbidden)
	}
}

func (suite *IdentityHandlersTestSuite) TestRegister_Returns422ForMissingFields() {
	suite.mockUsers.EXPECT().Save(gomock.Any(), gomock.Any()).Times(0)

	cases := map[string]string{
		"missing username": `{"first_name":"John","email":"john.doe@curtz.com","password":"pw"}`,
		"missing email":    `{"username":"johndoe","first_name":"John","password":"pw"}`,
		"missing password": `{"username":"johndoe","first_name":"John","email":"john.doe@curtz.com"}`,
		"empty body":       `{}`,
	}

	for name, payload := range cases {
		suite.Run(name, func() {
			_, status, _ := suite.do("POST", baseURI+"/auth/register", payload)
			suite.Equal(fiber.StatusUnprocessableEntity, status)
		})
	}
}

// A missing first name is rejected by the domain, not the handler, so it surfaces as a 400.
func (suite *IdentityHandlersTestSuite) TestRegister_Returns400ForMissingFirstName() {
	suite.mockUsers.EXPECT().Save(gomock.Any(), gomock.Any()).Times(0)

	_, status, _ := suite.do("POST", baseURI+"/auth/register",
		`{"username":"johndoe","email":"john.doe@curtz.com","password":"s3cret-password"}`)
	suite.Equal(fiber.StatusBadRequest, status)
}

func (suite *IdentityHandlersTestSuite) TestRegister_Returns422ForMalformedJSON() {
	suite.mockUsers.EXPECT().Save(gomock.Any(), gomock.Any()).Times(0)

	_, status, _ := suite.do("POST", baseURI+"/auth/register", `{"username":`)
	suite.Equal(fiber.StatusUnprocessableEntity, status)
}

func (suite *IdentityHandlersTestSuite) TestRegister_Returns409ForDuplicateAccount() {
	suite.mockUsers.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		Return(identity.User{}, errdefs.Conflict(errors.New("duplicate key value violates unique constraint"))).
		Times(1)

	_, status, body := suite.do("POST", baseURI+"/auth/register",
		`{"username":"johndoe","first_name":"John","email":"john.doe@curtz.com","password":"s3cret-password"}`)

	suite.Equal(fiber.StatusConflict, status)
	// the raw constraint text must not reach the client
	suite.NotContains(body["message"], "constraint")
}

func (suite *IdentityHandlersTestSuite) TestRegister_Returns400ForInvalidEmail() {
	suite.mockUsers.EXPECT().Save(gomock.Any(), gomock.Any()).Times(0)

	_, status, body := suite.do("POST", baseURI+"/auth/register",
		`{"username":"johndoe","first_name":"John","email":"not-an-email","password":"s3cret-password"}`)

	suite.Equal(fiber.StatusBadRequest, status, "a bad email is caller error, not a server fault")
	suite.Contains(body["message"], "invalid")
}

// --- login ------------------------------------------------------------------------------------

func (suite *IdentityHandlersTestSuite) TestLogin_Returns200WithTokens() {
	user := suite.registeredUser("s3cret-password")

	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), "john.doe@curtz.com").Return(*user, nil).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Return("access-token", nil).Times(1)
	suite.mockTokens.EXPECT().GenerateRefreshToken(gomock.Any()).Return("refresh-token", nil).Times(1)

	_, status, body := suite.do("POST", baseURI+"/auth/login",
		`{"email":"john.doe@curtz.com","password":"s3cret-password"}`)

	suite.Require().Equal(fiber.StatusOK, status)
	suite.Equal("access-token", body["access_token"])
	suite.Equal("refresh-token", body["refresh_token"])
	suite.Equal("john.doe@curtz.com", body["email"])
	suite.NotContains(body, "password_hash")
}

func (suite *IdentityHandlersTestSuite) TestLogin_Returns401ForWrongPassword() {
	user := suite.registeredUser("the-right-password")

	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

	_, status, body := suite.do("POST", baseURI+"/auth/login",
		`{"email":"john.doe@curtz.com","password":"the-wrong-password"}`)

	suite.Equal(fiber.StatusUnauthorized, status)
	suite.Contains(body["message"], "invalid email or password")
}

func (suite *IdentityHandlersTestSuite) TestLogin_Returns401ForUnknownEmail() {
	suite.mockUsers.EXPECT().
		FetchByEmail(gomock.Any(), gomock.Any()).
		Return(identity.User{}, errdefs.NotFound(errors.New("no rows"))).
		Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

	_, status, body := suite.do("POST", baseURI+"/auth/login",
		`{"email":"nobody@curtz.com","password":"whatever"}`)

	suite.Equal(fiber.StatusUnauthorized, status)
	suite.Contains(body["message"], "invalid email or password",
		"the message must not reveal whether the account exists")
}

func (suite *IdentityHandlersTestSuite) TestLogin_Returns422ForMissingCredentials() {
	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Times(0)

	for name, payload := range map[string]string{
		"no password": `{"email":"john.doe@curtz.com"}`,
		"no email":    `{"password":"s3cret"}`,
		"empty":       `{}`,
	} {
		suite.Run(name, func() {
			_, status, _ := suite.do("POST", baseURI+"/auth/login", payload)
			suite.Equal(fiber.StatusUnprocessableEntity, status)
		})
	}
}

// --- oauth/token ------------------------------------------------------------------------------

func (suite *IdentityHandlersTestSuite) TestOAuthToken_Returns200WithNewPair() {
	user := suite.registeredUser("s3cret-password")

	suite.mockTokens.EXPECT().Authenticate("a-refresh-token").Return("user-id", nil).Times(1)
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), "user-id").Return(*user, nil).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken("user-id").Return("new-access", nil).Times(1)
	suite.mockTokens.EXPECT().GenerateRefreshToken("user-id").Return("new-refresh", nil).Times(1)

	_, status, body := suite.do("POST",
		baseURI+"/auth/oauth/token?grant_type=refresh_token&refresh_token=a-refresh-token", "")

	suite.Equal(fiber.StatusOK, status)
	suite.Equal("new-access", body["access_token"])
	suite.Equal("new-refresh", body["refresh_token"])
	suite.Equal("Bearer", body["token_type"])
}

func (suite *IdentityHandlersTestSuite) TestOAuthToken_Returns401ForUnsupportedGrantType() {
	suite.mockTokens.EXPECT().Authenticate(gomock.Any()).Times(0)

	for name, query := range map[string]string{
		"password grant": "?grant_type=password&refresh_token=tok",
		"absent grant":   "?refresh_token=tok",
	} {
		suite.Run(name, func() {
			_, status, _ := suite.do("POST", baseURI+"/auth/oauth/token"+query, "")
			suite.Equal(fiber.StatusUnauthorized, status)
		})
	}
}

func (suite *IdentityHandlersTestSuite) TestOAuthToken_Returns401ForMissingRefreshToken() {
	suite.mockTokens.EXPECT().Authenticate(gomock.Any()).Times(0)

	_, status, _ := suite.do("POST", baseURI+"/auth/oauth/token?grant_type=refresh_token", "")
	suite.Equal(fiber.StatusUnauthorized, status)
}

// --- verify -----------------------------------------------------------------------------------

func (suite *IdentityHandlersTestSuite) TestVerify_Returns200ForValidToken() {
	user := suite.registeredUser("s3cret-password")
	verification := user.Verification()
	token := verification.Token()

	verified, err := identity.NewUser(identity.UserParams{
		Username:            user.Username(),
		FirstName:           user.FirstName(),
		Email:               "john.doe@curtz.com",
		Status:              identity.UserStatusActive,
		VerificationToken:   token,
		VerificationExpires: verification.Expires(),
		Verified:            true,
	})
	suite.Require().NoError(err)

	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), token).Return(*user, nil).Times(1)
	suite.mockUsers.EXPECT().MarkVerified(gomock.Any(), gomock.Any()).Return(*verified, nil).Times(1)

	_, status, body := suite.do("GET", baseURI+"/auth/verify?v="+token, "")

	suite.Equal(fiber.StatusOK, status)
	suite.Equal(true, body["verified"])
	suite.Equal(string(identity.UserStatusActive), body["status"])
}

func (suite *IdentityHandlersTestSuite) TestVerify_Returns400ForMissingToken() {
	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), gomock.Any()).Times(0)

	_, status, _ := suite.do("GET", baseURI+"/auth/verify", "")
	suite.Equal(fiber.StatusBadRequest, status)
}

func (suite *IdentityHandlersTestSuite) TestVerify_Returns400ForUnknownToken() {
	suite.mockUsers.EXPECT().
		FetchByVerificationToken(gomock.Any(), "no-such-token").
		Return(identity.User{}, errdefs.NotFound(errors.New("no rows"))).
		Times(1)
	suite.mockUsers.EXPECT().MarkVerified(gomock.Any(), gomock.Any()).Times(0)

	_, status, _ := suite.do("GET", baseURI+"/auth/verify?v=no-such-token", "")
	suite.Equal(fiber.StatusBadRequest, status, "an unknown token must not 404, or it reveals which tokens exist")
}

func TestNewRouter_RegistersAllIdentityRoutes(t *testing.T) {
	routes := NewRouter(baseURI, nil).Routes()

	got := map[string]string{}
	for _, route := range routes {
		got[route.Path()] = route.Method()
	}

	require.Len(t, routes, 4)
	assert.Equal(t, "POST", got[baseURI+"/auth/register"])
	assert.Equal(t, "POST", got[baseURI+"/auth/login"])
	assert.Equal(t, "POST", got[baseURI+"/auth/oauth/token"])
	assert.Equal(t, "GET", got[baseURI+"/auth/verify"])
}

var _ = router.Router(nil)
