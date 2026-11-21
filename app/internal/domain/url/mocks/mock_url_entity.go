package urlmock

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	timeutils "github.com/sanctumlabs/fupi/app/pkg/utils/time"
)

// base62Alphabet is the character set shared by the ShortCode and CustomAlias value objects.
const base62Alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randomBase62 returns a random base62 string of exactly n characters.
//
// faker.Word is deliberately not used for these fields. It returns a word from a fixed dictionary
// and ignores options.WithRandomStringLength, so its length is arbitrary — a 4-character word such
// as "odit" fails the ShortCode 6-10 character rule, and a long one fails the CustomAlias 3-12
// character rule. That made MockUrl return an error at random. Generating the characters here keeps
// every mock URL valid, and makes short codes effectively collision-free for tests that persist
// several URLs against the unique index on urls.short_code.
func randomBase62(n int) string {
	value := make([]byte, n)
	for i := range value {
		value[i] = base62Alphabet[rand.IntN(len(base62Alphabet))]
	}
	return string(value)
}

// randomOriginalUrl returns a URL that always satisfies the OriginalURL value object.
//
// faker.URL() is not used: it draws paths from a fixed set of templates, one of which contains
// "xxx", and the domain's FilterRegex rejects that — so roughly one call in a thousand made MockUrl
// return an error. The hex suffix below cannot contain any filtered token (they all need letters
// outside 0-9a-f, or dots), comfortably clears the 15 character minimum, and keeps each generated
// URL distinct so tests may persist several of them.
func randomOriginalUrl() string {
	return fmt.Sprintf("https://example.com/%016x", rand.Uint64())
}

type MockUrlOption func(*url.URLParams)

func MockUrl(mockUrlOption ...MockUrlOption) (*url.URL, error) {
	id := faker.UUIDHyphenated()
	userId := faker.UUIDHyphenated()
	shortCode := randomBase62(7)
	customAlias := randomBase62(6)
	originalUrl := randomOriginalUrl()
	var expiresOn time.Time
	expiresOnTimestamp := faker.Timestamp()
	if parsedExpiresOn, expiresOnTimestampErr := timeutils.ParseHumanFriendlyDate(expiresOnTimestamp); expiresOnTimestampErr != nil {
		expiresOn = time.Now().Add(time.Hour * 24)
	} else {
		expiresOn = parsedExpiresOn
	}
	keywords := []string{}

	for range 3 {
		// two words, so a one-letter word such as "a" never falls below the keyword minimum length
		keywords = append(keywords, faker.Word()+faker.Word())
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

func WithKeywords(keywords []string) MockUrlOption {
	return func(r *url.URLParams) {
		r.Keywords = keywords
	}
}

func WithStatus(status url.URLStatus) MockUrlOption {
	return func(r *url.URLParams) {
		r.Status = status
	}
}
