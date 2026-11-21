package write

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sanctumlabs/curtz/app/internal/core/contracts"
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/test/data"
	"github.com/sanctumlabs/curtz/app/test/mocks"
	"github.com/stretchr/testify/assert"
)

func TestUrlWriteSvc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UrlWriteSvc Suite")
}

var _ = Describe("UrlWriteSvc", func() {
	var (
		mockCtrl         *gomock.Controller
		mockUrlWriteRepo *mocks.MockUrlWriteRepository
		mockUserSvc      *mocks.MockUserService
	)

	urlWriteSvc := &UrlWriteSvc{}

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockUrlWriteRepo = mocks.NewMockUrlWriteRepository(mockCtrl)
		mockUserSvc = mocks.NewMockUserService(mockCtrl)
		urlWriteSvc.repo = mockUrlWriteRepo
		urlWriteSvc.userSvc = mockUserSvc
	})

	When("Removing/Deleting a url with a given a ID", func() {
		It("should return an error when there is a failure deleting a url", func() {
			urlID := identifier.New().String()
			expectedErr := errors.New("Failed to delete url by id")

			mockUrlWriteRepo.
				EXPECT().
				Delete(urlID).
				Return(expectedErr)

			actual := urlWriteSvc.Remove(urlID)

			if assert.Error(GinkgoT(), actual) {
				assert.Equal(GinkgoT(), expectedErr, actual)
			}
		})

		It("should not return an error when there is a success deleting a url", func() {
			urlID := identifier.New().String()

			mockUrlWriteRepo.
				EXPECT().
				Delete(urlID).
				Return(nil)

			actual := urlWriteSvc.Remove(urlID)

			assert.NoError(GinkgoT(), actual)
		})
	})

	When("Updating a url", func() {
		It("should return error if we can't get a user by the specified id", func() {
			userId := identifier.New()
			urlId := identifier.New()
			customAlias := "fnsoaks"
			keywords := []string{}
			expiresOn := time.Now().Add(time.Hour + 1)

			request := contracts.UpdateUrlRequest{
				UserId:      userId.String(),
				UrlId:       urlId.String(),
				CustomAlias: customAlias,
				Keywords:    keywords,
				ExpiresOn:   &expiresOn,
			}

			mockUserSvc.
				EXPECT().
				GetUserByID(userId.String()).
				Return(entities.User{}, errors.New("Failed to get user by id"))

			_, actualErr := urlWriteSvc.UpdateUrl(request)
			assert.Error(GinkgoT(), actualErr)
		})

		It("should return error if the expiration date is in the past", func() {
			userId := identifier.New()
			urlId := identifier.New()
			customAlias := "fnsoaks"
			keywords := []string{}
			expiresOn := time.Now().Add(-10)

			request := contracts.UpdateUrlRequest{
				UserId:      userId.String(),
				UrlId:       urlId.String(),
				CustomAlias: customAlias,
				Keywords:    keywords,
				ExpiresOn:   &expiresOn,
			}

			mockUser, err := data.MockUser("johndoe@example.com", "johndoe")
			assert.NoError(GinkgoT(), err)

			mockUserSvc.
				EXPECT().
				GetUserByID(userId.String()).
				Return(mockUser, nil)

			_, actualErr := urlWriteSvc.UpdateUrl(request)
			assert.Error(GinkgoT(), actualErr)
		})

		It("should return error if there is a failure in updating url", func() {
			userId := identifier.New()
			urlId := identifier.New()
			customAlias := "fnsoaks"
			keywords := []string{}
			expiresOn := time.Now().Add(time.Hour + 10)

			request := contracts.UpdateUrlRequest{
				UserId:      userId.String(),
				UrlId:       urlId.String(),
				CustomAlias: customAlias,
				Keywords:    keywords,
				ExpiresOn:   &expiresOn,
			}

			mockUser, err := data.MockUser("johndoe@example.com", "johndoe")
			assert.NoError(GinkgoT(), err)

			mockUserSvc.
				EXPECT().
				GetUserByID(userId.String()).
				Return(mockUser, nil)

			mockUrlWriteRepo.
				EXPECT().
				Update(urlId.String(), customAlias, gomock.Any(), &expiresOn).
				Return(entities.URL{}, errors.New("failed to update url"))

			_, actualErr := urlWriteSvc.UpdateUrl(request)
			assert.Error(GinkgoT(), actualErr)
		})

		It("should return updated url on success", func() {
			userId := identifier.New()
			urlId := identifier.New()
			customAlias := "fnsoaks"
			keywords := []string{}
			expiresOn := time.Now().Add(time.Hour + 10)

			request := contracts.UpdateUrlRequest{
				UserId:      userId.String(),
				UrlId:       urlId.String(),
				CustomAlias: customAlias,
				Keywords:    keywords,
				ExpiresOn:   &expiresOn,
			}

			mockUser, err := data.MockUser("johndoe@example.com", "johndoe")
			assert.NoError(GinkgoT(), err)

			originalUrl := "http://example.com"
			shortCode := "feamp"
			mockUrl := data.MockUrl(userId.String(), originalUrl, customAlias, shortCode, expiresOn, keywords)

			mockUserSvc.
				EXPECT().
				GetUserByID(userId.String()).
				Return(mockUser, nil)

			mockUrlWriteRepo.
				EXPECT().
				Update(urlId.String(), customAlias, gomock.Any(), &expiresOn).
				Return(*mockUrl, nil)

			actual, actualErr := urlWriteSvc.UpdateUrl(request)
			assert.NoError(GinkgoT(), actualErr)

			assert.Equal(GinkgoT(), *mockUrl, actual)
		})
	})
})
