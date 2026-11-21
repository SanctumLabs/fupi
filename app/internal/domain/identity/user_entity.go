package identity

import (
	"fmt"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
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

	verification, verificationErr := NewUserVerification(params.VerificationToken, params.VerificationExpires, params.Verified)
	if verificationErr != nil {
		return nil, verificationErr
	}

	return &User{
		AggregateRoot: aggregateRoot,
		username:      params.Username,
		fullName:      fullName,
		email:         email,
		verification:  verification,
		status:        params.Status,
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

func (user *User) Email() Email {
	return user.email
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

// Prefix returns the url prefix for logging
func (user *User) Prefix() string {
	return fmt.Sprintf("user-%s-%s", user.ID(), user.username)
}

func (user *User) String() string {
	return fmt.Sprintf("user(id=%s, username: %s)", user.ID(), user.username)
}
