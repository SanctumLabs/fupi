package urlapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/core/ports/repository"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/pkg/common"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// UpdateCommand carries the changes a user may make to one of their urls. A nil field is left as it is. The custom
// alias is not here: once assigned it is immutable.
type UpdateCommand struct {
	UserID    string
	ID        string
	ExpiresOn *time.Time
	Keywords  *[]string
}

// Get returns one of the caller's urls.
func (svc *Service) Get(ctx context.Context, userID, id string) (_ url.URL, err error) {
	ctx, span := svc.startUseCase(ctx, "Get")
	defer func() { endUseCase(span, err) }()

	return svc.fetchOwned(ctx, userID, id)
}

// List returns a page of the caller's urls, deleted ones excluded whatever params ask for.
func (svc *Service) List(ctx context.Context, userID string, params common.RequestParams) (_ repository.FetchRecordsResponse[url.URL], err error) {
	ctx, span := svc.startUseCase(ctx, "List")
	defer func() { endUseCase(span, err) }()

	params.IncludeDeleted = false

	return svc.urls.FetchByUserId(ctx, userID, params)
}

// Update changes the expiry and/or the keywords of one of the caller's urls.
func (svc *Service) Update(ctx context.Context, cmd UpdateCommand) (_ url.URL, err error) {
	ctx, span := svc.startUseCase(ctx, "Update")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Update>", svc.logPrefix)

	owned, fetchErr := svc.fetchOwned(ctx, cmd.UserID, cmd.ID)
	if fetchErr != nil {
		return url.URL{}, fetchErr
	}

	if cmd.ExpiresOn != nil {
		if expiryErr := owned.UpdateExpiry(*cmd.ExpiresOn); expiryErr != nil {
			return url.URL{}, classifyDomainError(expiryErr)
		}
	}
	if cmd.Keywords != nil {
		if keywordsErr := owned.SetKeywords(*cmd.Keywords); keywordsErr != nil {
			return url.URL{}, classifyDomainError(keywordsErr)
		}
	}

	updated, updateErr := svc.urls.Update(ctx, owned)
	if updateErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to update url", handlerLogPrefix), "url_id", cmd.ID, "error", updateErr)
		return url.URL{}, updateErr
	}

	slog.InfoContext(ctx, fmt.Sprintf("%s Updated url", handlerLogPrefix), "url_id", cmd.ID)

	return updated, nil
}

// Delete soft-deletes one of the caller's urls: it stops redirecting, its target can be shortened again, and URLDeleted
// is committed with it.
func (svc *Service) Delete(ctx context.Context, userID, id string) (err error) {
	ctx, span := svc.startUseCase(ctx, "Delete")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Delete>", svc.logPrefix)

	owned, fetchErr := svc.fetchOwned(ctx, userID, id)
	if fetchErr != nil {
		return fetchErr
	}

	if deleteErr := owned.MarkDeleted(); deleteErr != nil {
		return classifyDomainError(deleteErr)
	}

	if _, updateErr := svc.urls.Update(ctx, owned); updateErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to delete url", handlerLogPrefix), "url_id", id, "error", updateErr)
		return updateErr
	}

	slog.InfoContext(ctx, fmt.Sprintf("%s Deleted url", handlerLogPrefix), "url_id", id)

	return nil
}

// fetchOwned returns the url with the given id if it belongs to userID and is not deleted. Any other url is reported as
// NotFound, so a caller cannot probe which ids exist.
func (svc *Service) fetchOwned(ctx context.Context, userID, id string) (url.URL, error) {
	// Parsed here rather than with entity.StringToID, which logs every failure as an error: a malformed id in a request
	// path is the caller's mistake, not ours.
	if _, idErr := uuid.Parse(id); idErr != nil {
		return url.URL{}, errdefs.NotFound(fmt.Errorf("url %q: %w", id, idErr))
	}

	found, fetchErr := svc.urls.FetchById(ctx, id)
	if fetchErr != nil {
		return url.URL{}, fetchErr
	}

	owner, ownerErr := uuid.Parse(userID)
	if ownerErr != nil || entity.ID(owner) != found.UserId() || found.Status() == url.URLStatusDeleted {
		return url.URL{}, errdefs.NotFound(fmt.Errorf("url %s not found", id))
	}

	return found, nil
}

// classifyDomainError maps a rejection by the url aggregate onto an error class: a change the url's current status
// forbids is a Conflict, anything else is bad caller input.
func classifyDomainError(err error) error {
	if errors.Is(err, errdefs.ErrURLNotActive) || errors.Is(err, errdefs.ErrInvalidStatusTransition) {
		return errdefs.Conflict(err)
	}
	return errdefs.InvalidParameter(err)
}
