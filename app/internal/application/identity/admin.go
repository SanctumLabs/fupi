package identityapp

import (
	"context"

	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// IsAdmin reports whether the user is an ACTIVE admin right now. The role is read from the datastore on every call and
// is not carried in the access token, so a demoted or suspended admin loses the privilege at once instead of when the
// token expires (ADR-0022). An unknown user is not an admin.
func (svc *Service) IsAdmin(ctx context.Context, userID string) (_ bool, err error) {
	ctx, span := svc.startUseCase(ctx, "IsAdmin")
	defer func() { endUseCase(span, err) }()

	user, fetchErr := svc.users.FetchById(ctx, userID)
	if fetchErr != nil {
		if errdefs.IsNotFound(fetchErr) {
			return false, nil
		}
		return false, fetchErr
	}

	return user.IsAdmin() && user.Status() == identity.UserStatusActive, nil
}
