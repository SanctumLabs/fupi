package ports

import (
	"context"

	"github.com/sanctumlabs/curtz/app/internal/domain/url"
)

type ShortCodeGenerator interface {
	// Next returns the next unique short code.
	// Thread-safe. Claims a new range from PostgreSQL when the local range is exhausted.
	Next(ctx context.Context) (url.ShortCode, error)
}
