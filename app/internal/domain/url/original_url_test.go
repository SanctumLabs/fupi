package url

import (
	"errors"
	"testing"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

type originalURLTestCase struct {
	name  string
	input string
	err   error
}

var originalURLTestCases = []originalURLTestCase{
	{
		name:  "empty URL should return error",
		input: "",
		err:   errdefs.ErrInvalidURLLen,
	},
	{
		name:  "URL too short should return error",
		input: "http://a.b",
		err:   errdefs.ErrInvalidURLLen,
	},
	{
		name:  "URL too long should return error",
		input: "http://example.com/" + string(make([]byte, 2049)),
		err:   errdefs.ErrInvalidURLLen,
	},
	{
		name:  "invalid URL format should return error",
		input: "not-a-url",
		err:   errdefs.ErrInvalidURLLen, // too short
	},
	{
		name:  "URL without protocol should return no error", // protocol is optional in regex
		input: "example.com/path",
		err:   nil,
	},
	{
		name:  "valid HTTP URL should return no error",
		input: "http://example.com/path/to/resource",
		err:   nil,
	},
	{
		name:  "valid HTTPS URL should return no error",
		input: "https://example.com/path/to/resource",
		err:   nil,
	},
	{
		name:  "valid FTP URL should return no error",
		input: "ftp://example.com/path/to/resource",
		err:   nil,
	},
	{
		name:  "URL with query parameters should return no error",
		input: "https://example.com/path?param1=value1&param2=value2",
		err:   nil,
	},
	{
		name:  "URL with port should return no error",
		input: "https://example.com:8080/path",
		err:   nil,
	},
	{
		name:  "URL with IP address should return error", // regex may not support this format
		input: "http://192.168.1.1/path",
		err:   errdefs.ErrInvalidURL,
	},
	{
		name:  "URL with subdomain should return no error",
		input: "https://sub.example.com/path",
		err:   nil,
	},
}

func TestNewOriginalURL(t *testing.T) {
	for _, tc := range originalURLTestCases {
		t.Run(tc.name, func(t *testing.T) {
			originalURL, err := NewOriginalURL(tc.input)
			if !errors.Is(err, tc.err) {
				t.Errorf("NewOriginalURL(%s) = (%v, %v), expected error %v, got %v", tc.input, originalURL, err, tc.err, err)
			}
			if err == nil && originalURL.Value() != tc.input {
				t.Errorf("NewOriginalURL(%s) = %s, expected %s", tc.input, originalURL.Value(), tc.input)
			}
		})
	}
}

func BenchmarkNewOriginalURL(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range originalURLTestCases {
			_, _ = NewOriginalURL(tc.input)
		}
	}
}
