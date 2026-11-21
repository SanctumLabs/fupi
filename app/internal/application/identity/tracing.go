package identityapp

import (
	"context"

	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/sanctumlabs/fupi/app/internal/application/identity"

// startUseCase starts the span of one use case, named identity.<useCase>, as a child of the request's span.
func (svc *Service) startUseCase(ctx context.Context, useCase string) (context.Context, trace.Span) {
	return svc.tracer.Start(ctx, "identity."+useCase)
}

// endUseCase finishes the span of a use case. A failure sets the span's status and its error.type to the class of the
// failure and never to its text: the errors in this package embed what the caller sent ("email x is invalid", "token x
// is invalid"), and a span must not carry an email address, a name or a token.
func endUseCase(span trace.Span, err error) {
	if err != nil {
		class := errorClass(err)
		span.SetAttributes(attribute.String("error.type", class))
		span.SetStatus(codes.Error, class)
	}
	span.End()
}

// errorClass names the kind of an error with the same classes the HTTP layer maps to status codes.
func errorClass(err error) string {
	switch {
	case errdefs.IsInvalidParameter(err):
		return "invalid_parameter"
	case errdefs.IsUnauthorized(err):
		return "unauthorized"
	case errdefs.IsForbidden(err):
		return "forbidden"
	case errdefs.IsNotFound(err):
		return "not_found"
	case errdefs.IsConflict(err):
		return "conflict"
	case errdefs.IsUnavailable(err):
		return "unavailable"
	default:
		return "internal"
	}
}
