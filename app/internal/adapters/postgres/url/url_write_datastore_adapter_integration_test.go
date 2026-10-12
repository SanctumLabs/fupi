//go:build integration

package urldatastore

import (
	"context"
	"testing"
	"time"

	identitydatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/identity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	mockidentity "github.com/sanctumlabs/fupi/app/internal/domain/identity/mocks"
	domainurl "github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/suite"
)

type UrlWriteDatastoreAdapterIntegrationTestSuite struct {
	suite.Suite
	urlWriteDatastoreAdapter   domainurl.UrlWriteDatastore
	userWriteDatastore         identity.UserWriteDatastore
	config                     database.Config
	testPostgresDatabaseClient database.PostgresDatabaseClient
}

func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) SetupTest() {
	ctx := context.Background()
	testPostgresDatabaseClient := test.TestPostgresDatabaseClientHelper(suite.T(), ctx)

	config := database.Config{
		OperationTimeout: 5 * time.Minute,
		RetryConfig:      recoveryutils.DefaultRetryConfig,
	}
	suite.testPostgresDatabaseClient = testPostgresDatabaseClient
	suite.userWriteDatastore = identitydatastore.NewUserWriteDatastoreAdapter(testPostgresDatabaseClient, config)
	suite.urlWriteDatastoreAdapter = NewUrlWriteDatastoreAdapter(testPostgresDatabaseClient, config)
	suite.config = config
}

func TestUrlWriteDatastoreAdapterIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(UrlWriteDatastoreAdapterIntegrationTestSuite))
}

func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) AfterTest(_, _ string) {
	suite.testPostgresDatabaseClient.Close()
}

// TestCreate_CreatesNewUrlSuccessfully tests the Create method of the UrlWriteDatastoreAdapter
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestCreate_CreatesNewUrlSuccessfully() {
	bcgCtx := context.Background()
	ctx, cancel := context.WithTimeout(bcgCtx, suite.config.OperationTimeout)
	defer cancel()

	mockUser, mockUserErr := mockidentity.MockUser()
	suite.Require().NoError(mockUserErr)

	// urls.user_id references users(id), so the owner must exist first
	_, saveUserErr := suite.userWriteDatastore.Save(ctx, *mockUser)
	suite.Require().NoError(saveUserErr)

	mockUrl, mockUrlErr := urlmock.MockUrl(
		urlmock.WithUserId(mockUser.ID().String()),
		urlmock.WithExpiresOn(time.Now().Add(time.Hour*24)),
		urlmock.WithCustomAlias("custom"),
		urlmock.WithShortCode("shortcode"),
	)

	// Require stops the test immediately on failure, preventing a nil
	// dereference on *mockUrl in the Create call below.
	suite.Require().NoError(mockUrlErr)

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.Require().NoError(actualErr)

	suite.Equal(mockUrl.UserId(), actual.UserId())
	suite.Equal(mockUrl.ShortCode(), actual.ShortCode())
	suite.Equal(mockUrl.CustomAlias(), actual.CustomAlias())
	suite.Equal(mockUrl.OriginalURL(), actual.OriginalURL())
	// Postgres stores microseconds and drops the monotonic clock reading
	suite.WithinDuration(mockUrl.ExpiresOn(), actual.ExpiresOn(), time.Microsecond)
	suite.Equal(mockUrl.Status(), actual.Status())
	suite.Equal(mockUrl.OgTitle(), actual.OgTitle())
	suite.Equal(mockUrl.OgDescription(), actual.OgDescription())
	suite.Equal(mockUrl.OgImageUrl(), actual.OgImageUrl())
}

// TestSave_FailsWhenUserDoesNotExist tests that the users foreign key rejects a URL whose owner
// was never saved
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestSave_FailsWhenUserDoesNotExist() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	mockUser, mockUserErr := mockidentity.MockUser()
	suite.Require().NoError(mockUserErr)

	mockUrl, mockUrlErr := urlmock.MockUrl(
		urlmock.WithUserId(mockUser.ID().String()),
		urlmock.WithExpiresOn(time.Now().Add(time.Hour*24)),
		urlmock.WithShortCode("shortcode"),
	)
	suite.Require().NoError(mockUrlErr)

	actual, actualErr := suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
	suite.Error(actualErr)
	suite.Empty(actual)
}

