//go:build e2e

// Package e2e drives the real Fiber app against a real PostgreSQL instance, exercising the
// Identity slice end to end: HTTP -> application -> domain -> database.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/gofiber/fiber/v2"
	identityapi "github.com/sanctumlabs/curtz/app/api/v1/identity"
	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/internal/adapters/jwtauth"
	identitydatastore "github.com/sanctumlabs/curtz/app/internal/adapters/postgres/identity"
	identityapp "github.com/sanctumlabs/curtz/app/internal/application/identity"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/router"
	"github.com/sanctumlabs/curtz/app/pkg/jwt"
	recoveryutils "github.com/sanctumlabs/curtz/app/pkg/utils/recover"
	"github.com/sanctumlabs/curtz/app/test"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const baseURI = "/api/v1/curtz"

// capturingNotifier stands in for the email transport so the test can read the verification link
// the user would have received.
type capturingNotifier struct {
	lastRecipient string
	lastToken     string
	calls         int
	err           error
}

func (n *capturingNotifier) SendEmailVerification(_ context.Context, recipient, token string) error {
	n.calls++
	n.lastRecipient, n.lastToken = recipient, token
	return n.err
}

type IdentityE2ETestSuite struct {
	suite.Suite
	ctx      context.Context
	dbClient database.PostgresDatabaseClient
	app      *fiber.App
	notifier *capturingNotifier
}

func TestIdentityE2ETestSuite(t *testing.T) {
	suite.Run(t, new(IdentityE2ETestSuite))
}

func (suite *IdentityE2ETestSuite) SetupSuite() {
	suite.ctx = context.Background()

	dbClient, err := test.TestPostgresDatabaseClient(suite.ctx)
	suite.Require().NoError(err, "failed to start the test database")
	suite.dbClient = dbClient

	dbConfig := database.Config{
		OperationTimeout: 30 * time.Second,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}

	authConfig := config.AuthConfig{
		Jwt: config.Jwt{
			Secret:             "e2e-test-secret",
			Issuer:             "curtz-e2e",
			ExpireDelta:        15,
			RefreshExpireDelta: 24,
		},
	}

	suite.notifier = &capturingNotifier{}

	svc := identityapp.NewService(
		identitydatastore.NewUserDatastoreAdapter(dbClient, dbConfig),
		jwtauth.NewTokenService(authConfig, jwt.New()),
		suite.notifier,
	)

	app := fiber.New()
	for _, route := range identityapi.NewRouter(baseURI, svc).Routes() {
		app.Add(route.Method(), route.Path(), route.Handler())
	}
	suite.app = app

	var _ router.Router = identityapi.NewRouter(baseURI, svc)
}

func (suite *IdentityE2ETestSuite) TearDownSuite() {
	if suite.dbClient != nil {
		suite.dbClient.Close()
	}
}

// do issues a request against the real app and decodes the JSON response.
func (suite *IdentityE2ETestSuite) do(method, target, body string) (int, map[string]any) {
	suite.T().Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}

	// generous timeout: the first call pays for the container's connection warm-up
	resp, err := suite.app.Test(req, 30_000)
	suite.Require().NoError(err)

	payload := map[string]any{}
	raw, readErr := io.ReadAll(resp.Body)
	suite.Require().NoError(readErr)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	return resp.StatusCode, payload
}

type credentials struct {
	username string
	email    string
	password string
}

func newCredentials() credentials {
	return credentials{
		username: faker.Username(),
		email:    faker.Email(),
		password: "s3cret-password",
	}
}

func (suite *IdentityE2ETestSuite) register(creds credentials) map[string]any {
	suite.T().Helper()

	status, body := suite.do("POST", baseURI+"/auth/register", fmt.Sprintf(
		`{"username":%q,"first_name":"John","last_name":"Doe","email":%q,"password":%q}`,
		creds.username, creds.email, creds.password,
	))
	suite.Require().Equal(fiber.StatusCreated, status, "register failed: %v", body)
	return body
}

