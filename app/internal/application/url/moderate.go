package urlapp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// Suspend suspends an ACTIVE url for the given reason, whoever owns it: the Security context calls it when a scan flags a
// url. URLSuspended is committed with the status, and the redirect cache forgets the url's codes. A url that is not ACTIVE
// is a Conflict and an unknown one is NotFound; nothing is written in either case.
func (svc *Service) Suspend(ctx context.Context, id string, reason url.SuspensionReason) (err error) {
	ctx, span := svc.startUseCase(ctx, "Suspend")
	defer func() { endUseCase(span, err) }()

	return svc.moderate(ctx, id, "Suspend", func(u *url.URL) error { return u.Suspend(reason) })
}

// Reinstate puts a SUSPENDED url back to ACTIVE, whoever owns it. The caller decides who may do this. URLReinstated is
// committed with the status, and the redirect cache forgets the url's codes. A url that is not SUSPENDED is a Conflict and
// an unknown one is NotFound.
func (svc *Service) Reinstate(ctx context.Context, id string) (err error) {
	ctx, span := svc.startUseCase(ctx, "Reinstate")
	defer func() { endUseCase(span, err) }()

	return svc.moderate(ctx, id, "Reinstate", (*url.URL).Reinstate)
}

// moderate loads the url, applies a status transition of the aggregate, persists it and evicts the redirect cache.
func (svc *Service) moderate(ctx context.Context, id, operation string, transition func(*url.URL) error) error {
	handlerLogPrefix := fmt.Sprintf("%s<%s>", svc.logPrefix, operation)

	if _, idErr := uuid.Parse(id); idErr != nil {
		return errdefs.NotFound(fmt.Errorf("url %q: %w", id, idErr))
	}

	found, fetchErr := svc.urls.FetchById(ctx, id)
	if fetchErr != nil {
		return fetchErr
	}

	if transitionErr := transition(&found); transitionErr != nil {
		return classifyDomainError(transitionErr)
	}

	updated, updateErr := svc.urls.Update(ctx, found)
	if updateErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to update url", handlerLogPrefix), "url_id", id, "error", updateErr)
		return updateErr
	}

	svc.evictRedirects(ctx, &updated)

	slog.InfoContext(ctx, fmt.Sprintf("%s Changed the status of the url", handlerLogPrefix), "url_id", id, "status", updated.Status())

	return nil
}
