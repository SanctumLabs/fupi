package identity

import (
	"fmt"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

type (
	// User is the aggregate root for the User bounded context.
	User struct {
		entity.AggregateRoot

		// username is the user's chosen unique username
		username string

		// fullName is the user's full name, which may include first and last names
		fullName UserFullName

		// email is the user's email address
		email Email

		// status is the status of the user
		status UserStatus

		// verification contains the user verification details
		verification UserVerification

		// PasswordHash is the hashed password of the user
		passwordHash string
	}

	// UserParams represents the parameters for creating or updating a user
	UserParams struct {
		entity.AggregateRootParams

		// Username is the user's chosen username
		Username string

		// FirstName is the user's first name
		FirstName string

		// LastName is the user's last name
		LastName string

		// Email is a list of user's email
		Email string

		// Status is the stats of the user
		Status UserStatus

		// VerificationToken is the user's verification token
		VerificationToken string

		// VerificationExpires is the expiration date for the token sent to the user
		VerificationExpires time.Time

		// Verified is a flag indicating whether the user is verified
		Verified bool

		// PasswordHash is the hashed password of the user
		PasswordHash string
	}

	// RegisterUserParams are the inputs needed to register a brand new user. The password must
	// already be hashed: the domain never sees a plaintext password.
	RegisterUserParams struct {
		Username     string
		FirstName    string
		LastName     string
		Email        string
		PasswordHash string
		Metadata     map[string]any
	}
)

// NewUser creates a new User entity
func NewUser(params UserParams) (*User, error) {
	fullName, nameErr := NewUserFullName(params.FirstName, params.LastName)
	if nameErr != nil {
		return nil, nameErr
	}

	email, emailErr := NewEmail(params.Email)
	if emailErr != nil {
		return nil, emailErr
	}

	aggregateRoot, aggregateErr := entity.NewAggregateRoot(params.AggregateRootParams)
	if aggregateErr != nil {
		return nil, aggregateErr
	}

	var verification UserVerification
	if params.VerificationToken != "" {
		userVerification, verificationErr := NewUserVerification(params.VerificationToken, params.VerificationExpires, params.Verified)
		if verificationErr != nil {
			return nil, verificationErr
		}
		verification = userVerification
	} else {
		verification = UserVerification{
			verificationToken:   "",
			verificationExpires: time.Time{},
			verified:            false,
		}
	}

	return &User{
		AggregateRoot: aggregateRoot,
		username:      params.Username,
		fullName:      fullName,
		email:         email,
		verification:  verification,
		status:        params.Status,
		passwordHash:  params.PasswordHash,
	}, nil
}

// Username returns the user's username
func (user *User) Username() string {
	return user.username
}

func (user *User) FullName() UserFullName {
	return user.fullName
}

func (user *User) FirstName() string {
	return user.fullName.FirstName()
}

func (user *User) LastName() string {
	return user.fullName.LastName()
}

// WithFullName returns a copy of User with the full name updated, leaving the receiver unchanged.
func (user User) WithFullName(fullName UserFullName) User {
	user.fullName = fullName
	return user
}

func (user *User) Email() Email {
	return user.email
}

// WithEmail returns a copy of User with the email updated, leaving the receiver unchanged.
func (user User) WithEmail(email Email) User {
	user.email = email
	return user
}

func (user *User) Status() UserStatus {
	return user.status
}

func (user *User) Verification() UserVerification {
	return user.verification
}

func (user *User) PasswordHash() string {
	return user.passwordHash
}

// WithPasswordHash returns a copy of User with the password hash updated, leaving the receiver
// unchanged.
func (user User) WithPasswordHash(passwordHash string) User {
	user.passwordHash = passwordHash
	return user
}

// Register creates a new INACTIVE user, assigns its identity and timestamps, issues a
// verification token, and records UserRegistered. A user becomes ACTIVE only via Verify.
func Register(params RegisterUserParams) (*User, error) {
	token, tokenErr := NewVerificationToken()
	if tokenErr != nil {
		return nil, tokenErr
	}

	now := time.Now()
	user, err := NewUser(UserParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{
				EntityIDParams: entity.EntityIDParams{
					ID:    entity.NewID(),
					KeyID: entity.NewKeyID(),
				},
				EntityTimestampParams: entity.EntityTimestampParams{
					CreatedAt: now,
					UpdatedAt: now,
				},
				Metadata: params.Metadata,
			},
		},
		Username:            params.Username,
		FirstName:           params.FirstName,
		LastName:            params.LastName,
		Email:               params.Email,
		PasswordHash:        params.PasswordHash,
		Status:              UserStatusInactive,
		VerificationToken:   token,
		VerificationExpires: now.Add(VerificationTokenTTL),
		Verified:            false,
	})
	if err != nil {
		return nil, err
	}

	user.ApplyDomain(UserRegistered{
		baseEvent:           newBaseEvent("user.registered"),
		UserID:              entity.IDToString(user.ID()),
		Username:            user.username,
		Email:               user.email.Value(),
		VerificationToken:   token,
		VerificationExpires: user.verification.Expires(),
	})

	return user, nil
}

// Verify completes email verification with the given token and activates the user,
// recording UserVerified. The token must match and must not have expired.
func (user *User) Verify(token string, now time.Time) error {
	if user.verification.Verified() {
		return errdefs.ErrUserAlreadyVerified
	}
	if user.verification.Token() == "" || user.verification.Token() != token {
		return errdefs.ErrVerificationTokenInvalid
	}
	if user.verification.IsExpired(now) {
		return errdefs.ErrVerificationTokenExpired
	}

	user.verification.verified = true
	user.status = UserStatusActive
	user.touch()

	user.ApplyDomain(UserVerified{
		baseEvent: newBaseEvent("user.verified"),
		UserID:    entity.IDToString(user.ID()),
		Email:     user.email.Value(),
	})

	return nil
}

// MarkDeleted transitions any non-deleted user to DELETED and records UserDeleted.
func (user *User) MarkDeleted() error {
	if err := user.transition(UserStatusDeleted, UserStatusActive, UserStatusInactive, UserStatusSuspended); err != nil {
		return err
	}

	user.ApplyDomain(UserDeleted{
		baseEvent: newBaseEvent("user.deleted"),
		UserID:    entity.IDToString(user.ID()),
	})

	return nil
}

// IsActive reports whether the user is verified and in the ACTIVE state.
func (user *User) IsActive() bool {
	return user.status == UserStatusActive && user.verification.Verified()
}

// transition moves the user to `to` if its current status is one of `from`.
func (user *User) transition(to UserStatus, from ...UserStatus) error {
	for _, f := range from {
		if user.status == f {
			user.status = to
			user.touch()
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", errdefs.ErrInvalidUserStatusTransition, user.status, to)
}

func (user *User) touch() {
	user.EntityTimestamp = user.EntityTimestamp.WithUpdatedAt(time.Now())
}

// Prefix returns the url prefix for logging
func (user *User) Prefix() string {
	return fmt.Sprintf("user-%s", user.ID())
}

func (user *User) String() string {
	return fmt.Sprintf("User(id=%s)", user.ID())
}
