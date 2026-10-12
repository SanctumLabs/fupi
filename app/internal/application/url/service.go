package urlapp

import (
	"time"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Service exposes the Url use cases.
type Service struct {
	urls               url.UrlDatastore
	shortCodeGenerator ports.ShortCodeGenerator
	redirectCache      redirectCache
	tracer             trace.Tracer
	logPrefix          string
}

// NewService wires the Url use cases to their ports.
func NewService(urls url.UrlDatastore, shortCodeGenerator ports.ShortCodeGenerator, opts ...Option) *Service {
	svc := &Service{
		urls:               urls,
		shortCodeGenerator: shortCodeGenerator,
		tracer:             otel.Tracer(instrumentationName),
		logPrefix:          "UrlService",
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// Option customizes a Service.
type Option func(*Service)

// WithRedirectCache makes Resolve read through the cache. A found target is kept for at most ttl, and never past its
// expiry; a code that does not exist is kept for absentTTL. Without this option, or with a cache that fails, Resolve
// goes to the datastore.
func WithRedirectCache(cache ports.RedirectCache, ttl, absentTTL time.Duration) Option {
	return func(svc *Service) {
		svc.redirectCache = redirectCache{cache: cache, ttl: ttl, absentTTL: absentTTL}
	}
}