// saveUrlForNewUser persists a fresh user and a URL owned by that user, returning the save result.
// urls.user_id references users(id), so each URL needs its own owner to exist first.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) saveUrlForNewUser(ctx context.Context, options ...urlmock.MockUrlOption) (domainurl.URL, error) {
	mockUser, mockUserErr := mockidentity.MockUser()
	suite.Require().NoError(mockUserErr)

	_, saveUserErr := suite.userWriteDatastore.Save(ctx, *mockUser)
	suite.Require().NoError(saveUserErr)

	defaults := []urlmock.MockUrlOption{
		urlmock.WithUserId(mockUser.ID().String()),
		urlmock.WithExpiresOn(time.Now().Add(24 * time.Hour)),
	}

	mockUrl, mockUrlErr := urlmock.MockUrl(append(defaults, options...)...)
	suite.Require().NoError(mockUrlErr)

	return suite.urlWriteDatastoreAdapter.Save(ctx, *mockUrl)
}

// TestSave_RejectsTargetAlreadyShortenedByAnotherUser is the global uniqueness rule end to end: once
// any user has shortened a target, nobody else may shorten it again.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestSave_RejectsTargetAlreadyShortenedByAnotherUser() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	_, firstErr := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl("https://example.com/shared-target"))
	suite.Require().NoError(firstErr)

	second, secondErr := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl("https://example.com/shared-target"))
	suite.Require().Error(secondErr)
	suite.True(errdefs.IsConflict(secondErr), "expected a Conflict error, got %v", secondErr)
	suite.ErrorIs(secondErr, errdefs.ErrURLAlreadyExists)
	suite.Empty(second)
}

// TestSave_RejectsTargetAlreadyShortenedBySameUser checks the rule is global rather than per-user:
// the owner cannot shorten the same target twice either.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestSave_RejectsTargetAlreadyShortenedBySameUser() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	mockUser, mockUserErr := mockidentity.MockUser()
	suite.Require().NoError(mockUserErr)
	_, saveUserErr := suite.userWriteDatastore.Save(ctx, *mockUser)
	suite.Require().NoError(saveUserErr)

	newUrl := func() domainurl.URL {
		mockUrl, mockUrlErr := urlmock.MockUrl(
			urlmock.WithUserId(mockUser.ID().String()),
			urlmock.WithExpiresOn(time.Now().Add(24*time.Hour)),
			urlmock.WithOriginalUrl("https://example.com/same-owner-target"),
		)
		suite.Require().NoError(mockUrlErr)
		return *mockUrl
	}

	_, firstErr := suite.urlWriteDatastoreAdapter.Save(ctx, newUrl())
	suite.Require().NoError(firstErr)

	_, secondErr := suite.urlWriteDatastoreAdapter.Save(ctx, newUrl())
	suite.Require().Error(secondErr)
	suite.ErrorIs(secondErr, errdefs.ErrURLAlreadyExists)
}

// TestSave_RejectsDifferentSpellingOfTheSameTarget is the reason OriginalURL normalizes: a trailing
// slash, an uppercase host, a default port or a utm parameter must not buy a second row.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestSave_RejectsDifferentSpellingOfTheSameTarget() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	_, firstErr := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl("https://example.com/respelled"))
	suite.Require().NoError(firstErr)

	respellings := []string{
		"https://EXAMPLE.COM/respelled",
		"https://example.com:443/respelled",
		"https://example.com/respelled/",
		"https://example.com/respelled?utm_source=news",
		"HTTPS://Example.Com:443/respelled/?utm_medium=email",
	}

	for _, respelling := range respellings {
		suite.Run(respelling, func() {
			_, err := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl(respelling))
			suite.Require().Error(err, "%q should have collided with the stored target", respelling)
			suite.ErrorIs(err, errdefs.ErrURLAlreadyExists)
		})
	}
}

// TestSave_AllowsDistinctTargets is the control for the uniqueness tests above: the index must not
// reject genuinely different URLs.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestSave_AllowsDistinctTargets() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	_, firstErr := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl("https://example.com/first-target"))
	suite.Require().NoError(firstErr)

	_, secondErr := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl("https://example.com/second-target"))
	suite.Require().NoError(secondErr)
}

