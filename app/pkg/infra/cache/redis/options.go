package redis

type Option func(*redisClient)

func WithConnAttempts(attempts int) Option {
	return func(r *redisClient) {
		r.connAttempts = attempts
	}
}

func StatsEnabled(enabled bool) Option {
	return func(r *redisClient) {
		r.statsEnabled = enabled
	}
}
func MarshalFunc(fn func(any) ([]byte, error)) Option {
	return func(r *redisClient) {
		r.marshalFunc = fn
	}
}
func UnmarshalFunc(fn func([]byte, any) error) Option {
	return func(r *redisClient) {
		r.unmarshalFunc = fn
	}
}
