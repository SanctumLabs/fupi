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
	access             *AccessRecorder
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

// WithAccessRecorder makes redirects report URLAccessed through recorder; without one they report nothing.
func (svc *Service) WithAccessRecorder(recorder *AccessRecorder) *Service {
	svc.access = recorder
	return svc
}

// RecordAccess reports a visit to a short url. It never blocks and never fails the redirect.
func (svc *Service) RecordAccess(access Access) { svc.access.Record(access) }
