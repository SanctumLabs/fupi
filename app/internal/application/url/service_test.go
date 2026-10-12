package urlapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/core/ports/repository"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	mockports "github.com/sanctumlabs/fupi/app/internal/ports/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

type UrlServiceTestSuite struct {
	suite.Suite
	mockCtrl      *gomock.Controller
	mockUrls      *urlmock.MockUrlDatastore
	mockGenerator *mockports.MockShortCodeGenerator
	service       *Service
	userID        string
}

func TestUrlServiceTestSuite(t *testing.T) {
	suite.Run(t, new(UrlServiceTestSuite))
}

func (suite *UrlServiceTestSuite) SetupTest() {
	suite.mockCtrl = gomock.NewController(suite.T())
	suite.mockUrls = urlmock.NewMockUrlDatastore(suite.mockCtrl)
	suite.mockGenerator = mockports.NewMockShortCodeGenerator(suite.mockCtrl)
	suite.service = NewService(suite.mockUrls, suite.mockGenerator)
	suite.userID = entity.IDToString(entity.NewID())
}

func (suite *UrlServiceTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

func (suite *UrlServiceTestSuite) shortCode(value string) url.ShortCode {
	code, err := url.NewShortCode(value)
	suite.Require().NoError(err)
	return code
}

// ownedUrl builds a persisted-looking url belonging to the suite's user.
func (suite *UrlServiceTestSuite) ownedUrl(options ...urlmock.MockUrlOption) url.URL {
	defaults := []urlmock.MockUrlOption{
		urlmock.WithId(entity.NewID()),
		urlmock.WithUserId(suite.userID),
		urlmock.WithExpiresOn(time.Now().Add(24 * time.Hour)),
	}
	u, err := urlmock.MockUrl(append(defaults, options...)...)
	suite.Require().NoError(err)
	return *u
}

func eventTypes(u url.URL) []string {
	types := []string{}
	for _, event := range u.DomainEvents() {
		types = append(types, event.EventType())
	}
	return types
}

// --- Shorten ----------------------------------------------------------------------------------

func (suite *UrlServiceTestSuite) TestShorten_SavesAnActiveUrlWithTheGeneratedCodeAndItsCreatedEvent() {
	expiresOn := time.Now().Add(time.Hour)
	suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(suite.shortCode("000abc"), nil)
	suite.mockUrls.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) {
			suite.Equal("000abc", u.ShortCode().Value())
			suite.Equal(suite.userID, entity.IDToString(u.UserId()))
			suite.Equal("https://example.com/a/long/path", u.OriginalURL().Value())
			suite.Equal("my-alias", u.CustomAlias().Value())
			suite.Equal(url.URLStatusActive, u.Status())
			suite.Len(u.Keywords(), 2)
			suite.Equal([]string{"url.created"}, eventTypes(u))
			return u, nil
		})

	saved, err := suite.service.Shorten(context.Background(), ShortenCommand{
		UserID:      suite.userID,
		OriginalURL: "https://example.com/a/long/path/",
		CustomAlias: "my-alias",
		ExpiresOn:   expiresOn,
		Keywords:    []string{"docs", "news"},
	})

	suite.Require().NoError(err)
	suite.Equal("000abc", saved.ShortCode().Value())
}

func (suite *UrlServiceTestSuite) TestShorten_RejectsInvalidInputWithoutSaving() {
	cases := map[string]ShortenCommand{
		"malformed url":   {UserID: suite.userID, OriginalURL: "not a url at all", ExpiresOn: time.Now().Add(time.Hour)},
		"past expiry":     {UserID: suite.userID, OriginalURL: "https://example.com/a/long/path", ExpiresOn: time.Now().Add(-time.Hour)},
		"missing expiry":  {UserID: suite.userID, OriginalURL: "https://example.com/a/long/path"},
		"invalid alias":   {UserID: suite.userID, OriginalURL: "https://example.com/a/long/path", ExpiresOn: time.Now().Add(time.Hour), CustomAlias: "no spaces!"},
		"code-like alias": {UserID: suite.userID, OriginalURL: "https://example.com/a/long/path", ExpiresOn: time.Now().Add(time.Hour), CustomAlias: "Abc1234"},
		"invalid keyword": {UserID: suite.userID, OriginalURL: "https://example.com/a/long/path", ExpiresOn: time.Now().Add(time.Hour), Keywords: []string{"x"}},
	}

	for name, cmd := range cases {
		suite.Run(name, func() {
			suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(suite.shortCode("000abc"), nil)

			_, err := suite.service.Shorten(context.Background(), cmd)

			suite.True(errdefs.IsInvalidParameter(err), "expected InvalidParameter, got %v", err)
		})
	}
}

