package urlsvc

import (
	"errors"
	"sync"
	"testing"
	"time"

	"bou.ke/monkey"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/test/data"
	"github.com/sanctumlabs/curtz/app/test/mocks"
	"github.com/stretchr/testify/assert"
)

func TestUrlSvc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UrlSvc Suite")
}

var _ = Describe("UrlSvc", func() {
	var (
		mockCtrl         *gomock.Controller
		mockUrlWriteRepo *mocks.MockUrlWriteRepository
		mockUrlReadRepo  *mocks.MockUrlReadRepository
		mockUserSvc      *mocks.MockUserService
		mockCache        *mocks.MockCacheService
	)

	urlSvc := &UrlSvc{}

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockUrlWriteRepo = mocks.NewMockUrlWriteRepository(mockCtrl)
		mockUrlReadRepo = mocks.NewMockUrlReadRepository(mockCtrl)
		mockUserSvc = mocks.NewMockUserService(mockCtrl)
		mockCache = mocks.NewMockCacheService(mockCtrl)
		urlSvc.urlReadRepo = mockUrlReadRepo
		urlSvc.urlWriteRepo = mockUrlWriteRepo
		urlSvc.userSvc = mockUserSvc
		urlSvc.cache = mockCache
	})

	When("Looking up a URL by short code and there is a cache miss", func() {
		Describe("where error is returned by cache", func() {
			It("should call url read repository & return empty string and error if there is an error fetching from repository", func() {
				defer mockCtrl.Finish()

				shortCode := "short-code"
				cacheErr := errors.New("Failed to get url")
				expectedErr := errors.New("Failed to get url by short code")

				mockCache.
					EXPECT().
					LookupUrl(shortCode).
					Return("", cacheErr)

				mockUrlReadRepo.
					EXPECT().
					GetByShortCode(shortCode).
					Return(entities.URL{}, expectedErr)

				actual, actualErr := urlSvc.LookupUrl(shortCode)

				if assert.Error(GinkgoT(), actualErr) {
					assert.Equal(GinkgoT(), expectedErr, actualErr)
				}

				Expect(actual).To(Equal(""))
			})
		})

		Describe("when empty string is returned by cache", func() {
			It("should call url read repository & return empty string & error if there is an error fetching url from read repository", func() {
				defer mockCtrl.Finish()

				shortCode := "short-code"
				expectedErr := errors.New("Failed to get url by short code")

				mockCache.
					EXPECT().
					LookupUrl(shortCode).
					Return("", nil)

				mockUrlReadRepo.
					EXPECT().
					GetByShortCode(shortCode).
					Return(entities.URL{}, expectedErr)

				actual, actualErr := urlSvc.LookupUrl(shortCode)

				if assert.Error(GinkgoT(), actualErr) {
					assert.Equal(GinkgoT(), expectedErr, actualErr)
				}

				Expect(actual).To(Equal(""))
			})
		})

		When("either an empty string or error is returned by cache", func() {
			Describe("should call url read repository & return url", func() {
				It("if url is inactive(expired), an empty string & error should be returned", func() {
					defer mockCtrl.Finish()

					shortCode := "short"
					expectedErr := errdefs.ErrURLExpired

					userId := identifier.New()
					customAlias := "custom"
					originalUrl := "https://example.com"
					expiresOn := time.Date(2020, 1, 1, 1, 1, 1, 1, time.UTC)
					mockUrl := data.MockUrl(userId.String(), originalUrl, customAlias, shortCode, expiresOn, []string{})

					mockCache.
						EXPECT().
						LookupUrl(shortCode).
						Return("", nil)

					mockUrlReadRepo.
						EXPECT().
						GetByShortCode(shortCode).
						Return(*mockUrl, nil)

					actual, actualErr := urlSvc.LookupUrl(shortCode)

					if assert.Error(GinkgoT(), actualErr) {
						assert.Equal(GinkgoT(), expectedErr, actualErr)
					}

					Expect(actual).To(Equal(""))
				})
			})

			// FIXME: Skipped tests need to find a better way of testing goroutines
			XIt("if url is active(not expired), url should be saved to cache, increase hit counter & original url should be returned", func() {
				defer mockCtrl.Finish()
				defer GinkgoRecover()
				// defer goleak.VerifyNone(GinkgoT())

				monkey.Patch(time.Now, func() time.Time {
					return time.Date(2022, 9, 19, 16, 20, 00, 651387237, time.UTC)
				})

				wg := sync.WaitGroup{}
				wg.Add(2)

				shortCode := "short"

				userId := identifier.New()

				customAlias := "custom"
				originalUrl := "https://example.com"
				expiresOn := time.Now().Add(time.Hour * 1)
				mockUrl := data.MockUrl(userId.String(), originalUrl, customAlias, shortCode, expiresOn, []string{})

				mockCache.
					EXPECT().
					LookupUrl(shortCode).
					Return("", nil)

				mockUrlReadRepo.
					EXPECT().
					GetByShortCode(shortCode).
					Return(*mockUrl, nil)

				duration := time.Until(mockUrl.GetExpiresOn())

				mockCache.
					EXPECT().
					SaveURL(shortCode, originalUrl, duration).
					Do(func(arg0, arg1, arg2 any) {
						defer wg.Done()
					})

				mockUrlWriteRepo.
					EXPECT().
					IncrementHits(shortCode).
					Do(func(arg0 any) {
						defer wg.Done()
					})

				actual, actualErr := urlSvc.LookupUrl(shortCode)

				wg.Wait()

				assert.NoError(GinkgoT(), actualErr)
				Expect(actual).To(Equal(originalUrl))
			})
		})
	})

	When("Looking up a URL by short code & there is a cache hit", func() {
		Describe("and the cache returns nil error", func() {
			XIt("should return original url & increment hit counter", func() {
				defer mockCtrl.Finish()
				defer GinkgoRecover()
				// defer goleak.VerifyNone(GinkgoT())

				monkey.Patch(time.Now, func() time.Time {
					return time.Date(2022, 9, 19, 16, 20, 00, 651387237, time.UTC)
				})

				wg := sync.WaitGroup{}
				wg.Add(1)

				shortCode := "short-code"
				userId := identifier.New()
				customAlias := "custom"
				originalUrl := "https://example.com"
				expiresOn := time.Now().Add(time.Hour * 1)

				mockUrl := data.MockUrl(userId.String(), originalUrl, customAlias, shortCode, expiresOn, []string{})

				mockCache.
					EXPECT().
					LookupUrl(shortCode).
					Return(originalUrl, nil)

				mockUrlReadRepo.
					EXPECT().
					GetByShortCode(shortCode).Times(0)

				duration := time.Until(mockUrl.GetExpiresOn())

				mockCache.
					EXPECT().
					SaveURL(shortCode, originalUrl, duration).Times(0)

				mockUrlWriteRepo.
					EXPECT().
					IncrementHits(shortCode).
					Do(func(arg0 any) {
						defer wg.Done()
					})

				actual, actualErr := urlSvc.LookupUrl(shortCode)

				wg.Wait()

				assert.NoError(GinkgoT(), actualErr)
				Expect(actual).To(Equal(originalUrl))
			})
		})
	})
})
