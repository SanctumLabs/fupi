package config

import "time"

// RateLimitRule is the budget of one route group for each client: a steady rate of Max requests per Window with a burst
// of Max (GCRA), so a client can spend Max at once and then earns one back every Window/Max. It is not a cap per window:
// any one Window can let through up to about 2×Max−1.
type RateLimitRule struct {
	Max    int
	Window time.Duration
}

// RateLimitSettings configures the HTTP rate limiter.
type RateLimitSettings struct {
	// Enabled turns the limiter off entirely when false.
	Enabled bool
	// Auth, Create, Redirect and Default are the budgets of the route groups the limiter tells apart; Default covers
	// every route in none of the others.
	Auth, Create, Redirect, Default RateLimitRule
	// RedisTimeout bounds one limiter call to Redis.
	RedisTimeout time.Duration
	// RedisRetryAfter is the cooldown after a Redis error: how long the limiter decides requests in memory before asking
	// Redis again. Clients never see it.
	RedisRetryAfter time.Duration
}

// LoadRateLimit reads the rate limiter settings: windows and the retry-after in seconds, the Redis timeout in
// milliseconds.
func LoadRateLimit(lookup Lookup) (RateLimitSettings, error) {
	r := newReader(lookup)
	settings := RateLimitSettings{
		Enabled:         r.boolean("RATE_LIMIT_ENABLED", true),
		Auth:            r.rateLimitRule("AUTH", 10),
		Create:          r.rateLimitRule("CREATE", 30),
		Redirect:        r.rateLimitRule("REDIRECT", 300),
		Default:         r.rateLimitRule("DEFAULT", 100),
		RedisTimeout:    r.units("RATE_LIMIT_REDIS_TIMEOUT_MS", 50, time.Millisecond),
		RedisRetryAfter: r.units("RATE_LIMIT_REDIS_RETRY_AFTER", 5, time.Second),
	}
	if settings.RedisTimeout <= 0 {
		r.fail("RATE_LIMIT_REDIS_TIMEOUT_MS must be greater than zero")
	}
	if settings.RedisRetryAfter <= 0 {
		r.fail("RATE_LIMIT_REDIS_RETRY_AFTER must be greater than zero")
	}
	return settings, r.err()
}

// rateLimitRule reads RATE_LIMIT_<group>_MAX and RATE_LIMIT_<group>_WINDOW (seconds; default one minute).
func (r *reader) rateLimitRule(group string, defaultMax int) RateLimitRule {
	maxKey, windowKey := "RATE_LIMIT_"+group+"_MAX", "RATE_LIMIT_"+group+"_WINDOW"
	rule := RateLimitRule{
		Max:    r.integer(maxKey, defaultMax),
		Window: r.units(windowKey, 60, time.Second),
	}
	if rule.Max < 1 {
		r.fail("%s must be at least 1", maxKey)
	}
	if rule.Window <= 0 {
		r.fail("%s must be greater than zero", windowKey)
	}
	return rule
}
