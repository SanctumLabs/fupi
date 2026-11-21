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

// RegisterCommand carries the inputs for registering a new user. Password is plaintext here and
// nowhere else: it is hashed before the domain or the datastore ever see it.
type RegisterCommand struct {
	Username  string
	FirstName string
	LastName  string
	Email     string
	Password  string
}

// Register creates a new user, persists them, and sends a verification email.
//
// The user is created INACTIVE and becomes ACTIVE only once VerifyEmail succeeds.
func (svc *Service) Register(ctx context.Context, cmd RegisterCommand) (identity.User, error) {
	handlerLogPrefix := fmt.Sprintf("%s<Register>", svc.logPrefix)

	passwordHash, hashErr := utils.HashPassword(cmd.Password)
	if hashErr != nil {
		return identity.User{}, errdefs.InvalidParameter(hashErr)
	}

	user, registerErr := identity.Register(identity.RegisterUserParams{
		Username:     cmd.Username,
		FirstName:    cmd.FirstName,
		LastName:     cmd.LastName,
		Email:        cmd.Email,
		PasswordHash: passwordHash,
	})
	if registerErr != nil {
		// Everything Register rejects is bad caller input (malformed email, missing first name),
		// so it is classified here rather than surfacing as an unexplained 500.
		return identity.User{}, errdefs.InvalidParameter(registerErr)
	}

	savedUser, saveErr := svc.users.Save(ctx, *user)
	if saveErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Failed to save user", handlerLogPrefix),
			"username", cmd.Username,
			"error", saveErr,
		)
		return identity.User{}, saveErr
	}

	verification := user.Verification()

	// The user is already committed, so a failure to deliver the email must not fail the
	// registration: reporting failure would tell the caller registration did not happen when it
	// did. Once the transactional outbox relay lands, the UserRegistered event recorded on the
	// aggregate drives delivery with retries and this direct call goes away.
	if notifyErr := svc.notifier.SendEmailVerification(ctx, cmd.Email, verification.Token()); notifyErr != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("%s Registered user but failed to send verification email", handlerLogPrefix),
			"userId", entity.IDToString(savedUser.ID()),
			"error", notifyErr,
		)
	}

	slog.InfoContext(ctx, fmt.Sprintf("%s Registered user", handlerLogPrefix), "userId", entity.IDToString(savedUser.ID()))

	return savedUser, nil
}