func (suite *UrlServiceTestSuite) TestShorten_ReturnsGeneratorAndSaveFailures() {
	generatorErr := errors.New("sequence unavailable")
	suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(url.ShortCode{}, generatorErr)

	_, err := suite.service.Shorten(context.Background(), ShortenCommand{UserID: suite.userID})
	suite.ErrorIs(err, generatorErr)

	conflict := errdefs.Conflict(errdefs.ErrURLAlreadyExists)
	suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(suite.shortCode("000abd"), nil)
	suite.mockUrls.EXPECT().Save(gomock.Any(), gomock.Any()).Return(url.URL{}, conflict)

	_, err = suite.service.Shorten(context.Background(), ShortenCommand{
		UserID:      suite.userID,
		OriginalURL: "https://example.com/a/long/path",
		ExpiresOn:   time.Now().Add(time.Hour),
	})
	suite.True(errdefs.IsConflict(err))
	suite.ErrorIs(err, errdefs.ErrURLAlreadyExists)
}

// --- Resolve ----------------------------------------------------------------------------------

func (suite *UrlServiceTestSuite) TestResolve_ReturnsTheTargetOfAnActiveShortCode() {
	active := suite.ownedUrl(urlmock.WithOriginalUrl("https://example.com/target/page"))
	suite.mockUrls.EXPECT().FetchByShortCode(gomock.Any(), "000abc").Return(active, nil)

	target, err := suite.service.Resolve(context.Background(), "000abc")

	suite.Require().NoError(err)
	suite.Equal("https://example.com/target/page", target.Value())
}

func (suite *UrlServiceTestSuite) TestResolve_FallsBackToTheCustomAlias() {
	active := suite.ownedUrl(urlmock.WithOriginalUrl("https://example.com/target/page"))
	suite.mockUrls.EXPECT().FetchByShortCode(gomock.Any(), "my-alias").Return(url.URL{}, errdefs.NotFound(errors.New("no rows")))
	suite.mockUrls.EXPECT().FetchByCustomAlias(gomock.Any(), "my-alias").Return(active, nil)

	target, err := suite.service.Resolve(context.Background(), "my-alias")

	suite.Require().NoError(err)
	suite.Equal("https://example.com/target/page", target.Value())
}

func (suite *UrlServiceTestSuite) TestResolve_UnknownCodeIsNotFound() {
	suite.mockUrls.EXPECT().FetchByShortCode(gomock.Any(), "nope").Return(url.URL{}, errdefs.NotFound(errors.New("no rows")))
	suite.mockUrls.EXPECT().FetchByCustomAlias(gomock.Any(), "nope").Return(url.URL{}, errdefs.NotFound(errors.New("no rows")))

	_, err := suite.service.Resolve(context.Background(), "nope")

	suite.True(errdefs.IsNotFound(err))
}

func (suite *UrlServiceTestSuite) TestResolve_UrlThatMayNotRedirectIsNotFound() {
	cases := map[string]url.URL{
		"expired":   suite.ownedUrl(urlmock.WithExpiresOn(time.Now().Add(-time.Hour))),
		"deleted":   suite.ownedUrl(urlmock.WithStatus(url.URLStatusDeleted)),
		"suspended": suite.ownedUrl(urlmock.WithStatus(url.URLStatusSuspended)),
	}

	for name, found := range cases {
		suite.Run(name, func() {
			suite.mockUrls.EXPECT().FetchByShortCode(gomock.Any(), "000abc").Return(found, nil)

			_, err := suite.service.Resolve(context.Background(), "000abc")

			suite.True(errdefs.IsNotFound(err), "expected NotFound, got %v", err)
		})
	}
}

func (suite *UrlServiceTestSuite) TestResolve_DatastoreFailureIsNotHidden() {
	dbErr := errors.New("connection reset")
	suite.mockUrls.EXPECT().FetchByShortCode(gomock.Any(), "000abc").Return(url.URL{}, dbErr)

	_, err := suite.service.Resolve(context.Background(), "000abc")

	suite.ErrorIs(err, dbErr)
	suite.False(errdefs.IsNotFound(err))
}

// --- Get / List -------------------------------------------------------------------------------

func (suite *UrlServiceTestSuite) TestGet_ReturnsTheCallersUrl() {
	owned := suite.ownedUrl()
	id := entity.IDToString(owned.ID())
	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(owned, nil)

	found, err := suite.service.Get(context.Background(), suite.userID, id)

	suite.Require().NoError(err)
	suite.Equal(owned.ID(), found.ID())
}

func (suite *UrlServiceTestSuite) TestGet_HidesUrlsTheCallerCannotSee() {
	someoneElses := suite.ownedUrl(urlmock.WithUserId(entity.IDToString(entity.NewID())))
	deleted := suite.ownedUrl(urlmock.WithStatus(url.URLStatusDeleted))

	for name, found := range map[string]url.URL{"another user's": someoneElses, "deleted": deleted} {
		suite.Run(name, func() {
			id := entity.IDToString(found.ID())
			suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(found, nil)

			_, err := suite.service.Get(context.Background(), suite.userID, id)

			suite.True(errdefs.IsNotFound(err), "expected NotFound, got %v", err)
		})
	}
}

