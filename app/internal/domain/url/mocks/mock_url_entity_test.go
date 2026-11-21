package urlmock

import (
	"testing"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
)

// TestMockUrl_AlwaysBuildsAValidUrl pins down three separate ways MockUrl used to fail at random:
//
//   - faker.Word ignores options.WithRandomStringLength, so the short code and custom alias violated
//     the ShortCode (6-10) and CustomAlias (3-12) length rules whenever the dictionary word it
//     returned was the wrong length.
//   - faker.URL() draws from path templates, one of which contains "xxx", which the domain's
//     FilterRegex rejects — about one call in a thousand.
//
// The iteration count is high enough that any of these would surface.
func TestMockUrl_AlwaysBuildsAValidUrl(t *testing.T) {
	for i := range 20000 {
		mockUrl, err := MockUrl()
		if err != nil {
			t.Fatalf("MockUrl() failed on iteration %d: %v", i, err)
		}

		if _, shortCodeErr := url.NewShortCode(mockUrl.ShortCode().Value()); shortCodeErr != nil {
			t.Fatalf("MockUrl() produced an invalid short code %q on iteration %d: %v",
				mockUrl.ShortCode().Value(), i, shortCodeErr)
		}

		if _, aliasErr := url.NewCustomAlias(mockUrl.CustomAlias().Value()); aliasErr != nil {
			t.Fatalf("MockUrl() produced an invalid custom alias %q on iteration %d: %v",
				mockUrl.CustomAlias().Value(), i, aliasErr)
		}

		if _, originalUrlErr := url.NewOriginalURL(mockUrl.OriginalURL().Value()); originalUrlErr != nil {
			t.Fatalf("MockUrl() produced an invalid original url %q on iteration %d: %v",
				mockUrl.OriginalURL().Value(), i, originalUrlErr)
		}
	}
}

// TestMockUrl_ShortCodesAreDistinct guards the property integration tests rely on: urls.short_code
// carries a unique index, so seeding several mock URLs in one test must not collide.
func TestMockUrl_ShortCodesAreDistinct(t *testing.T) {
	const iterations = 1000

	seen := make(map[string]struct{}, iterations)
	for i := range iterations {
		mockUrl, err := MockUrl()
		if err != nil {
			t.Fatalf("MockUrl() failed on iteration %d: %v", i, err)
		}

		shortCode := mockUrl.ShortCode().Value()
		if _, duplicate := seen[shortCode]; duplicate {
			t.Fatalf("MockUrl() reused short code %q by iteration %d", shortCode, i)
		}
		seen[shortCode] = struct{}{}
	}
}
