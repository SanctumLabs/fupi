package urlapp

import (
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
	tracer             trace.Tracer
	logPrefix          string
}

// NewService wires the Url use cases to their ports.
func NewService(urls url.UrlDatastore, shortCodeGenerator ports.ShortCodeGenerator) *Service {
	return &Service{
		urls:               urls,
		shortCodeGenerator: shortCodeGenerator,
		tracer:             otel.Tracer(instrumentationName),
		logPrefix:          "UrlService",
	}
}

// WithAccessRecorder makes redirects report URLAccessed through recorder; without one they report nothing.
func (svc *Service) WithAccessRecorder(recorder *AccessRecorder) *Service {
	svc.access = recorder
	return svc
}

// RecordAccess reports a visit to a short url. It never blocks and never fails the redirect.
func (svc *Service) RecordAccess(access Access) { svc.access.Record(access) }
