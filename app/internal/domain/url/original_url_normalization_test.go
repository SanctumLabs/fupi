package url

import "testing"

// Normalization exists so that the global uniqueness rule on urls.original_url cannot be bypassed by
// spelling the same target differently. Every case below is a spelling that must collapse onto the
// same stored value.
var originalURLNormalizationCases = []struct {
	name  string
	input string
	want  string
}{
	{
		name:  "host is lowercased",
		input: "https://EXAMPLE.COM/path",
		want:  "https://example.com/path",
	},
	{
		name:  "scheme is lowercased",
		input: "HTTPS://example.com/path",
		want:  "https://example.com/path",
	},
	{
		name:  "default https port is stripped",
		input: "https://example.com:443/path",
		want:  "https://example.com/path",
	},
	{
		name:  "default http port is stripped",
		input: "http://example.com:80/path",
		want:  "http://example.com/path",
	},
	{
		name:  "non default port is kept",
		input: "https://example.com:8080/path",
		want:  "https://example.com:8080/path",
	},
	{
		name:  "trailing slash is stripped",
		input: "https://example.com/path/",
		want:  "https://example.com/path",
	},
	{
		name:  "bare trailing slash is stripped",
		input: "https://example.com/",
		want:  "https://example.com",
	},
	{
		name:  "utm parameters are dropped",
		input: "https://example.com/path?utm_source=news&utm_medium=email",
		want:  "https://example.com/path",
	},
	{
		name:  "utm parameters are dropped but others are kept",
		input: "https://example.com/path?id=7&utm_campaign=spring",
		want:  "https://example.com/path?id=7",
	},
	{
		name:  "non utm query is left untouched",
		input: "https://example.com/path?param1=value1&param2=value2",
		want:  "https://example.com/path?param1=value1&param2=value2",
	},
	{
		name:  "already canonical url is unchanged",
		input: "https://example.com/path/to/resource",
		want:  "https://example.com/path/to/resource",
	},
	{
		name:  "url without a scheme is left alone",
		input: "example.com/path",
		want:  "example.com/path",
	},
	{
		name:  "several rules at once",
		input: "HTTPS://EXAMPLE.COM:443/path/?utm_source=news",
		want:  "https://example.com/path",
	},
}

func TestNewOriginalURL_Normalizes(t *testing.T) {
	for _, tc := range originalURLNormalizationCases {
		t.Run(tc.name, func(t *testing.T) {
			originalURL, err := NewOriginalURL(tc.input)
			if err != nil {
				t.Fatalf("NewOriginalURL(%q) returned an unexpected error: %v", tc.input, err)
			}

			if originalURL.Value() != tc.want {
				t.Errorf("NewOriginalURL(%q).Value() = %q, want %q", tc.input, originalURL.Value(), tc.want)
			}
		})
	}
}

// TestNewOriginalURL_NormalizationIsIdempotent matters because stored URLs are mapped back through
// NewOriginalURL when a row is read: a second pass must not keep rewriting the value.
func TestNewOriginalURL_NormalizationIsIdempotent(t *testing.T) {
	for _, tc := range originalURLNormalizationCases {
		t.Run(tc.name, func(t *testing.T) {
			once, err := NewOriginalURL(tc.input)
			if err != nil {
				t.Fatalf("NewOriginalURL(%q) returned an unexpected error: %v", tc.input, err)
			}

			twice, err := NewOriginalURL(once.Value())
			if err != nil {
				t.Fatalf("NewOriginalURL(%q) returned an unexpected error on the second pass: %v", once.Value(), err)
			}

			if twice.Value() != once.Value() {
				t.Errorf("normalizing %q twice gave %q, want %q", tc.input, twice.Value(), once.Value())
			}
		})
	}
}

// TestNewOriginalURL_DifferentSpellingsCollide is the property the unique index depends on.
func TestNewOriginalURL_DifferentSpellingsCollide(t *testing.T) {
	spellings := []string{
		"https://example.com/path",
		"https://EXAMPLE.com/path",
		"https://example.com:443/path",
		"https://example.com/path/",
		"https://example.com/path?utm_source=news",
		"HTTPS://Example.Com:443/path/?utm_medium=email",
	}

	first, err := NewOriginalURL(spellings[0])
	if err != nil {
		t.Fatalf("NewOriginalURL(%q) returned an unexpected error: %v", spellings[0], err)
	}

	for _, spelling := range spellings[1:] {
		other, otherErr := NewOriginalURL(spelling)
		if otherErr != nil {
			t.Fatalf("NewOriginalURL(%q) returned an unexpected error: %v", spelling, otherErr)
		}

		if other.Value() != first.Value() {
			t.Errorf("NewOriginalURL(%q).Value() = %q, want it to match %q", spelling, other.Value(), first.Value())
		}
	}
}

// TestNewOriginalURL_FilterAppliesAfterNormalization stops normalization from opening a hole in the
// filter: an uppercase or default-port spelling of a blocked host must still be rejected.
func TestNewOriginalURL_FilterAppliesAfterNormalization(t *testing.T) {
	blocked := []string{
		"http://LOCALHOST:8080/path",
		"https://URLSSH.xyz/abc123",
		"http://127.0.0.1:80/path",
	}

	for _, input := range blocked {
		t.Run(input, func(t *testing.T) {
			if _, err := NewOriginalURL(input); err == nil {
				t.Errorf("NewOriginalURL(%q) succeeded, want it rejected", input)
			}
		})
	}
}
