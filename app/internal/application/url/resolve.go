package urlapp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// Resolve returns the original url a short code or custom alias redirects to.
//
// The code is looked up as a short code first and as a custom alias second. New aliases cannot have a short code's
// shape (ADR-0020), so the two cannot overlap; for aliases stored before that rule, a generated code wins. A url that exists but may not redirect (expired, suspended, deleted) is reported as
// NotFound: to the visitor following the link the two are the same, and the reason is not theirs to know.
func (svc *Service) Resolve(ctx context.Context, code string) (_ url.OriginalURL, err error) {
	ctx, span := svc.startUseCase(ctx, "Resolve")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Resolve>", svc.logPrefix)

	found, fetchErr := svc.urls.FetchByShortCode(ctx, code)
	if errdefs.IsNotFound(fetchErr) {
		found, fetchErr = svc.urls.FetchByCustomAlias(ctx, code)
	}
	if fetchErr != nil {
		return url.OriginalURL{}, fetchErr
	}

	target, redirectErr := found.Redirect()
	if redirectErr != nil {
		slog.InfoContext(ctx, fmt.Sprintf("%s Refusing to redirect", handlerLogPrefix),
			"url_id", entity.IDToString(found.ID()),
			"status", found.Status(),
			"error", redirectErr,
		)
		return url.OriginalURL{}, errdefs.NotFound(redirectErr)
	}

	return target, nil
}