// TestUpdate_DeletingReplacesKeywordsAndReleasesTheTarget covers the soft delete end to end: the DELETED status sets
// deleted_at, which releases the target for re-shortening, and the keywords are replaced rather than appended.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestUpdate_DeletingReplacesKeywordsAndReleasesTheTarget() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	saved, saveErr := suite.saveUrlForNewUser(ctx,
		urlmock.WithOriginalUrl("https://example.com/released-target"),
		urlmock.WithKeywords([]string{"first", "second"}),
	)
	suite.Require().NoError(saveErr)

	readDatastore := NewUrlReadDatastoreAdapter(suite.testPostgresDatabaseClient, suite.config)
	stored, fetchErr := readDatastore.FetchById(ctx, saved.ID().String())
	suite.Require().NoError(fetchErr)
	suite.Require().Len(stored.Keywords(), 2, "the read side must load the keywords Save wrote")

	suite.Require().NoError(stored.SetKeywords([]string{"third"}))
	suite.Require().NoError(stored.MarkDeleted())
	updated, updateErr := suite.urlWriteDatastoreAdapter.Update(ctx, stored)
	suite.Require().NoError(updateErr)
	suite.Equal(domainurl.URLStatusDeleted, updated.Status())
	suite.NotNil(updated.DeletedAt(), "the DELETED status must set deleted_at")

	reread, rereadErr := readDatastore.FetchById(ctx, saved.ID().String())
	suite.Require().NoError(rereadErr)
	suite.Require().Len(reread.Keywords(), 1)
	suite.Equal("third", reread.Keywords()[0].Value)

	_, reshortenErr := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl("https://example.com/released-target"))
	suite.NoError(reshortenErr, "a deleted url must release its target")
}

func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestUpdate_ChangesTheExpiry() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	saved, saveErr := suite.saveUrlForNewUser(ctx)
	suite.Require().NoError(saveErr)

	newExpiry := time.Now().Add(72 * time.Hour)
	suite.Require().NoError(saved.UpdateExpiry(newExpiry))
	updated, updateErr := suite.urlWriteDatastoreAdapter.Update(ctx, saved)
	suite.Require().NoError(updateErr)

	suite.WithinDuration(newExpiry, updated.ExpiresOn(), time.Microsecond)
	suite.Equal(domainurl.URLStatusActive, updated.Status())
	suite.Nil(updated.DeletedAt())
}

// TestUpdate_AStaleCopyIsAConflictAndDoesNotOverwriteTheNewerChange covers the expiry job racing a user: both load the
// url, the user suspends it, and the job's copy, which is still ACTIVE, must not turn it back into EXPIRED.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestUpdate_AStaleCopyIsAConflictAndDoesNotOverwriteTheNewerChange() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	saved, saveErr := suite.saveUrlForNewUser(ctx)
	suite.Require().NoError(saveErr)
	readDatastore := NewUrlReadDatastoreAdapter(suite.testPostgresDatabaseClient, suite.config)
	forUser, fetchErr := readDatastore.FetchById(ctx, saved.ID().String())
	suite.Require().NoError(fetchErr)
	forJob, fetchErr := readDatastore.FetchById(ctx, saved.ID().String())
	suite.Require().NoError(fetchErr)

	suite.Require().NoError(forUser.Suspend(domainurl.SuspensionReasonSpam))
	_, userErr := suite.urlWriteDatastoreAdapter.Update(ctx, forUser)
	suite.Require().NoError(userErr)

	suite.Require().NoError(forJob.MarkExpired())
	_, jobErr := suite.urlWriteDatastoreAdapter.Update(ctx, forJob)
	suite.True(errdefs.IsConflict(jobErr), "expected Conflict, got %v", jobErr)

	stored, rereadErr := readDatastore.FetchById(ctx, saved.ID().String())
	suite.Require().NoError(rereadErr)
	suite.Equal(domainurl.URLStatusSuspended, stored.Status(), "the newer change stands")
	suite.Zero(suite.outboxEvents(ctx, saved.ID().String(), "url.expired"), "the lost update wrote no event")
}

