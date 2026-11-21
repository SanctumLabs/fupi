package identityapp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// VerifyEmail completes email verification for the given token, activating the user.
//
// An unknown token is reported as an invalid token rather than "not found": to a caller holding a
// bad link, the two are the same thing, and distinguishing them leaks which tokens exist.
func (svc *Service) VerifyEmail(ctx context.Context, token string) (_ identity.User, err error) {
	ctx, span := svc.startUseCase(ctx, "VerifyEmail")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<VerifyEmail>", svc.logPrefix)

	if token == "" {
		return identity.User{}, errdefs.InvalidParameter(errdefs.ErrVerificationTokenInvalid)
	}

	user, fetchErr := svc.users.FetchByVerificationToken(ctx, token)
	if fetchErr != nil {
		if errdefs.IsNotFound(fetchErr) {
			slog.WarnContext(ctx, fmt.Sprintf("%s Verification attempted with an unknown token", handlerLogPrefix))
			return identity.User{}, errdefs.InvalidParameter(errdefs.ErrVerificationTokenInvalid)
		}
		return identity.User{}, fetchErr
	}

	if verifyErr := user.Verify(token, time.Now()); verifyErr != nil {
		slog.WarnContext(ctx, fmt.Sprintf("%s Verification rejected", handlerLogPrefix),
			"userId", entity.IDToString(user.ID()),
			"error", verifyErr,
		)
		return identity.User{}, errdefs.InvalidParameter(verifyErr)
	}

	verifiedUser, markErr := svc.users.MarkVerified(ctx, user)
	if markErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to persist verification", handlerLogPrefix),
			"userId", entity.IDToString(user.ID()),
			"error", markErr,
		)
		return identity.User{}, markErr
	}

	slog.InfoContext(ctx, fmt.Sprintf("%s Verified user", handlerLogPrefix), "userId", entity.IDToString(verifiedUser.ID()))

	return verifiedUser, nil
}
