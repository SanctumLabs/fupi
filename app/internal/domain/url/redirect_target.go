package url

import (
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// RedirectTarget is the part of a URL the redirect needs: where it points and whether it may redirect. It is what the
// redirect cache holds. The decision is not stored with it: Redirect makes it on every call, so a target that expires
// or changes status stops redirecting without being evicted.
type RedirectTarget struct {
	originalUrl OriginalURL
	status      URLStatus
	expiresOn   time.Time
}

// NewRedirectTarget builds a RedirectTarget from stored values.
func NewRedirectTarget(originalUrl string, status URLStatus, expiresOn time.Time) (RedirectTarget, error) {
	target, err := NewOriginalURL(originalUrl)
	if err != nil {
		return RedirectTarget{}, err
	}
	return RedirectTarget{originalUrl: target, status: status, expiresOn: expiresOn}, nil
}

// OriginalURL returns where the target points.
func (t RedirectTarget) OriginalURL() OriginalURL { return t.originalUrl }

// Status returns the lifecycle status the target was read with.
func (t RedirectTarget) Status() URLStatus { return t.status }

// ExpiresOn returns when the target stops redirecting.
func (t RedirectTarget) ExpiresOn() time.Time { return t.expiresOn }

// Redirect returns the original url to redirect to. Only an ACTIVE, unexpired target redirects.
func (t RedirectTarget) Redirect() (OriginalURL, error) {
	if t.status != URLStatusActive {
		return OriginalURL{}, errdefs.ErrURLNotActive
	}
	if !t.expiresOn.After(time.Now()) {
		return OriginalURL{}, errdefs.ErrURLExpired
	}
	return t.originalUrl, nil
}
