package url

import (
	"errors"
	"strings"
	"testing"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

type keywordTestCase struct {
	name  string
	input string
	err   error
}

var keywordTestCases = []keywordTestCase{
	{
		name:  "empty keyword should return nil and error",
		input: "",
		err:   errdefs.ErrKeywordLength,
	},
	{
		name:  "invalid keyword should return nil and error",
		input: "`'/?.;,<]{}",
		err:   errdefs.ErrInvalidKeyword,
	},
	{
		name:  "keyword longer than 100 characters should return error",
		input: strings.Repeat("a", 101),
		err:   errdefs.ErrKeywordLength,
	},
	{
		name:  "keyword of 100 characters should return keyword and nil error",
		input: strings.Repeat("a", 100),
		err:   nil,
	},
	{
		name:  "valid keyword should return keyword and nil error",
		input: "Social",
		err:   nil,
	},
}

func TestNewKeyword(t *testing.T) {
	for _, tc := range keywordTestCases {
		t.Run(tc.name, func(t *testing.T) {
			keyword, err := NewKeyword(tc.input)
			if !errors.Is(err, tc.err) {
				t.Errorf("NewKeyword(%s) = (%v, %v), expected error %v, got %v", tc.input, keyword, err, tc.err, err)
			}
		})
	}
}

func BenchmarkNewKeyword(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	for b.Loop() {
		for _, tc := range keywordTestCases {
			_, _ = NewKeyword(tc.input)
		}
	}
}

type createKeywordsTestCase struct {
	name  string
	input []string
	err   error
}

var createKeywordsTestCases = []createKeywordsTestCase{
	{
		name:  "empty keywords should return empty keywords and nil error",
		input: []string{},
		err:   nil,
	},
	{
		name:  "More than 10 keywords should return nil and error",
		input: []string{"Social", "Media", "Entertainment", "Sports", "Technology", "Politics", "Business", "Science", "Health", "Travel", "Food", "Fashion"},
		err:   errdefs.ErrKeywordsCount,
	},
	{
		name:  "valid keyword length return keywords and nil error",
		input: []string{"Social", "Media", "Entertainment", "Sports", "Technology", "Politics", "Business", "Science", "Health", "Travel"},
		err:   nil,
	},
	{
		name:  "an invalid keyword in the list should return error",
		input: []string{"Social", "bad keyword"},
		err:   errdefs.ErrInvalidKeyword,
	},
}

func TestCreateKeywords(t *testing.T) {
	for _, tc := range createKeywordsTestCases {
		t.Run(tc.name, func(t *testing.T) {
			keywords, err := createKeywords(tc.input)
			if !errors.Is(err, tc.err) {
				t.Errorf("createKeywords(%s) = (%v, %v), expected error %v, got %v", tc.input, keywords, err, tc.err, err)
			}
			if err == nil && len(keywords) != len(tc.input) {
				t.Errorf("createKeywords(%s) returned %d keywords, expected %d", tc.input, len(keywords), len(tc.input))
			}
		})
	}
}

func BenchmarkCreateKeywords(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	for i := 0; i < b.N; i++ {
		for _, tc := range createKeywordsTestCases {
			_, _ = createKeywords(tc.input)
		}
	}
}