// TestFullRegistrationJourney walks the whole identity flow the way a real client would:
// register, follow the emailed verification link, log in, and refresh the token pair.
func (suite *IdentityE2ETestSuite) TestFullRegistrationJourney() {
	creds := newCredentials()

	// 1. register -> the user is persisted INACTIVE and unverified
	registered := suite.register(creds)
	suite.Equal(creds.username, registered["username"])
	suite.Equal("INACTIVE", registered["status"])
	suite.Equal(false, registered["verified"])
	userID := registered["id"].(string)
	suite.Require().NotEmpty(userID)

	// the verification email carries a token that was actually written to the database
	suite.Require().Equal(1, suite.notifier.calls)
	suite.Equal(creds.email, suite.notifier.lastRecipient)
	token := suite.notifier.lastToken
	suite.Require().NotEmpty(token)

	// 2. follow the link -> the user becomes verified and ACTIVE
	status, verified := suite.do("GET", baseURI+"/auth/verify?v="+token, "")
	suite.Require().Equal(fiber.StatusOK, status, "verify failed: %v", verified)
	suite.Equal(true, verified["verified"])
	suite.Equal("ACTIVE", verified["status"])
	suite.Equal(userID, verified["id"], "verification must act on the user the token belongs to")

	// 3. log in -> a token pair is issued for the same user
	status, loggedIn := suite.do("POST", baseURI+"/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, creds.email, creds.password))
	suite.Require().Equal(fiber.StatusOK, status, "login failed: %v", loggedIn)
	suite.Equal(userID, loggedIn["id"])
	accessToken := loggedIn["access_token"].(string)
	refreshToken := loggedIn["refresh_token"].(string)
	suite.NotEmpty(accessToken)
	suite.NotEmpty(refreshToken)

	// 4. refresh -> a fresh pair is issued
	status, refreshed := suite.do("POST",
		baseURI+"/auth/oauth/token?grant_type=refresh_token&refresh_token="+refreshToken, "")
	suite.Require().Equal(fiber.StatusOK, status, "refresh failed: %v", refreshed)
	suite.NotEmpty(refreshed["access_token"])
	suite.NotEmpty(refreshed["refresh_token"])
	suite.Equal("Bearer", refreshed["token_type"])
}

// The token must survive the round trip through PostgreSQL: this is what the pre-v2 flow got
// wrong, emailing a base64 value while storing the raw one, so no link ever verified.
func (suite *IdentityE2ETestSuite) TestVerificationTokenSurvivesPersistence() {
	creds := newCredentials()
	suite.register(creds)

	token := suite.notifier.lastToken
	suite.Require().NotEmpty(token)

	status, body := suite.do("GET", baseURI+"/auth/verify?v="+token, "")
	suite.Equal(fiber.StatusOK, status,
		"the emailed token must match what was stored, got: %v", body)
}

func (suite *IdentityE2ETestSuite) TestRegisterRejectsDuplicateEmail() {
	creds := newCredentials()
	suite.register(creds)

	status, body := suite.do("POST", baseURI+"/auth/register", fmt.Sprintf(
		`{"username":%q,"first_name":"Jane","email":%q,"password":"another-password"}`,
		faker.Username(), creds.email,
	))

	suite.Equal(fiber.StatusConflict, status, "a duplicate email must be rejected: %v", body)
}

func (suite *IdentityE2ETestSuite) TestRegisterRejectsDuplicateUsername() {
	creds := newCredentials()
	suite.register(creds)

	status, body := suite.do("POST", baseURI+"/auth/register", fmt.Sprintf(
		`{"username":%q,"first_name":"Jane","email":%q,"password":"another-password"}`,
		creds.username, faker.Email(),
	))

	suite.Equal(fiber.StatusConflict, status, "a duplicate username must be rejected: %v", body)
}

func (suite *IdentityE2ETestSuite) TestLoginRejectsWrongPassword() {
	creds := newCredentials()
	suite.register(creds)

	status, body := suite.do("POST", baseURI+"/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"definitely-not-the-password"}`, creds.email))

	suite.Equal(fiber.StatusUnauthorized, status)
	suite.Contains(body["message"], "invalid email or password")
}

func (suite *IdentityE2ETestSuite) TestLoginRejectsUnknownEmail() {
	status, body := suite.do("POST", baseURI+"/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"whatever"}`, faker.Email()))

	suite.Equal(fiber.StatusUnauthorized, status)
	suite.Contains(body["message"], "invalid email or password")
}

// An unverified user could sign in before v2; that is preserved deliberately.
func (suite *IdentityE2ETestSuite) TestLoginSucceedsBeforeVerification() {
	creds := newCredentials()
	suite.register(creds)

	status, body := suite.do("POST", baseURI+"/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, creds.email, creds.password))

	suite.Require().Equal(fiber.StatusOK, status, "login before verification should succeed: %v", body)
	suite.Equal("INACTIVE", body["status"])
	suite.Equal(false, body["verified"])
}

func (suite *IdentityE2ETestSuite) TestVerifyRejectsUnknownToken() {
	status, _ := suite.do("GET", baseURI+"/auth/verify?v=0123456789abcdef0123456789abcdef", "")
	suite.Equal(fiber.StatusBadRequest, status)
}

func (suite *IdentityE2ETestSuite) TestVerifyRejectsReusedToken() {
	creds := newCredentials()
	suite.register(creds)
	token := suite.notifier.lastToken

	status, _ := suite.do("GET", baseURI+"/auth/verify?v="+token, "")
	suite.Require().Equal(fiber.StatusOK, status)

	// a verification link is single-use
	status, body := suite.do("GET", baseURI+"/auth/verify?v="+token, "")
	suite.Equal(fiber.StatusBadRequest, status, "a token must not verify twice: %v", body)
}

func (suite *IdentityE2ETestSuite) TestRefreshRejectsGarbageToken() {
	status, _ := suite.do("POST",
		baseURI+"/auth/oauth/token?grant_type=refresh_token&refresh_token=not-a-jwt", "")
	suite.Equal(fiber.StatusUnauthorized, status)
}

// An access token must not be usable as a refresh token's replacement path, and neither may an
// unsupported grant type.
func (suite *IdentityE2ETestSuite) TestRefreshRejectsUnsupportedGrantType() {
	status, _ := suite.do("POST",
		baseURI+"/auth/oauth/token?grant_type=password&refresh_token=whatever", "")
	suite.Equal(fiber.StatusUnauthorized, status)
}

// The response body is the contract clients see: it must never carry the password hash or the
// verification token.
func (suite *IdentityE2ETestSuite) TestResponsesNeverLeakSecrets() {
	creds := newCredentials()
	registered := suite.register(creds)

	for _, forbidden := range []string{"password", "password_hash", "verification_token", "token"} {
		suite.NotContains(registered, forbidden)
	}

	status, loggedIn := suite.do("POST", baseURI+"/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, creds.email, creds.password))
	require.Equal(suite.T(), fiber.StatusOK, status)

	for _, forbidden := range []string{"password", "password_hash", "verification_token"} {
		suite.NotContains(loggedIn, forbidden)
	}
}