func (suite *UrlServiceTestSuite) TestGet_MalformedIdIsNotFoundWithoutAQuery() {
	_, err := suite.service.Get(context.Background(), suite.userID, "not-a-uuid")

	suite.True(errdefs.IsNotFound(err))
}

func (suite *UrlServiceTestSuite) TestList_ReturnsTheCallersUrls() {
	page := repository.FetchRecordsResponse[url.URL]{Records: []url.URL{suite.ownedUrl()}, Total: 1, Page: 1, Size: 1}
	suite.mockUrls.EXPECT().FetchByUserId(gomock.Any(), suite.userID).Return(page, nil)

	actual, err := suite.service.List(context.Background(), suite.userID)

	suite.Require().NoError(err)
	suite.Equal(page, actual)
}

// --- Update -----------------------------------------------------------------------------------

func (suite *UrlServiceTestSuite) TestUpdate_AppliesExpiryAndKeywords() {
	owned := suite.ownedUrl(urlmock.WithKeywords([]string{"old"}))
	id := entity.IDToString(owned.ID())
	newExpiry := time.Now().Add(72 * time.Hour)
	newKeywords := []string{"docs", "news"}

	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(owned, nil)
	suite.mockUrls.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) {
			suite.True(newExpiry.Equal(u.ExpiresOn()))
			suite.Require().Len(u.Keywords(), 2)
			suite.Equal("docs", u.Keywords()[0].Value)
			return u, nil
		})

	_, err := suite.service.Update(context.Background(), UpdateCommand{UserID: suite.userID, ID: id, ExpiresOn: &newExpiry, Keywords: &newKeywords})

	suite.Require().NoError(err)
}

func (suite *UrlServiceTestSuite) TestUpdate_LeavesFieldsThatAreNotSent() {
	owned := suite.ownedUrl(urlmock.WithKeywords([]string{"keep"}))
	id := entity.IDToString(owned.ID())
	newExpiry := time.Now().Add(72 * time.Hour)

	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(owned, nil)
	suite.mockUrls.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) {
			suite.Require().Len(u.Keywords(), 1)
			suite.Equal("keep", u.Keywords()[0].Value)
			return u, nil
		})

	_, err := suite.service.Update(context.Background(), UpdateCommand{UserID: suite.userID, ID: id, ExpiresOn: &newExpiry})

	suite.Require().NoError(err)
}

func (suite *UrlServiceTestSuite) TestUpdate_RejectionsAreClassified() {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	tooMany := []string{"k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8", "k9", "k10", "k11"}

	cases := []struct {
		name  string
		found url.URL
		cmd   UpdateCommand
		check func(error) bool
	}{
		{"past expiry", suite.ownedUrl(), UpdateCommand{ExpiresOn: &past}, errdefs.IsInvalidParameter},
		{"too many keywords", suite.ownedUrl(), UpdateCommand{Keywords: &tooMany}, errdefs.IsInvalidParameter},
		{"expiry on an expired url", suite.ownedUrl(urlmock.WithStatus(url.URLStatusExpired)), UpdateCommand{ExpiresOn: &future}, errdefs.IsConflict},
	}

	for _, tc := range cases {
		suite.Run(tc.name, func() {
			id := entity.IDToString(tc.found.ID())
			tc.cmd.UserID, tc.cmd.ID = suite.userID, id
			suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(tc.found, nil)

			_, err := suite.service.Update(context.Background(), tc.cmd)

			suite.True(tc.check(err), "unexpected error class for %v", err)
		})
	}
}

// --- Delete -----------------------------------------------------------------------------------

func (suite *UrlServiceTestSuite) TestDelete_PersistsTheDeletedStatusAndEvent() {
	owned := suite.ownedUrl()
	id := entity.IDToString(owned.ID())

	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(owned, nil)
	suite.mockUrls.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) {
			suite.Equal(url.URLStatusDeleted, u.Status())
			suite.Equal([]string{"url.deleted"}, eventTypes(u))
			return u, nil
		})

	suite.Require().NoError(suite.service.Delete(context.Background(), suite.userID, id))
}

func (suite *UrlServiceTestSuite) TestDelete_AnotherUsersUrlIsNotFoundAndUntouched() {
	someoneElses := suite.ownedUrl(urlmock.WithUserId(entity.IDToString(entity.NewID())))
	id := entity.IDToString(someoneElses.ID())
	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(someoneElses, nil)

	err := suite.service.Delete(context.Background(), suite.userID, id)

	suite.True(errdefs.IsNotFound(err))
}
