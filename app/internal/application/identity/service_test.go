package identityapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/fupi/app/internal/domain/identity/mocks"
	mockports "github.com/sanctumlabs/fupi/app/internal/ports/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

type IdentityServiceTestSuite struct {
	suite.Suite
	mockCtrl     *gomock.Controller
	mockUsers    *mockidentity.MockUserDatastore
	mockTokens   *mockports.MockTokenService
	mockNotifier *mockports.MockNotifier
	service      *Service
}

func TestIdentityServiceTestSuite(t *testing.T) {
	suite.Run(t, new(IdentityServiceTestSuite))
}

func (suite *IdentityServiceTestSuite) SetupTest() {
	suite.mockCtrl = gomock.NewController(suite.T())
	suite.mockUsers = mockidentity.NewMockUserDatastore(suite.mockCtrl)
	suite.mockTokens = mockports.NewMockTokenService(suite.mockCtrl)
	suite.mockNotifier = mockports.NewMockNotifier(suite.mockCtrl)
	suite.service = NewService(suite.mockUsers, suite.mockTokens, suite.mockNotifier)
}

func (suite *IdentityServiceTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

func validCommand() RegisterCommand {
	return RegisterCommand{
		Username:  "johndoe",
		FirstName: "John",
		LastName:  "Doe",
		Email:     "john.doe@fupi.com",
		Password:  "s3cret-password",
	}
}

// registeredUser builds a persisted-looking user with a known password and verification token.
func (suite *IdentityServiceTestSuite) registeredUser(password string) *identity.User {
	hash, err := utils.HashPassword(password)
	suite.Require().NoError(err)

	user, err := identity.Register(identity.RegisterUserParams{
		Username:     "johndoe",
		FirstName:    "John",
		LastName:     "Doe",
		Email:        "john.doe@fupi.com",
		PasswordHash: hash,
	})
	suite.Require().NoError(err)
	return user
}

// --- Register ---------------------------------------------------------------------------------

func (suite *IdentityServiceTestSuite) TestRegister_PersistsUserAndSendsVerificationEmail() {
	cmd := validCommand()

	var saved identity.User
	suite.mockUsers.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, user identity.User) (identity.User, error) {
			saved = user
			return user, nil
		}).
		Times(1)

	var sentToken string
	suite.mockNotifier.EXPECT().
		SendEmailVerification(gomock.Any(), cmd.Email, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, token string) error {
			sentToken = token
			return nil
		}).
		Times(1)

	actual, err := suite.service.Register(context.Background(), cmd)
	suite.Require().NoError(err)

	suite.Equal(cmd.Username, actual.Username())
	suite.Equal(identity.UserStatusInactive, actual.Status(), "a new user must start INACTIVE")

	// The plaintext password must never reach the domain or the datastore.
	suite.NotEqual(cmd.Password, saved.PasswordHash())
	ok, compareErr := utils.CompareHashAndPassword(saved.PasswordHash(), cmd.Password)
	suite.Require().NoError(compareErr)
	suite.True(ok, "the stored hash must verify against the original password")

	// The emailed token must be the one persisted, or the link can never be redeemed.
	verification := saved.Verification()
	suite.Equal(verification.Token(), sentToken)
	suite.NotEmpty(sentToken)
}

func (suite *IdentityServiceTestSuite) TestRegister_RecordsUserRegisteredEvent() {
	suite.mockUsers.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, user identity.User) (identity.User, error) { return user, nil }).
		Times(1)
	suite.mockNotifier.EXPECT().SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

	actual, err := suite.service.Register(context.Background(), validCommand())
	suite.Require().NoError(err)

	events := actual.DomainEvents()
	suite.Require().Len(events, 1)
	suite.Equal("user.registered", events[0].EventType())
}

