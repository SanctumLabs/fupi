package url

import (
	"testing"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
)

func createValidURLParams() URLParams {
	now := time.Now()
	return URLParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{
				EntityIDParams: entity.EntityIDParams{
					ID:    entity.NewID(),
					KeyID: entity.NewKeyID(),
				},
				EntityTimestampParams: entity.EntityTimestampParams{
					CreatedAt: now,
					UpdatedAt: now,
					DeletedAt: nil,
				},
				Metadata: nil,
			},
			DomainEvents: nil,
		},
		UserId:      entity.IDToString(entity.NewID()),
		ShortCode:   "abcdef12",
		CustomAlias: "test-alias",
		OriginalUrl: "https://example.com/path/to/resource",
		ExpiresOn:   now.Add(24 * time.Hour),
		Keywords:    []string{"test", "example"},
		Status:      URLStatusActive,
	}
}

type urlTestCase struct {
	name   string
	params URLParams
}

var urlTestCases = []urlTestCase{
	{
		name: "valid URL params should create URL successfully",
		params: func() URLParams {
			p := createValidURLParams()
			return p
		}(),
	},
	{
		name: "invalid original URL should return error",
		params: func() URLParams {
			p := createValidURLParams()
			p.OriginalUrl = "invalid-url"
			return p
		}(),
	},
	{
		name: "invalid custom alias should return error",
		params: func() URLParams {
			p := createValidURLParams()
			p.CustomAlias = "ab"
			return p
		}(),
	},
	{
		name: "invalid keyword should return error",
		params: func() URLParams {
			p := createValidURLParams()
			p.Keywords = []string{"ok", "not ok"}
			return p
		}(),
	},
	{
		name: "invalid short code should return error",
		params: func() URLParams {
			p := createValidURLParams()
			p.ShortCode = "abc"
			return p
		}(),
	},
	{
		name: "invalid user ID should return error",
		params: func() URLParams {
			p := createValidURLParams()
			p.UserId = "invalid-uuid"
			return p
		}(),
	},
	{
		name: "too many keywords should return error",
		params: func() URLParams {
			p := createValidURLParams()
			p.Keywords = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}
			return p
		}(),
	},
}

func TestNewUrl_NoCustomAlias(t *testing.T) {
	p := createValidURLParams()
	p.CustomAlias = ""
	url, err := NewUrl(p)
	if err != nil {
		t.Fatalf("NewUrl() without custom alias returned error: %v", err)
	}
	if !url.CustomAlias().IsZero() {
		t.Errorf("expected zero custom alias, got %q", url.CustomAlias().Value())
	}
}

func TestNewUrl_HydratesExpiredUrlWithPastExpiry(t *testing.T) {
	p := createValidURLParams()
	p.ExpiresOn = time.Now().Add(-24 * time.Hour)
	p.Status = URLStatusExpired
	if _, err := NewUrl(p); err != nil {
		t.Fatalf("NewUrl() should hydrate an expired url, got error: %v", err)
	}
}

func TestNewUrl(t *testing.T) {
	for _, tc := range urlTestCases {
		t.Run(tc.name, func(t *testing.T) {
			url, err := NewUrl(tc.params)
			if tc.name == "valid URL params should create URL successfully" {
				if err != nil {
					t.Errorf("NewUrl() = (%v, %v), expected no error, got %v", url, err, err)
				}
				if url != nil {
					if url.OriginalURL().Value() != tc.params.OriginalUrl {
						t.Errorf("OriginalURL mismatch: expected %s, got %s", tc.params.OriginalUrl, url.OriginalURL().Value())
					}
					if url.ShortCode().Value() != tc.params.ShortCode {
						t.Errorf("ShortCode mismatch: expected %s, got %s", tc.params.ShortCode, url.ShortCode().Value())
					}
					if url.CustomAlias().Value() != tc.params.CustomAlias {
						t.Errorf("CustomAlias mismatch: expected %s, got %s", tc.params.CustomAlias, url.CustomAlias().Value())
					}
				}
			} else {
				if err == nil {
					t.Errorf("NewUrl() = (%v, %v), expected error for invalid case, got no error", url, err)
				}
			}
		})
	}
}

func TestURL_IsActive(t *testing.T) {
	tests := []struct {
		name      string
		expiresOn time.Time
		status    URLStatus
		expected  bool
	}{
		{
			name:      "ACTIVE URL with future expiration should be active",
			expiresOn: time.Now().Add(24 * time.Hour),
			status:    URLStatusActive,
			expected:  true,
		},
		{
			name:      "ACTIVE URL with past expiration should not be active",
			expiresOn: time.Now().Add(-24 * time.Hour),
			status:    URLStatusActive,
			expected:  false,
		},
		{
			name:      "SUSPENDED URL with future expiration should not be active",
			expiresOn: time.Now().Add(24 * time.Hour),
			status:    URLStatusSuspended,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := createValidURLParams()
			params.ExpiresOn = tt.expiresOn
			params.Status = tt.status
			url, err := NewUrl(params)
			if err != nil {
				t.Fatalf("Failed to create URL: %v", err)
			}
			if got := url.IsActive(); got != tt.expected {
				t.Errorf("URL.IsActive() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func BenchmarkNewUrl(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	params := createValidURLParams()
	for i := 0; i < b.N; i++ {
		_, _ = NewUrl(params)
	}
}

// NewUrl hydrates persisted state, so a past expiry must be accepted: expired URLs still have to be readable.
func TestNewUrl_AllowsPastExpirationWhenHydrating(t *testing.T) {
	params := createValidURLParams()
	params.ExpiresOn = time.Now().Add(-24 * time.Hour)

	u, err := NewUrl(params)
	if err != nil {
		t.Fatalf("NewUrl() with past expiry returned error %v, expected none", err)
	}
	if u.IsActive() {
		t.Errorf("URL with past expiry should not be active")
	}
}
