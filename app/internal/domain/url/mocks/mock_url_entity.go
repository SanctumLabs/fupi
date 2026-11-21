package urlmock

import (
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/go-faker/faker/v4/pkg/options"
	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/internal/domain/url"
	timeutils "github.com/sanctumlabs/curtz/app/pkg/utils/time"
)

type MockUrlOption func(*url.URLParams)

func MockUrl(mockUrlOption ...MockUrlOption) (*url.URL, error) {
	id := faker.UUIDHyphenated()
	userId := faker.UUIDHyphenated()
	shortCode := faker.Word(options.WithRandomStringLength(7))
	customAlias := faker.Word(options.WithRandomStringLength(4))
	originalUrl := faker.URL()
	var expiresOn time.Time
	expiresOnTimestamp := faker.Timestamp()
	if parsedExpiresOn, expiresOnTimestampErr := timeutils.ParseHumanFriendlyDate(expiresOnTimestamp); expiresOnTimestampErr != nil {
		expiresOn = time.Now().Add(time.Hour * 24)
	} else {
		expiresOn = parsedExpiresOn
	}
	keywords := []string{}

	for range 3 {
		keywords = append(keywords, faker.Word())
	}

	ogTitle := faker.Word()
	ogDescription := faker.Word()
	ogImageUrl := faker.URL()

	var urlId entity.ID
	if generatedUrlId, generatedIdErr := entity.StringToID(id); generatedIdErr != nil {
		urlId = entity.NewID()
	} else {
		urlId = generatedUrlId
	}

	params := url.URLParams{
		AggregateRootParams: entity.AggregateRootParams{
			EntityParams: entity.EntityParams{
				EntityIDParams: entity.EntityIDParams{
					ID: urlId,
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
		UserId:        userId,
		ShortCode:     shortCode,
		CustomAlias:   customAlias,
		OriginalUrl:   originalUrl,
		ExpiresOn:     expiresOn,
		Keywords:      keywords,
		OgTitle:       ogTitle,
		OgDescription: ogDescription,
		OgImageUrl:    ogImageUrl,
		Status:        url.URLStatusActive,
	}

	for _, option := range mockUrlOption {
		option(&params)
	}

	return url.NewUrl(params)
}

// WithId updates the id of the user entity
func WithId(id entity.ID) MockUrlOption {
	return func(r *url.URLParams) {
		r.ID = id
	}
}

// WithMetadata updates the metadata of the url entity
func WithMetadata(metadata map[string]any) MockUrlOption {
	return func(r *url.URLParams) {
		r.Metadata = metadata
	}
}

// WithDomainEvents updates the domain events of the url entity
func WithDomainEvents(domainEvents []entity.DomainEvent) MockUrlOption {
	return func(r *url.URLParams) {
		r.DomainEvents = domainEvents
	}
}

// WithCreatedTime updates the created at time of the url entity
func WithCreatedTime(createdAt time.Time) MockUrlOption {
	return func(r *url.URLParams) {
		r.CreatedAt = createdAt
	}
}

// WithUpdatedTime updates the updated at time of the url entity
func WithUpdatedTime(updatedAt time.Time) MockUrlOption {
	return func(r *url.URLParams) {
		r.UpdatedAt = updatedAt
	}
}

// WithDeletedTime updates the deleted at time of the url entity
func WithDeletedTime(deletedAt *time.Time) MockUrlOption {
	return func(r *url.URLParams) {
		r.DeletedAt = deletedAt
	}
}

func WithUserId(userId string) MockUrlOption {
	return func(r *url.URLParams) {
		r.UserId = userId
	}
}

func WithShortCode(shortCode string) MockUrlOption {
	return func(r *url.URLParams) {
		r.ShortCode = shortCode
	}
}

func WithCustomAlias(customAlias string) MockUrlOption {
	return func(r *url.URLParams) {
		r.CustomAlias = customAlias
	}
}

func WithOriginalUrl(originalUrl string) MockUrlOption {
	return func(r *url.URLParams) {
		r.OriginalUrl = originalUrl
	}
}

func WithOgTitle(ogTitle string) MockUrlOption {
	return func(u *url.URLParams) {
		u.OgTitle = ogTitle
	}
}
func WithOgDescription(ogDescription string) MockUrlOption {
	return func(u *url.URLParams) {
		u.OgDescription = ogDescription
	}
}

func WithOgImageUrl(ogImageUrl string) MockUrlOption {
	return func(u *url.URLParams) {
		u.OgImageUrl = ogImageUrl
	}
}

func WithExpiresOn(expiresOn time.Time) MockUrlOption {
	return func(r *url.URLParams) {
		r.ExpiresOn = expiresOn
	}
}