// A committed user must not be reported as a failed registration just because the email bounced.
func (suite *IdentityServiceTestSuite) TestRegister_SucceedsWhenVerificationEmailFails() {
	suite.mockUsers.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, user identity.User) (identity.User, error) { return user, nil }).
		Times(1)
	suite.mockNotifier.EXPECT().
		SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("smtp unavailable")).
		Times(1)

	actual, err := suite.service.Register(context.Background(), validCommand())
	suite.NoError(err, "registration must succeed even when the verification email fails")
	suite.NotEmpty(actual.Username())
}

func (suite *IdentityServiceTestSuite) TestRegister_DoesNotNotifyWhenSaveFails() {
	suite.mockUsers.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		Return(identity.User{}, errdefs.Conflict(errors.New("duplicate email"))).
		Times(1)
	suite.mockNotifier.EXPECT().SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	_, err := suite.service.Register(context.Background(), validCommand())
	suite.Require().Error(err)
	suite.True(errdefs.IsConflict(err), "a duplicate registration should surface as a conflict, got %v", err)
}

func (suite *IdentityServiceTestSuite) TestRegister_RejectsInvalidInputBeforeTouchingPorts() {
	suite.mockUsers.EXPECT().Save(gomock.Any(), gomock.Any()).Times(0)
	suite.mockNotifier.EXPECT().SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	cases := map[string]func(*RegisterCommand){
		"invalid email":    func(c *RegisterCommand) { c.Email = "not-an-email" },
		"empty first name": func(c *RegisterCommand) { c.FirstName = "" },
		"empty password":   func(c *RegisterCommand) { c.Password = "" },
	}

	for name, mutate := range cases {
		suite.Run(name, func() {
			cmd := validCommand()
			mutate(&cmd)
			_, err := suite.service.Register(context.Background(), cmd)
			suite.Require().Error(err)
			suite.True(errdefs.IsInvalidParameter(err),
				"bad input must be classified so the API can answer 400, got %v", err)
		})
	}
}

// --- VerifyEmail ------------------------------------------------------------------------------

func (suite *IdentityServiceTestSuite) TestVerifyEmail_ActivatesUser() {
	user := suite.registeredUser("s3cret-password")
	verification := user.Verification()
	token := verification.Token()
	userID := entity.IDToString(user.ID())

	// what the datastore hands back once the row is verified and activated
	persisted, err := identity.NewUser(identity.UserParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{EntityIDParams: entity.EntityIDParams{ID: user.ID()}},
		},
		Username:            user.Username(),
		FirstName:           user.FirstName(),
		LastName:            user.LastName(),
		Email:               "john.doe@fupi.com",
		Status:              identity.UserStatusActive,
		VerificationToken:   token,
		VerificationExpires: verification.Expires(),
		Verified:            true,
	})
	suite.Require().NoError(err)

	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), token).Return(*user, nil).Times(1)
	// the service must hand over the aggregate Verify transitioned: ACTIVE, verified, and carrying
	// the UserVerified event for the outbox
	suite.mockUsers.EXPECT().
		MarkVerified(gomock.Any(), gomock.Cond(func(verified identity.User) bool {
			events := verified.DomainEvents()
			return entity.IDToString(verified.ID()) == userID &&
				verified.Status() == identity.UserStatusActive &&
				len(events) > 0 && events[len(events)-1].EventType() == "user.verified"
		})).
		Return(*persisted, nil).
		Times(1)

	actual, verifyErr := suite.service.VerifyEmail(context.Background(), token)
	suite.Require().NoError(verifyErr)
	suite.Equal(identity.UserStatusActive, actual.Status())
	suite.True(actual.IsActive(), "a verified user should be active")
}

func (suite *IdentityServiceTestSuite) TestVerifyEmail_RejectsEmptyTokenWithoutQuerying() {
	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), gomock.Any()).Times(0)

	_, err := suite.service.VerifyEmail(context.Background(), "")
	suite.Require().Error(err)
	suite.True(errdefs.IsInvalidParameter(err), "expected InvalidParameter, got %v", err)
}

