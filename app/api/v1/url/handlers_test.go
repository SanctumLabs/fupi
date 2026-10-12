package urlapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	apimiddleware "github.com/sanctumlabs/fupi/app/api/middleware"
	urlapp "github.com/sanctumlabs/fupi/app/internal/application/url"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/core/ports/repository"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	urlmock "github.com/sanctumlabs/fupi/app/internal/domain/url/mocks"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
	mockports "github.com/sanctumlabs/fupi/app/internal/ports/mocks"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

const (
	baseURI      = "/api/v1/fupi"
	shortBaseURL = "https://fu.pi/"
)

type UrlHandlersTestSuite struct {
	suite.Suite
	mockCtrl      *gomock.Controller
	mockUrls      *urlmock.MockUrlDatastore
	mockGenerator *mockports.MockShortCodeGenerator
	app           *fiber.App
	userID        string
}

func TestUrlHandlersTestSuite(t *testing.T) {
	suite.Run(t, new(UrlHandlersTestSuite))
}

func (suite *UrlHandlersTestSuite) SetupTest() {
	suite.mockCtrl = gomock.NewController(suite.T())
	suite.mockUrls = urlmock.NewMockUrlDatastore(suite.mockCtrl)
	suite.mockGenerator = mockports.NewMockShortCodeGenerator(suite.mockCtrl)
	suite.userID = entity.IDToString(entity.NewID())

	svc := urlapp.NewService(suite.mockUrls, suite.mockGenerator)

	app := fiber.New()
	// Stands in for the auth middleware: a request carrying X-Test-User is authenticated as that user.
	app.Use(func(ctx *fiber.Ctx) error {
		if userID := ctx.Get("X-Test-User"); userID != "" {
			ctx.Locals(apimiddleware.UserIDContextKey, userID)
		}
		return ctx.Next()
	})
	for _, route := range NewRouter(baseURI, shortBaseURL, svc).Routes() {
		app.Add(route.Method(), route.Path(), route.Handler())
	}
	suite.app = app
}

func (suite *UrlHandlersTestSuite) AfterTest(_, _ string) {
	suite.mockCtrl.Finish()
}

// do sends a request as the suite's user and returns the status, the decoded JSON body and the Location header.
func (suite *UrlHandlersTestSuite) do(method, target, body string) (int, map[string]any, string) {
	suite.T().Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}
	req.Header.Set("X-Test-User", suite.userID)

	resp, err := suite.app.Test(req)
	suite.Require().NoError(err)

	payload := map[string]any{}
	raw, readErr := io.ReadAll(resp.Body)
	suite.Require().NoError(readErr)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	return resp.StatusCode, payload, resp.Header.Get(fiber.HeaderLocation)
}

func (suite *UrlHandlersTestSuite) ownedUrl(options ...urlmock.MockUrlOption) url.URL {
	defaults := []urlmock.MockUrlOption{
		urlmock.WithId(entity.NewID()),
		urlmock.WithUserId(suite.userID),
		urlmock.WithShortCode("000abc"),
		urlmock.WithExpiresOn(time.Now().Add(24 * time.Hour)),
	}
	u, err := urlmock.MockUrl(append(defaults, options...)...)
	suite.Require().NoError(err)
	return *u
}

// --- POST /urls -------------------------------------------------------------------------------

func (suite *UrlHandlersTestSuite) TestCreate_Returns201WithTheShortUrl() {
	code, _ := url.NewShortCode("000abc")
	suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(code, nil)
	suite.mockUrls.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) { return u, nil })

	status, body, _ := suite.do("POST", baseURI+"/urls", `{
		"original_url": "https://example.com/some/long/path",
		"custom_alias": "my-alias",
		"expires_on": "`+time.Now().Add(time.Hour).Format(time.RFC3339)+`",
		"keywords": ["docs"]
	}`)

	suite.Equal(fiber.StatusCreated, status)
	suite.Equal("000abc", body["short_code"])
	suite.Equal("https://fu.pi/000abc", body["short_url"])
	suite.Equal("my-alias", body["custom_alias"])
	suite.Equal(suite.userID, body["user_id"])
	suite.Equal("ACTIVE", body["status"])
	suite.Equal([]any{"docs"}, body["keywords"])
}

func (suite *UrlHandlersTestSuite) TestCreate_MissingFieldsAre422() {
	for name, payload := range map[string]string{
		"not json":       `{`,
		"no url":         `{"expires_on": "2099-01-01T00:00:00Z"}`,
		"no expiry date": `{"original_url": "https://example.com/some/long/path"}`,
	} {
		suite.Run(name, func() {
			status, _, _ := suite.do("POST", baseURI+"/urls", payload)
			suite.Equal(fiber.StatusUnprocessableEntity, status)
		})
	}
}

