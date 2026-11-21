package mockidentity

import (
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/identity"
	timeutils "github.com/sanctumlabs/curtz/app/pkg/utils/time"
)

type MockUserOption func(*identity.UserParams)

func MockUser(mockUrlOption ...MockUserOption) (*identity.User, error) {
	id := faker.UUIDHyphenated()
	userName := faker.Username()
	firstName := faker.FirstName()
	lastName := faker.LastName()
	email := faker.Email()

	status := identity.UserStatusActive

	verificationToken := faker.UUIDHyphenated()
	var verificationExpires time.Time
	expiresOnTimestamp := faker.Timestamp()
	if parsedExpiresOn, expiresOnTimestampErr := timeutils.ParseHumanFriendlyDate(expiresOnTimestamp); expiresOnTimestampErr != nil {
		verificationExpires = time.Now().Add(time.Hour * 24)
	} else {
		verificationExpires = parsedExpiresOn
	}
	verified := true

	var userId entity.ID
	if generatedUrlId, generatedIdErr := entity.StringToID(id); generatedIdErr != nil {
		userId = entity.NewID()
	} else {
		userId = generatedUrlId
	}

	params := identity.UserParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{
				EntityIDParams: entity.EntityIDParams{
					ID: userId,
				},
				EntityTimestampParams: entity.EntityTimestampParams{
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
					DeletedAt: nil,
				},
				Metadata: nil,
			},
			DomainEvents: nil,
		},
		Username:            userName,
		FirstName:           firstName,
		LastName:            lastName,
		Email:               email,
		VerificationToken:   verificationToken,
		VerificationExpires: verificationExpires,
		Verified:            verified,
		Status:              status,
	}

	for _, option := range mockUrlOption {
		option(&params)
	}

	return identity.NewUser(params)
}

// WithId updates the id of the user entity
func WithId(id entity.ID) MockUserOption {
	return func(r *identity.UserParams) {
		r.ID = id
	}
}

// WithMetadata updates the metadata of the url entity
func WithMetadata(metadata map[string]any) MockUserOption {
	return func(r *identity.UserParams) {
		r.Metadata = metadata
	}
}

// WithDomainEvents updates the domain events of the url entity
func WithDomainEvents(domainEvents []entity.DomainEvent) MockUserOption {
	return func(r *identity.UserParams) {
		r.DomainEvents = domainEvents
	}
}

// WithCreatedTime updates the created at time of the url entity
func WithCreatedTime(createdAt time.Time) MockUserOption {
	return func(r *identity.UserParams) {
		r.CreatedAt = createdAt
	}
}

// WithUpdatedTime updates the updated at time of the url entity
func WithUpdatedTime(updatedAt time.Time) MockUserOption {
	return func(r *identity.UserParams) {
		r.UpdatedAt = updatedAt
	}
}

// WithDeletedTime updates the deleted at time of the url entity
func WithDeletedTime(deletedAt *time.Time) MockUserOption {
	return func(r *identity.UserParams) {
		r.DeletedAt = deletedAt
	}
}

func WithUsername(username string) MockUserOption {
	return func(r *identity.UserParams) {
		r.Username = username
	}
}

func WithFirstName(firstName string) MockUserOption {
	return func(r *identity.UserParams) {
		r.FirstName = firstName
	}
}

func WithLastName(lastName string) MockUserOption {
	return func(r *identity.UserParams) {
		r.LastName = lastName
	}
}

func WithEmail(email string) MockUserOption {
	return func(r *identity.UserParams) {
		r.Email = email
	}
}

func WithStatus(status identity.UserStatus) MockUserOption {
	return func(u *identity.UserParams) {
		u.Status = status
	}
}
func WithVerificationToken(verificationToken string) MockUserOption {
	return func(u *identity.UserParams) {
		u.VerificationToken = verificationToken
	}
}

func WithVerificationExpires(verificationExpires time.Time) MockUserOption {
	return func(u *identity.UserParams) {
		u.VerificationExpires = verificationExpires
	}
}

func WithVerified(verified bool) MockUserOption {
	return func(r *identity.UserParams) {
		r.Verified = verified
	}
}