// An unknown token must look identical to an invalid one, so the response cannot be used to
// probe which tokens exist.
func (suite *IdentityServiceTestSuite) TestVerifyEmail_UnknownTokenLooksInvalidNotMissing() {
	suite.mockUsers.EXPECT().
		FetchByVerificationToken(gomock.Any(), "no-such-token").
		Return(identity.User{}, errdefs.NotFound(errors.New("no rows"))).
		Times(1)
	suite.mockUsers.EXPECT().MarkVerified(gomock.Any(), gomock.Any()).Times(0)

	_, err := suite.service.VerifyEmail(context.Background(), "no-such-token")
	suite.Require().Error(err)
	suite.True(errdefs.IsInvalidParameter(err), "expected InvalidParameter, got %v", err)
	suite.False(errdefs.IsNotFound(err), "must not leak that the token was simply not found")
}

func (suite *IdentityServiceTestSuite) TestVerifyEmail_RejectsExpiredToken() {
	user, err := identity.NewUser(identity.UserParams{
		Username:            "johndoe",
		FirstName:           "John",
		Email:               "john.doe@fupi.com",
		Status:              identity.UserStatusInactive,
		VerificationToken:   "expired-token",
		VerificationExpires: time.Now().Add(-time.Hour),
	})
	suite.Require().NoError(err)

	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), "expired-token").Return(*user, nil).Times(1)
	suite.mockUsers.EXPECT().MarkVerified(gomock.Any(), gomock.Any()).Times(0)

	_, verifyErr := suite.service.VerifyEmail(context.Background(), "expired-token")
	suite.Require().Error(verifyErr)
	suite.True(errdefs.IsInvalidParameter(verifyErr), "expected InvalidParameter, got %v", verifyErr)
	suite.ErrorIs(verifyErr, errdefs.ErrVerificationTokenExpired)
}

func (suite *IdentityServiceTestSuite) TestVerifyEmail_RejectsAlreadyVerifiedUser() {
	user, err := identity.NewUser(identity.UserParams{
		Username:            "johndoe",
		FirstName:           "John",
		Email:               "john.doe@fupi.com",
		Status:              identity.UserStatusActive,
		VerificationToken:   "used-token",
		VerificationExpires: time.Now().Add(time.Hour),
		Verified:            true,
	})
	suite.Require().NoError(err)

	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), "used-token").Return(*user, nil).Times(1)
	suite.mockUsers.EXPECT().MarkVerified(gomock.Any(), gomock.Any()).Times(0)

	_, verifyErr := suite.service.VerifyEmail(context.Background(), "used-token")
	suite.Require().Error(verifyErr)
	suite.ErrorIs(verifyErr, errdefs.ErrUserAlreadyVerified)
}

// --- Login ------------------------------------------------------------------------------------

func (suite *IdentityServiceTestSuite) TestLogin_IssuesTokenPair() {
	const password = "s3cret-password"
	user := suite.registeredUser(password)
	userID := entity.IDToString(user.ID())

	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), "john.doe@fupi.com").Return(*user, nil).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(userID).Return("access-token", nil).Times(1)
	suite.mockTokens.EXPECT().GenerateRefreshToken(userID).Return("refresh-token", nil).Times(1)

	actual, tokens, err := suite.service.Login(context.Background(), "john.doe@fupi.com", password)
	suite.Require().NoError(err)
	suite.Equal(user.ID(), actual.ID())
	suite.Equal("access-token", tokens.AccessToken)
	suite.Equal("refresh-token", tokens.RefreshToken)
}

// An unverified user could still sign in before v2; that behaviour is preserved deliberately.
func (suite *IdentityServiceTestSuite) TestLogin_AllowsUnverifiedUser() {
	const password = "s3cret-password"
	user := suite.registeredUser(password)
	suite.Require().Equal(identity.UserStatusInactive, user.Status())

	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Return("access-token", nil).Times(1)
	suite.mockTokens.EXPECT().GenerateRefreshToken(gomock.Any()).Return("refresh-token", nil).Times(1)

	_, _, err := suite.service.Login(context.Background(), "john.doe@fupi.com", password)
	suite.NoError(err)
}