func (suite *UrlHandlersTestSuite) TestCreate_InvalidUrlIs400() {
	code, _ := url.NewShortCode("000abc")
	suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(code, nil)

	status, _, _ := suite.do("POST", baseURI+"/urls", `{"original_url": "http://localhost/some/path", "expires_on": "2099-01-01T00:00:00Z"}`)

	suite.Equal(fiber.StatusBadRequest, status)
}

func (suite *UrlHandlersTestSuite) TestCreate_RejectedUrlMessagesCarryNoFormatVerbs() {
	code, _ := url.NewShortCode("000abc")
	suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(code, nil).AnyTimes()

	for name, tc := range map[string]struct{ originalURL, message string }{
		"malformed": {"this is not a url at all", "url is invalid"},
		"filtered":  {"http://localhost/some/path", "url is not allowed"},
		"too short": {"not a url", "must be between 15 and 2048 characters"},
	} {
		suite.Run(name, func() {
			status, body, _ := suite.do("POST", baseURI+"/urls", `{"original_url": "`+tc.originalURL+`", "expires_on": "2099-01-01T00:00:00Z"}`)

			suite.Equal(fiber.StatusBadRequest, status)
			suite.Contains(body["message"], tc.message)
			suite.NotContains(body["message"], "%")
		})
	}
}

func (suite *UrlHandlersTestSuite) TestCreate_AlreadyShortenedIs409WithoutTheDatabaseError() {
	code, _ := url.NewShortCode("000abc")
	suite.mockGenerator.EXPECT().Next(gomock.Any()).Return(code, nil)
	suite.mockUrls.EXPECT().Save(gomock.Any(), gomock.Any()).
		Return(url.URL{}, errdefs.Conflict(errors.Join(errdefs.ErrURLAlreadyExists, errors.New(`duplicate key value violates unique constraint "idx_urls_original_url"`))))

	status, body, _ := suite.do("POST", baseURI+"/urls", `{"original_url": "https://example.com/some/long/path", "expires_on": "2099-01-01T00:00:00Z"}`)

	suite.Equal(fiber.StatusConflict, status)
	suite.Equal("this url has already been shortened", body["message"])
}

func (suite *UrlHandlersTestSuite) TestManagementRoutesRequireAnAuthenticatedCaller() {
	resp, err := suite.app.Test(httptest.NewRequest("GET", baseURI+"/urls", nil))
	suite.Require().NoError(err)
	suite.Equal(fiber.StatusUnauthorized, resp.StatusCode)
}

// --- GET /urls, GET /urls/:id -----------------------------------------------------------------

func (suite *UrlHandlersTestSuite) TestList_ReturnsThePage() {
	suite.mockUrls.EXPECT().FetchByUserId(gomock.Any(), suite.userID, common.NewRequestParams()).
		Return(repository.FetchRecordsResponse[url.URL]{Records: []url.URL{suite.ownedUrl()}, Total: 1, Page: 1, Size: 1}, nil)

	status, body, _ := suite.do("GET", baseURI+"/urls", "")

	suite.Equal(fiber.StatusOK, status)
	suite.Equal(float64(1), body["total"])
	suite.Len(body["records"], 1)
}

func (suite *UrlHandlersTestSuite) TestList_PassesPagingOrderingAndStatusThrough() {
	want := common.NewRequestParams(
		common.WithRequestLimit(100), common.WithOffset(100),
		common.WithOrderBy(common.OrderByUpdatedAt), common.WithSortOrder(common.SortOderAsc),
		common.WithStatus("EXPIRED"))
	suite.mockUrls.EXPECT().FetchByUserId(gomock.Any(), suite.userID, want).
		Return(repository.FetchRecordsResponse[url.URL]{Records: []url.URL{}, Total: 150, Page: 2, Size: 50}, nil)

	status, body, _ := suite.do("GET", baseURI+"/urls?page=2&size=100&order_by=updated_at&sort_order=asc&status=expired", "")

	suite.Equal(fiber.StatusOK, status)
	suite.Equal(float64(150), body["total"])
	suite.Equal(float64(2), body["page"])
}

// TestList_IgnoresIncludeDeleted asserts a caller cannot ask for their deleted urls.
func (suite *UrlHandlersTestSuite) TestList_IgnoresIncludeDeleted() {
	suite.mockUrls.EXPECT().FetchByUserId(gomock.Any(), suite.userID, common.NewRequestParams()).
		Return(repository.FetchRecordsResponse[url.URL]{Records: []url.URL{}}, nil)

	status, _, _ := suite.do("GET", baseURI+"/urls?include_deleted=true", "")

	suite.Equal(fiber.StatusOK, status)
}

