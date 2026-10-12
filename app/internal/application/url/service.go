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