func (suite *IdentityServiceTestSuite) TestLogin_RejectsWrongPassword() {
	user := suite.registeredUser("the-right-password")

	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

	_, _, err := suite.service.Login(context.Background(), "john.doe@fupi.com", "the-wrong-password")
	suite.Require().Error(err)
	suite.True(errdefs.IsUnauthorized(err), "expected Unauthorized, got %v", err)
	suite.ErrorIs(err, errdefs.ErrInvalidCredentials)
}

// An unknown email and a wrong password must be indistinguishable to the caller.
func (suite *IdentityServiceTestSuite) TestLogin_UnknownEmailIsIndistinguishableFromWrongPassword() {
	suite.mockUsers.EXPECT().
		FetchByEmail(gomock.Any(), gomock.Any()).
		Return(identity.User{}, errdefs.NotFound(errors.New("no rows"))).
		Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

	_, _, err := suite.service.Login(context.Background(), "nobody@fupi.com", "any-password")
	suite.Require().Error(err)
	suite.True(errdefs.IsUnauthorized(err), "expected Unauthorized, got %v", err)
	suite.ErrorIs(err, errdefs.ErrInvalidCredentials)
	suite.False(errdefs.IsNotFound(err), "must not leak that the account does not exist")
}

// A failing datastore is an outage, not bad credentials, and must not be reported as one.
func (suite *IdentityServiceTestSuite) TestLogin_PropagatesDatastoreFailure() {
	datastoreErr := errors.New("connection refused")
	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(identity.User{}, datastoreErr).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

	_, _, err := suite.service.Login(context.Background(), "john.doe@fupi.com", "any-password")
	suite.Require().ErrorIs(err, datastoreErr)
	suite.False(errdefs.IsUnauthorized(err), "an outage must not look like bad credentials")
}

func (suite *IdentityServiceTestSuite) TestLogin_RejectsSuspendedAndDeletedAccounts() {
	const password = "s3cret-password"

	for _, status := range []identity.UserStatus{identity.UserStatusSuspended, identity.UserStatusDeleted} {
		suite.Run(string(status), func() {
			hash, err := utils.HashPassword(password)
			suite.Require().NoError(err)

			user, err := identity.NewUser(identity.UserParams{
				Username:     "johndoe",
				FirstName:    "John",
				Email:        "john.doe@fupi.com",
				PasswordHash: hash,
				Status:       status,
			})
			suite.Require().NoError(err)

			suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil).Times(1)
			suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

			_, _, loginErr := suite.service.Login(context.Background(), "john.doe@fupi.com", password)
			suite.Require().Error(loginErr)
			suite.True(errdefs.IsForbidden(loginErr), "expected Forbidden, got %v", loginErr)
		})
	}
}

// --- Refresh ----------------------------------------------------------------------------------

func (suite *IdentityServiceTestSuite) TestRefresh_IssuesFreshTokenPair() {
	user := suite.registeredUser("s3cret-password")
	userID := entity.IDToString(user.ID())

	suite.mockTokens.EXPECT().Authenticate("a-refresh-token").Return(userID, nil).Times(1)
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), userID).Return(*user, nil).Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(userID).Return("new-access", nil).Times(1)
	suite.mockTokens.EXPECT().GenerateRefreshToken(userID).Return("new-refresh", nil).Times(1)

	tokens, err := suite.service.Refresh(context.Background(), "a-refresh-token")
	suite.Require().NoError(err)
	suite.Equal("new-access", tokens.AccessToken)
	suite.Equal("new-refresh", tokens.RefreshToken)
}

