package identityapp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// Refresh exchanges a valid refresh token for a fresh token pair.
//
// The user is re-read so that a token belonging to a since-deleted or suspended account stops
// working.
func (svc *Service) Refresh(ctx context.Context, refreshToken string) (_ TokenPair, err error) {
	ctx, span := svc.startUseCase(ctx, "Refresh")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Refresh>", svc.logPrefix)

	if refreshToken == "" {
		return TokenPair{}, errdefs.Unauthorized(errdefs.ErrTokenRequired)
	}

	userID, authErr := svc.tokens.Authenticate(refreshToken)
	if authErr != nil {
		slog.WarnContext(ctx, fmt.Sprintf("%s Refresh rejected: invalid token", handlerLogPrefix), "error", authErr)
		return TokenPair{}, errdefs.Unauthorized(errdefs.ErrTokenInvalid)
	}

	user, fetchErr := svc.users.FetchById(ctx, userID)
	if fetchErr != nil {
		if !errdefs.IsNotFound(fetchErr) {
			return TokenPair{}, fetchErr
		}
		slog.WarnContext(ctx, fmt.Sprintf("%s Refresh rejected: user no longer exists", handlerLogPrefix), "userId", userID)
		return TokenPair{}, errdefs.Unauthorized(errdefs.ErrTokenInvalid)
	}

	if signInErr := ensureCanSignIn(ctx, handlerLogPrefix, user); signInErr != nil {
		return TokenPair{}, signInErr
	}

	tokens, tokenErr := svc.issueTokens(userID)
	if tokenErr != nil {
		return TokenPair{}, tokenErr
	}

	return tokens, nil
}
