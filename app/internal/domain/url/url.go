package url

import (
	"fmt"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

type (
	// URL is the aggregate root for the URL bounded context.
	// It owns all state transitions for a shortened link.
	URL struct {
		entity.AggregateRoot

		// UserID is the user id of the url owner
		userId entity.ID

		// ShortCode is the short code for the url
		shortCode ShortCode

		// CustomAlias is the custom alias for the url
		customAlias CustomAlias

		// OriginalURL is the original url
		originalUrl OriginalURL

		// Keywords is a list of keywords for the url
		keywords []Keyword

		status URLStatus

		// ExpiresOn is the expiration date for the url
		expiresOn time.Time

		ogTitle       string
		ogDescription string
		ogImageUrl    string
	}

	// URLParams represents the parameters for creating or updating a url
	URLParams struct {
		entity.AggregateRootParams

		// UserID is the user id of the url owner
		UserId string

		// ShortCode is the short code for the url
		ShortCode string

		// CustomAlias is the custom alias for the url
		CustomAlias string

		// OriginalURL is the original url
		OriginalUrl string

		// ExpiresOn is the expiration date for the url
		ExpiresOn time.Time

		// Keywords is a list of keywords for the url
		Keywords []string

		OgTitle       string
		OgDescription string
		OgImageUrl    string

		Status URLStatus
	}

	// CreateURLParams are the inputs needed to create a brand new URL. The short code comes from
	// the ShortCodeGenerator port; the aggregate never generates it.
	CreateURLParams struct {
		UserId      string
		ShortCode   ShortCode
		CustomAlias string
		OriginalUrl string
		ExpiresOn   time.Time
		Keywords    []string
		Metadata    map[string]any
	}
)

// NewUrl hydrates a URL entity from already-persisted state. It validates the value objects
// but not time-based invariants, so an EXPIRED url with a past expiry can be reconstituted.
func NewUrl(params URLParams) (*URL, error) {
	originalUrl, err := NewOriginalURL(params.OriginalUrl)
	if err != nil {
		return nil, err
	}

	kws, err := createKeywords(params.Keywords)
	if err != nil {
		return nil, err
	}

	var customAlias CustomAlias
	if params.CustomAlias != "" {
		customAlias, err = NewCustomAlias(params.CustomAlias)
		if err != nil {
			return nil, err
		}
	}

	shortCode, shortCodeErr := NewShortCode(params.ShortCode)
	if shortCodeErr != nil {
		return nil, shortCodeErr
	}

	aggregateRoot, err := entity.NewAggregateRoot(params.AggregateRootParams)
	if err != nil {
		return nil, err
	}

	userId, userIdErr := entity.StringToID(params.UserId)
	if userIdErr != nil {
		return nil, userIdErr
	}

	return &URL{
		AggregateRoot: aggregateRoot,
		shortCode:     shortCode,
		customAlias:   customAlias,
		userId:        userId,
		originalUrl:   originalUrl,
		keywords:      kws,
		expiresOn:     params.ExpiresOn,
		status:        params.Status,
		ogTitle:       params.OgTitle,
		ogDescription: params.OgDescription,
		ogImageUrl:    params.OgImageUrl,
	}, nil
}

// Create creates a new ACTIVE URL, assigns its identity and timestamps, and records URLCreated.
func Create(params CreateURLParams) (*URL, error) {
	if !params.ExpiresOn.After(time.Now()) {
		return nil, fmt.Errorf("%w: '%s'", errdefs.ErrPastExpiration, params.ExpiresOn)
	}

	now := time.Now()
	url, err := NewUrl(URLParams{
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
		UserId:      params.UserId,
		ShortCode:   params.ShortCode.Value(),
		CustomAlias: params.CustomAlias,
		OriginalUrl: params.OriginalUrl,
		ExpiresOn:   params.ExpiresOn,
		Keywords:    params.Keywords,
		Status:      URLStatusActive,
	})
	if err != nil {
		return nil, err
	}

	url.ApplyDomain(URLCreated{
		baseEvent:   newBaseEvent("url.created"),
		URLID:       entity.IDToString(url.ID()),
		UserID:      params.UserId,
		OriginalURL: url.originalUrl.Value(),
		ShortCode:   url.shortCode.Value(),
		ExpiresOn:   url.expiresOn,
	})

	return url, nil
}

// Redirect returns the original url to redirect to. Only an ACTIVE, unexpired url redirects.
func (url *URL) Redirect() (OriginalURL, error) {
	if url.status != URLStatusActive {
		return OriginalURL{}, errdefs.ErrURLNotActive
	}
	if !url.expiresOn.After(time.Now()) {
		return OriginalURL{}, errdefs.ErrURLExpired
	}
	return url.originalUrl, nil
}

// MarkExpired transitions ACTIVE → EXPIRED and records URLExpired.
func (url *URL) MarkExpired() error {
	if err := url.transition(URLStatusExpired, URLStatusActive); err != nil {
		return err
	}
	url.ApplyDomain(URLExpired{
		baseEvent: newBaseEvent("url.expired"),
		URLID:     entity.IDToString(url.ID()),
		ShortCode: url.shortCode.Value(),
	})
	return nil
}

// Suspend transitions ACTIVE → SUSPENDED and records URLSuspended.
func (url *URL) Suspend(reason SuspensionReason) error {
	if err := url.transition(URLStatusSuspended, URLStatusActive); err != nil {
		return err
	}
	url.ApplyDomain(URLSuspended{
		baseEvent: newBaseEvent("url.suspended"),
		URLID:     entity.IDToString(url.ID()),
		Reason:    reason,
	})
	return nil
}

// Reinstate transitions SUSPENDED → ACTIVE and records URLReinstated.
func (url *URL) Reinstate() error {
	if err := url.transition(URLStatusActive, URLStatusSuspended); err != nil {
		return err
	}
	url.ApplyDomain(URLReinstated{
		baseEvent: newBaseEvent("url.reinstated"),
		URLID:     entity.IDToString(url.ID()),
		ShortCode: url.shortCode.Value(),
	})
	return nil
}

// MarkDeleted transitions any non-deleted status → DELETED and records URLDeleted.
func (url *URL) MarkDeleted() error {
	if err := url.transition(URLStatusDeleted, URLStatusActive, URLStatusExpired, URLStatusSuspended); err != nil {
		return err
	}
	url.ApplyDomain(URLDeleted{
		baseEvent: newBaseEvent("url.deleted"),
		URLID:     entity.IDToString(url.ID()),
		ShortCode: url.shortCode.Value(),
	})
	return nil
}

// UpdateExpiry sets a new expiry on an ACTIVE url. The new expiry must be in the future.
func (url *URL) UpdateExpiry(expiresOn time.Time) error {
	if url.status != URLStatusActive {
		return errdefs.ErrURLNotActive
	}
	if !expiresOn.After(time.Now()) {
		return fmt.Errorf("%w: '%s'", errdefs.ErrPastExpiration, expiresOn)
	}
	url.expiresOn = expiresOn
	url.touch()
	return nil
}

// AddKeyword appends a keyword, enforcing the MaxKeywords cap.
func (url *URL) AddKeyword(keyword string) error {
	if len(url.keywords) >= MaxKeywords {
		return errdefs.ErrKeywordsCount
	}
	kw, err := NewKeyword(keyword)
	if err != nil {
		return err
	}
	url.keywords = append(url.keywords, kw)
	url.touch()
	return nil
}

// SetKeywords replaces all keywords, enforcing the MaxKeywords cap.
func (url *URL) SetKeywords(keywords []string) error {
	kws, err := createKeywords(keywords)
	if err != nil {
		return err
	}
	url.keywords = kws
	url.touch()
	return nil
}

// transition moves the url to `to` if its current status is one of `from`.
func (url *URL) transition(to URLStatus, from ...URLStatus) error {
	for _, f := range from {
		if url.status == f {
			url.status = to
			url.touch()
			return nil
		}
	}
	return fmt.Errorf("%w: %s → %s", errdefs.ErrInvalidStatusTransition, url.status, to)
}

func (url *URL) touch() {
	url.EntityTimestamp = url.EntityTimestamp.WithUpdatedAt(time.Now())
}

func (url *URL) UserId() entity.ID {
	return url.userId
}

// IsActive reports whether the url is ACTIVE and has not passed its expiry.
func (url *URL) IsActive() bool {
	return url.status == URLStatusActive && url.expiresOn.After(time.Now())
}

func (url *URL) OriginalURL() OriginalURL {
	return url.originalUrl
}

func (url *URL) ShortCode() ShortCode {
	return url.shortCode
}

func (url *URL) Keywords() []Keyword {
	return url.keywords
}

func (url *URL) ExpiresOn() time.Time {
	return url.expiresOn
}

func (url *URL) CustomAlias() CustomAlias {
	return url.customAlias
}

func (url *URL) Status() URLStatus {
	return url.status
}

func (url *URL) OgTitle() string {
	return url.ogTitle
}

func (url *URL) OgDescription() string {
	return url.ogDescription
}

func (url *URL) OgImageUrl() string {
	return url.ogImageUrl
}

// ExpiryDuration returns as a time.Duration how long before the url expires
// This returns an absolute value after subtracting time.Now()
func (url *URL) ExpiryDuration() time.Duration {
	duration := time.Until(url.expiresOn)
	if duration >= 0 {
		return duration
	}
	return -duration
}

// Prefix returns the url prefix for logging
func (url *URL) Prefix() string {
	return fmt.Sprintf("url-%s-%s", url.ID(), url.shortCode)
}

func (url *URL) String() string {
	return fmt.Sprintf("url(id=%s, userId: %s)", url.ID(), url.userId)
}