func (suite *IdentityServiceTestSuite) TestRefresh_RejectsEmptyToken() {
	suite.mockTokens.EXPECT().Authenticate(gomock.Any()).Times(0)

	_, err := suite.service.Refresh(context.Background(), "")
	suite.Require().Error(err)
	suite.True(errdefs.IsUnauthorized(err), "expected Unauthorized, got %v", err)
}

func (suite *IdentityServiceTestSuite) TestRefresh_RejectsInvalidToken() {
	suite.mockTokens.EXPECT().
		Authenticate("bad-token").
		Return("", errdefs.Unauthorized(errors.New("bad signature"))).
		Times(1)
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), gomock.Any()).Times(0)

	_, err := suite.service.Refresh(context.Background(), "bad-token")
	suite.Require().Error(err)
	suite.True(errdefs.IsUnauthorized(err), "expected Unauthorized, got %v", err)
}

// A token for an account that has since been removed must stop working.
func (suite *IdentityServiceTestSuite) TestRefresh_RejectsTokenForDeletedUser() {
	userID := entity.IDToString(entity.NewID())

	suite.mockTokens.EXPECT().Authenticate("orphan-token").Return(userID, nil).Times(1)
	suite.mockUsers.EXPECT().
		FetchById(gomock.Any(), userID).
		Return(identity.User{}, errdefs.NotFound(errors.New("no rows"))).
		Times(1)
	suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

	_, err := suite.service.Refresh(context.Background(), "orphan-token")
	suite.Require().Error(err)
	suite.True(errdefs.IsUnauthorized(err), "expected Unauthorized, got %v", err)
}

// Refresh applies the same sign-in policy as Login, so a suspended or deleted account cannot
// keep minting tokens.
func (suite *IdentityServiceTestSuite) TestRefresh_RejectsSuspendedAndDeletedAccounts() {
	for _, status := range []identity.UserStatus{identity.UserStatusSuspended, identity.UserStatusDeleted} {
		suite.Run(string(status), func() {
			user, err := identity.NewUser(identity.UserParams{
				Username:  "johndoe",
				FirstName: "John",
				Email:     "john.doe@fupi.com",
				Status:    status,
			})
			suite.Require().NoError(err)
			userID := entity.IDToString(user.ID())

			suite.mockTokens.EXPECT().Authenticate("a-refresh-token").Return(userID, nil).Times(1)
			suite.mockUsers.EXPECT().FetchById(gomock.Any(), userID).Return(*user, nil).Times(1)
			suite.mockTokens.EXPECT().GenerateAccessToken(gomock.Any()).Times(0)

			_, refreshErr := suite.service.Refresh(context.Background(), "a-refresh-token")
			suite.Require().Error(refreshErr)
			suite.True(errdefs.IsForbidden(refreshErr), "expected Forbidden, got %v", refreshErr)
		})
	}
}

// A failing datastore is an outage, not a bad token, and must not be reported as one.
func (suite *IdentityServiceTestSuite) TestRefresh_PropagatesDatastoreFailure() {
	userID := entity.IDToString(entity.NewID())
	datastoreErr := errors.New("connection refused")

	suite.mockTokens.EXPECT().Authenticate("a-refresh-token").Return(userID, nil).Times(1)
	suite.mockUsers.EXPECT().FetchById(gomock.Any(), userID).Return(identity.User{}, datastoreErr).Times(1)

	_, err := suite.service.Refresh(context.Background(), "a-refresh-token")
	suite.Require().ErrorIs(err, datastoreErr)
	suite.False(errdefs.IsUnauthorized(err), "an outage must not look like a bad token")
}

func TestNewService_WiresPorts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := NewService(
		mockidentity.NewMockUserDatastore(ctrl),
		mockports.NewMockTokenService(ctrl),
		mockports.NewMockNotifier(ctrl),
	)

	require.NotNil(t, svc)
	assert.NotNil(t, svc.users)
	assert.NotNil(t, svc.tokens)
	assert.NotNil(t, svc.notifier)
}
