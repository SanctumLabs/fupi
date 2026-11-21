package identityapp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/utils"
)

// Login authenticates a user by email and password and issues a token pair.
//
// An unknown email and a wrong password both return ErrInvalidCredentials so the response cannot
// be used to discover which addresses are registered.
func (svc *Service) Login(ctx context.Context, email, password string) (identity.User, TokenPair, error) {
	handlerLogPrefix := fmt.Sprintf("%s<Login>", svc.logPrefix)

	user, fetchErr := svc.users.FetchByEmail(ctx, email)
	if fetchErr != nil {
		if !errdefs.IsNotFound(fetchErr) {
			return identity.User{}, TokenPair{}, fetchErr
		}
		slog.WarnContext(ctx, fmt.Sprintf("%s Login attempted for an unknown email", handlerLogPrefix))
		return identity.User{}, TokenPair{}, errdefs.Unauthorized(errdefs.ErrInvalidCredentials)
	}

	if ok, compareErr := utils.CompareHashAndPassword(user.PasswordHash(), password); compareErr != nil || !ok {
		slog.WarnContext(ctx, fmt.Sprintf("%s Login rejected: password mismatch", handlerLogPrefix),
			"userId", entity.IDToString(user.ID()),
		)
		return identity.User{}, TokenPair{}, errdefs.Unauthorized(errdefs.ErrInvalidCredentials)
	}

	if signInErr := ensureCanSignIn(ctx, handlerLogPrefix, user); signInErr != nil {
		return identity.User{}, TokenPair{}, signInErr
	}

	tokens, tokenErr := svc.issueTokens(entity.IDToString(user.ID()))
	if tokenErr != nil {
		return identity.User{}, TokenPair{}, tokenErr
	}

	slog.InfoContext(ctx, fmt.Sprintf("%s Logged in user", handlerLogPrefix), "userId", entity.IDToString(user.ID()))

	return user, tokens, nil
}

// ensureCanSignIn applies the sign-in policy shared by Login and Refresh. A user who has not
// verified their email may still sign in, matching the pre-v2 behaviour. Suspended and deleted
// accounts may not.
func ensureCanSignIn(ctx context.Context, handlerLogPrefix string, user identity.User) error {
	switch user.Status() {
	case identity.UserStatusSuspended, identity.UserStatusDeleted:
		slog.WarnContext(ctx, fmt.Sprintf("%s Rejected: account not permitted to sign in", handlerLogPrefix),
			"userId", entity.IDToString(user.ID()),
			"status", user.Status(),
		)
		return errdefs.Forbidden(fmt.Errorf("account is %s", user.Status()))
	}
	return nil
}
