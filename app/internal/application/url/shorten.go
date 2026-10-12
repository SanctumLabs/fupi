package urlapp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// ShortenCommand carries the inputs for shortening a url on behalf of UserID.
type ShortenCommand struct {
	UserID      string
	OriginalURL string
	CustomAlias string
	ExpiresOn   time.Time
	Keywords    []string
}

// Shorten creates a new ACTIVE short url. Its URLCreated event is committed with it.
func (svc *Service) Shorten(ctx context.Context, cmd ShortenCommand) (_ url.URL, err error) {
	ctx, span := svc.startUseCase(ctx, "Shorten")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Shorten>", svc.logPrefix)

	shortCode, codeErr := svc.shortCodeGenerator.Next(ctx)
	if codeErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to generate a short code", handlerLogPrefix), "error", codeErr)
		return url.URL{}, codeErr
	}

	created, createErr := url.Create(url.CreateURLParams{
		UserId:      cmd.UserID,
		ShortCode:   shortCode,
		CustomAlias: cmd.CustomAlias,
		OriginalUrl: cmd.OriginalURL,
		ExpiresOn:   cmd.ExpiresOn,
		Keywords:    cmd.Keywords,
	})
	if createErr != nil {
		// Everything Create rejects is bad caller input (malformed url, past expiry, invalid alias or keyword).
		return url.URL{}, errdefs.InvalidParameter(createErr)
	}

	saved, saveErr := svc.urls.Save(ctx, *created)
	if saveErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to save url", handlerLogPrefix), "short_code", shortCode.Value(), "error", saveErr)
		return url.URL{}, saveErr
	}

	slog.InfoContext(ctx, fmt.Sprintf("%s Shortened url", handlerLogPrefix), "url_id", entity.IDToString(saved.ID()), "short_code", shortCode.Value())

	return saved, nil
}
