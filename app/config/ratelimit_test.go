package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRateLimit_Defaults(t *testing.T) {
	settings, err := LoadRateLimit(lookupOf(nil))
	require.NoError(t, err)

	assert.Equal(t, RateLimitSettings{
		Enabled:         true,
		Auth:            RateLimitRule{Max: 10, Window: 60 * time.Second},
		Create:          RateLimitRule{Max: 30, Window: 60 * time.Second},
		Redirect:        RateLimitRule{Max: 300, Window: 60 * time.Second},
		Default:         RateLimitRule{Max: 100, Window: 60 * time.Second},
		RedisTimeout:    50 * time.Millisecond,
		RedisRetryAfter: 5 * time.Second,
	}, settings)
}

func TestLoadRateLimit_Overrides(t *testing.T) {
	settings, err := LoadRateLimit(lookupOf(map[string]string{
		"RATE_LIMIT_ENABLED":           "false",
		"RATE_LIMIT_AUTH_MAX":          "1",
		"RATE_LIMIT_AUTH_WINDOW":       "2",
		"RATE_LIMIT_CREATE_MAX":        "3",
		"RATE_LIMIT_CREATE_WINDOW":     "4",
		"RATE_LIMIT_REDIRECT_MAX":      "5",
		"RATE_LIMIT_REDIRECT_WINDOW":   "6",
		"RATE_LIMIT_DEFAULT_MAX":       "7",
		"RATE_LIMIT_DEFAULT_WINDOW":    "8",
		"RATE_LIMIT_REDIS_TIMEOUT_MS":  "9",
		"RATE_LIMIT_REDIS_RETRY_AFTER": "10",
	}))
	require.NoError(t, err)

	assert.Equal(t, RateLimitSettings{
		Enabled:         false,
		Auth:            RateLimitRule{Max: 1, Window: 2 * time.Second},
		Create:          RateLimitRule{Max: 3, Window: 4 * time.Second},
		Redirect:        RateLimitRule{Max: 5, Window: 6 * time.Second},
		Default:         RateLimitRule{Max: 7, Window: 8 * time.Second},
		RedisTimeout:    9 * time.Millisecond,
		RedisRetryAfter: 10 * time.Second,
	}, settings)
}

func TestLoadRateLimit_RejectsInvalidValues(t *testing.T) {
	cases := map[string]struct {
		key, value string
	}{
		"auth max zero":          {"RATE_LIMIT_AUTH_MAX", "0"},
		"create max negative":    {"RATE_LIMIT_CREATE_MAX", "-1"},
		"redirect max zero":      {"RATE_LIMIT_REDIRECT_MAX", "0"},
		"default max negative":   {"RATE_LIMIT_DEFAULT_MAX", "-1"},
		"auth window zero":       {"RATE_LIMIT_AUTH_WINDOW", "0"},
		"create window zero":     {"RATE_LIMIT_CREATE_WINDOW", "0"},
		"redirect window zero":   {"RATE_LIMIT_REDIRECT_WINDOW", "0"},
		"default window zero":    {"RATE_LIMIT_DEFAULT_WINDOW", "0"},
		"redis timeout zero":     {"RATE_LIMIT_REDIS_TIMEOUT_MS", "0"},
		"redis retry after zero": {"RATE_LIMIT_REDIS_RETRY_AFTER", "0"},
		"enabled not a boolean":  {"RATE_LIMIT_ENABLED", "maybe"},
		"max not a number":       {"RATE_LIMIT_AUTH_MAX", "many"},
		"window not a number":    {"RATE_LIMIT_AUTH_WINDOW", "1m"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := LoadRateLimit(lookupOf(map[string]string{tc.key: tc.value}))

			require.Error(t, err)
			assert.ErrorContains(t, err, tc.key)
		})
	}
}