// countRows runs a COUNT(*) query against the test database.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) countRows(ctx context.Context, query string, args ...any) int {
	var count int
	suite.Require().NoError(suite.testPostgresDatabaseClient.GetDB().QueryRow(ctx, query, args...).Scan(&count))
	return count
}

// outboxEvents counts the outbox events of one type recorded for a url.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) outboxEvents(ctx context.Context, urlID, eventType string) int {
	return suite.countRows(ctx, `SELECT COUNT(*) FROM outbox_events WHERE partition_key = $1 AND event_type = $2`, urlID, eventType)
}

// TestSoftDelete_MarksDeletedReleasesTheTargetAndRecordsOneEvent covers the soft delete end to end: status and
// deleted_at are set together, the keywords stay, URLDeleted is in the outbox, the target can be shortened again, and a
// second delete is a Conflict that records nothing.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestSoftDelete_MarksDeletedReleasesTheTargetAndRecordsOneEvent() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	saved, saveErr := suite.saveUrlForNewUser(ctx,
		urlmock.WithOriginalUrl("https://example.com/soft-deleted-target"),
		urlmock.WithKeywords([]string{"kept"}),
	)
	suite.Require().NoError(saveErr)
	id := saved.ID().String()

	suite.Require().NoError(suite.urlWriteDatastoreAdapter.SoftDelete(ctx, id))

	stored, fetchErr := NewUrlReadDatastoreAdapter(suite.testPostgresDatabaseClient, suite.config).FetchById(ctx, id)
	suite.Require().NoError(fetchErr)
	suite.Equal(domainurl.URLStatusDeleted, stored.Status())
	suite.NotNil(stored.DeletedAt(), "the DELETED status must set deleted_at")
	suite.Len(stored.Keywords(), 1, "a soft delete keeps the keywords")
	suite.Equal(1, suite.outboxEvents(ctx, id, "url.deleted"))

	_, reshortenErr := suite.saveUrlForNewUser(ctx, urlmock.WithOriginalUrl("https://example.com/soft-deleted-target"))
	suite.NoError(reshortenErr, "a deleted url must release its target")

	again := suite.urlWriteDatastoreAdapter.SoftDelete(ctx, id)
	suite.True(errdefs.IsConflict(again), "deleting twice is a Conflict, got %v", again)
	suite.Equal(1, suite.outboxEvents(ctx, id, "url.deleted"), "the second delete must not record another event")
}

// TestDelete_PurgesTheUrlWithItsKeywordsAndScans covers the hard delete: the row goes with its keywords and its scan
// results (the ON DELETE CASCADE of migration 000004), and URLPurged stays in the outbox.
func (suite *UrlWriteDatastoreAdapterIntegrationTestSuite) TestDelete_PurgesTheUrlWithItsKeywordsAndScans() {
	ctx, cancel := context.WithTimeout(context.Background(), suite.config.OperationTimeout)
	defer cancel()

	saved, saveErr := suite.saveUrlForNewUser(ctx, urlmock.WithKeywords([]string{"gone"}))
	suite.Require().NoError(saveErr)
	id := saved.ID().String()

	_, scanErr := suite.testPostgresDatabaseClient.GetDB().Exec(ctx,
		`INSERT INTO url_scans (url_id, provider, result) VALUES ($1, 'google_safe_browsing', 'safe')`, id)
	suite.Require().NoError(scanErr)

	suite.Require().NoError(suite.urlWriteDatastoreAdapter.Delete(ctx, id))

	suite.Zero(suite.countRows(ctx, `SELECT COUNT(*) FROM urls WHERE id = $1`, id))
	suite.Zero(suite.countRows(ctx, `SELECT COUNT(*) FROM keywords WHERE url_id = $1`, id))
	suite.Zero(suite.countRows(ctx, `SELECT COUNT(*) FROM url_scans WHERE url_id = $1`, id))
	suite.Equal(1, suite.outboxEvents(ctx, id, "url.purged"))

	suite.True(errdefs.IsNotFound(suite.urlWriteDatastoreAdapter.Delete(ctx, id)), "a purged url is gone")
}