func (suite *UrlHandlersTestSuite) TestList_InvalidQueryIs422NamingTheParameter() {
	cases := map[string]string{
		"page=0":              "page",
		"page=abc":            "page",
		"size=0":              "size",
		"size=101":            "size",
		"size=-1":             "size",
		"order_by=deleted_at": "order_by",
		"order_by=nope":       "order_by",
		"sort_order=up":       "sort_order",
		"status=DELETED":      "status",
		"status=nope":         "status",
	}
	for query, parameter := range cases {
		suite.Run(query, func() {
			status, body, _ := suite.do("GET", baseURI+"/urls?"+query, "")

			suite.Equal(fiber.StatusUnprocessableEntity, status)
			suite.Contains(body["message"], parameter)
		})
	}
}

func (suite *UrlHandlersTestSuite) TestGet_AnotherUsersUrlIs404() {
	someoneElses := suite.ownedUrl(urlmock.WithUserId(entity.IDToString(entity.NewID())))
	id := entity.IDToString(someoneElses.ID())
	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(someoneElses, nil)

	status, body, _ := suite.do("GET", baseURI+"/urls/"+id, "")

	suite.Equal(fiber.StatusNotFound, status)
	suite.Equal("url not found", body["message"])
}

// --- PATCH /urls/:id --------------------------------------------------------------------------

func (suite *UrlHandlersTestSuite) TestUpdate_ClearsKeywordsWhenSentEmpty() {
	owned := suite.ownedUrl(urlmock.WithKeywords([]string{"old"}))
	id := entity.IDToString(owned.ID())
	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(owned, nil)
	suite.mockUrls.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) { return u, nil })

	status, body, _ := suite.do("PATCH", baseURI+"/urls/"+id, `{"keywords": []}`)

	suite.Equal(fiber.StatusOK, status)
	suite.Equal([]any{}, body["keywords"])
}

func (suite *UrlHandlersTestSuite) TestUpdate_EmptyPatchIs422() {
	status, _, _ := suite.do("PATCH", baseURI+"/urls/"+entity.IDToString(entity.NewID()), `{}`)

	suite.Equal(fiber.StatusUnprocessableEntity, status)
}

// --- DELETE /urls/:id -------------------------------------------------------------------------

func (suite *UrlHandlersTestSuite) TestDelete_Returns204() {
	owned := suite.ownedUrl()
	id := entity.IDToString(owned.ID())
	suite.mockUrls.EXPECT().FetchById(gomock.Any(), id).Return(owned, nil)
	suite.mockUrls.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u url.URL) (url.URL, error) { return u, nil })

	status, _, _ := suite.do("DELETE", baseURI+"/urls/"+id, "")

	suite.Equal(fiber.StatusNoContent, status)
}

// --- GET /:code -------------------------------------------------------------------------------

func (suite *UrlHandlersTestSuite) TestRedirect_Sends302ToTheTarget() {
	active := suite.ownedUrl(urlmock.WithOriginalUrl("https://example.com/target/page"))
	suite.mockUrls.EXPECT().FetchByShortCode(gomock.Any(), "000abc").Return(active, nil)

	resp, err := suite.app.Test(httptest.NewRequest("GET", "/000abc", nil))

	suite.Require().NoError(err)
	suite.Equal(fiber.StatusFound, resp.StatusCode)
	suite.Equal("https://example.com/target/page", resp.Header.Get(fiber.HeaderLocation))
}

func (suite *UrlHandlersTestSuite) TestRedirect_UnknownCodeIs404() {
	suite.mockUrls.EXPECT().FetchByShortCode(gomock.Any(), "nope42").Return(url.URL{}, errdefs.NotFound(errors.New("no rows")))
	suite.mockUrls.EXPECT().FetchByCustomAlias(gomock.Any(), "nope42").Return(url.URL{}, errdefs.NotFound(errors.New("no rows")))

	resp, err := suite.app.Test(httptest.NewRequest("GET", "/nope42", nil))

	suite.Require().NoError(err)
	suite.Equal(fiber.StatusNotFound, resp.StatusCode)
}

func TestRedirectPathPattern(t *testing.T) {
	for path, want := range map[string]bool{
		"/000abc":           true,
		"/my-alias":         true,
		"/ab":               false,
		"/thirteenchars":    false,
		"/health/ready":     false,
		"/api":              true, // a single segment; real routes are registered before the redirect
		"/favicon.ico":      false,
		"/api/v1/fupi/urls": false,
	} {
		if got := RedirectPathPattern.MatchString(path); got != want {
			t.Errorf("RedirectPathPattern.MatchString(%q) = %v, want %v", path, got, want)
		}
	}
}
