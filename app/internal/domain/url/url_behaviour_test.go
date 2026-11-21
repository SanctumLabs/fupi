package url

import (
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

func mustShortCode(t *testing.T, v string) ShortCode {
	t.Helper()
	sc, err := NewShortCode(v)
	if err != nil {
		t.Fatalf("NewShortCode(%s): %v", v, err)
	}
	return sc
}

func validCreateParams(t *testing.T) CreateURLParams {
	t.Helper()
	return CreateURLParams{
		UserId:      entity.IDToString(entity.NewID()),
		ShortCode:   mustShortCode(t, "abcdef12"),
		OriginalUrl: "https://example.com/path/to/resource",
		ExpiresOn:   time.Now().Add(24 * time.Hour),
		Keywords:    []string{"test"},
	}
}

func urlWithStatus(t *testing.T, status URLStatus) *URL {
	t.Helper()
	p := createValidURLParams()
	p.Status = status
	u, err := NewUrl(p)
	if err != nil {
		t.Fatalf("NewUrl: %v", err)
	}
	return u
}

func lastEvent(t *testing.T, u *URL) entity.DomainEvent {
	t.Helper()
	events := u.DomainEvents()
	if len(events) == 0 {
		t.Fatalf("expected a domain event to be recorded, got none")
	}
	return events[len(events)-1]
}

func TestCreate(t *testing.T) {
	t.Run("creates an ACTIVE url with identity and URLCreated event", func(t *testing.T) {
		u, err := Create(validCreateParams(t))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if u.Status() != URLStatusActive {
			t.Errorf("status = %s, want ACTIVE", u.Status())
		}
		if u.ID() == (entity.ID{}) {
			t.Error("expected a generated ID")
		}
		if u.CreatedAt().IsZero() || u.UpdatedAt().IsZero() {
			t.Error("expected timestamps to be set")
		}
		ev := lastEvent(t, u)
		if ev.EventType() != "url.created" {
			t.Errorf("event type = %s, want url.created", ev.EventType())
		}
		created, ok := ev.(URLCreated)
		if !ok {
			t.Fatalf("event is %T, want URLCreated", ev)
		}
		if created.ShortCode != "abcdef12" || created.URLID != entity.IDToString(u.ID()) {
			t.Errorf("unexpected URLCreated payload: %+v", created)
		}
	})

	t.Run("rejects an expiry in the past", func(t *testing.T) {
		p := validCreateParams(t)
		p.ExpiresOn = time.Now().Add(-time.Hour)
		if _, err := Create(p); !errors.Is(err, errdefs.ErrPastExpiration) {
			t.Errorf("err = %v, want ErrPastExpiration", err)
		}
	})

	t.Run("rejects a zero-value short code", func(t *testing.T) {
		p := validCreateParams(t)
		p.ShortCode = ShortCode{}
		if _, err := Create(p); !errors.Is(err, errdefs.ErrShortCodeInvalidLength) {
			t.Errorf("err = %v, want ErrShortCodeInvalidLength", err)
		}
	})
}

func TestURL_Redirect(t *testing.T) {
	t.Run("ACTIVE url redirects to original url", func(t *testing.T) {
		u := urlWithStatus(t, URLStatusActive)
		got, err := u.Redirect()
		if err != nil {
			t.Fatalf("Redirect: %v", err)
		}
		if got.Value() != u.OriginalURL().Value() {
			t.Errorf("Redirect() = %s, want %s", got.Value(), u.OriginalURL().Value())
		}
	})

	for _, status := range []URLStatus{URLStatusExpired, URLStatusSuspended, URLStatusDeleted} {
		t.Run(string(status)+" url does not redirect", func(t *testing.T) {
			u := urlWithStatus(t, status)
			if _, err := u.Redirect(); !errors.Is(err, errdefs.ErrURLNotActive) {
				t.Errorf("err = %v, want ErrURLNotActive", err)
			}
		})
	}

	t.Run("ACTIVE url past its expiry does not redirect", func(t *testing.T) {
		p := createValidURLParams()
		p.ExpiresOn = time.Now().Add(-time.Minute)
		u, err := NewUrl(p)
		if err != nil {
			t.Fatalf("NewUrl: %v", err)
		}
		if _, err := u.Redirect(); !errors.Is(err, errdefs.ErrURLExpired) {
			t.Errorf("err = %v, want ErrURLExpired", err)
		}
	})
}

func TestURL_StatusTransitions(t *testing.T) {
	type transition struct {
		name      string
		apply     func(*URL) error
		to        URLStatus
		eventType string
		allowed   []URLStatus
	}

	all := []URLStatus{URLStatusActive, URLStatusExpired, URLStatusSuspended, URLStatusDeleted}

	transitions := []transition{
		{"MarkExpired", (*URL).MarkExpired, URLStatusExpired, "url.expired", []URLStatus{URLStatusActive}},
		{"Suspend", func(u *URL) error { return u.Suspend(SuspensionReasonPhishing) }, URLStatusSuspended, "url.suspended", []URLStatus{URLStatusActive}},
		{"Reinstate", (*URL).Reinstate, URLStatusActive, "url.reinstated", []URLStatus{URLStatusSuspended}},
		{"MarkDeleted", (*URL).MarkDeleted, URLStatusDeleted, "url.deleted", []URLStatus{URLStatusActive, URLStatusExpired, URLStatusSuspended}},
	}

	isAllowed := func(tr transition, from URLStatus) bool {
		for _, s := range tr.allowed {
			if s == from {
				return true
			}
		}
		return false
	}

	for _, tr := range transitions {
		for _, from := range all {
			t.Run(tr.name+" from "+string(from), func(t *testing.T) {
				u := urlWithStatus(t, from)
				before := u.UpdatedAt()
				err := tr.apply(u)

				if !isAllowed(tr, from) {
					if !errors.Is(err, errdefs.ErrInvalidStatusTransition) {
						t.Fatalf("err = %v, want ErrInvalidStatusTransition", err)
					}
					if u.Status() != from {
						t.Errorf("status changed to %s on rejected transition", u.Status())
					}
					if len(u.DomainEvents()) != 0 {
						t.Errorf("expected no events on rejected transition, got %d", len(u.DomainEvents()))
					}
					return
				}

				if err != nil {
					t.Fatalf("%s: %v", tr.name, err)
				}
				if u.Status() != tr.to {
					t.Errorf("status = %s, want %s", u.Status(), tr.to)
				}
				if ev := lastEvent(t, u); ev.EventType() != tr.eventType {
					t.Errorf("event type = %s, want %s", ev.EventType(), tr.eventType)
				}
				if !u.UpdatedAt().After(before) {
					t.Errorf("expected updatedAt to advance")
				}
			})
		}
	}

	t.Run("Suspend records the reason", func(t *testing.T) {
		u := urlWithStatus(t, URLStatusActive)
		if err := u.Suspend(SuspensionReasonMalware); err != nil {
			t.Fatal(err)
		}
		ev, ok := lastEvent(t, u).(URLSuspended)
		if !ok || ev.Reason != SuspensionReasonMalware {
			t.Errorf("unexpected URLSuspended event: %+v", ev)
		}
	})
}

func TestURL_UpdateExpiry(t *testing.T) {
	t.Run("extends expiry on ACTIVE url", func(t *testing.T) {
		u := urlWithStatus(t, URLStatusActive)
		next := time.Now().Add(48 * time.Hour)
		if err := u.UpdateExpiry(next); err != nil {
			t.Fatal(err)
		}
		if !u.ExpiresOn().Equal(next) {
			t.Errorf("expiresOn = %s, want %s", u.ExpiresOn(), next)
		}
	})

	t.Run("rejects expiry in the past", func(t *testing.T) {
		u := urlWithStatus(t, URLStatusActive)
		if err := u.UpdateExpiry(time.Now().Add(-time.Hour)); !errors.Is(err, errdefs.ErrPastExpiration) {
			t.Errorf("err = %v, want ErrPastExpiration", err)
		}
	})

	t.Run("rejects update on non-ACTIVE url", func(t *testing.T) {
		u := urlWithStatus(t, URLStatusSuspended)
		if err := u.UpdateExpiry(time.Now().Add(time.Hour)); !errors.Is(err, errdefs.ErrURLNotActive) {
			t.Errorf("err = %v, want ErrURLNotActive", err)
		}
	})
}

func TestURL_Keywords(t *testing.T) {
	t.Run("AddKeyword appends up to the cap", func(t *testing.T) {
		p := createValidURLParams()
		p.Keywords = nil
		u, err := NewUrl(p)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < MaxKeywords; i++ {
			if err := u.AddKeyword("kw" + string(rune('a'+i))); err != nil {
				t.Fatalf("AddKeyword #%d: %v", i+1, err)
			}
		}
		if err := u.AddKeyword("one-too-many"); !errors.Is(err, errdefs.ErrKeywordsCount) {
			t.Errorf("err = %v, want ErrKeywordsCount", err)
		}
		if len(u.Keywords()) != MaxKeywords {
			t.Errorf("len(keywords) = %d, want %d", len(u.Keywords()), MaxKeywords)
		}
	})

	t.Run("AddKeyword rejects an invalid keyword", func(t *testing.T) {
		u := urlWithStatus(t, URLStatusActive)
		if err := u.AddKeyword("not valid!"); !errors.Is(err, errdefs.ErrInvalidKeyword) {
			t.Errorf("err = %v, want ErrInvalidKeyword", err)
		}
	})

	t.Run("SetKeywords replaces keywords and enforces the cap", func(t *testing.T) {
		u := urlWithStatus(t, URLStatusActive)
		if err := u.SetKeywords([]string{"alpha", "beta"}); err != nil {
			t.Fatal(err)
		}
		if got := u.Keywords(); len(got) != 2 || got[0].Value != "alpha" || got[1].Value != "beta" {
			t.Errorf("unexpected keywords: %+v", got)
		}
		tooMany := make([]string, MaxKeywords+1)
		for i := range tooMany {
			tooMany[i] = "kw" + string(rune('a'+i))
		}
		if err := u.SetKeywords(tooMany); !errors.Is(err, errdefs.ErrKeywordsCount) {
			t.Errorf("err = %v, want ErrKeywordsCount", err)
		}
	})
}
