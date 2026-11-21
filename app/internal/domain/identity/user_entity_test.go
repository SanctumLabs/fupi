package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/sanctumlabs/curtz/app/internal/core/entity"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

func validRegisterParams() RegisterUserParams {
	return RegisterUserParams{
		Username:     "johndoe",
		FirstName:    "John",
		LastName:     "Doe",
		Email:        "john.doe@curtz.com",
		PasswordHash: "hashed-password",
	}
}

func mustRegister(t *testing.T) *User {
	t.Helper()
	user, err := Register(validRegisterParams())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return user
}

func lastEvent(t *testing.T, user *User) entity.DomainEvent {
	t.Helper()
	events := user.DomainEvents()
	if len(events) == 0 {
		t.Fatalf("expected a domain event to be recorded, got none")
	}
	return events[len(events)-1]
}

func TestRegister(t *testing.T) {
	t.Run("creates an INACTIVE, unverified user with identity and timestamps", func(t *testing.T) {
		user := mustRegister(t)

		if user.Status() != UserStatusInactive {
			t.Errorf("status = %s, want INACTIVE", user.Status())
		}
		verification := user.Verification()
		if verification.Verified() {
			t.Error("a freshly registered user must not be verified")
		}
		if user.IsActive() {
			t.Error("a freshly registered user must not be active")
		}
		if user.ID() == (entity.ID{}) {
			t.Error("expected a generated ID")
		}
		if user.CreatedAt().IsZero() || user.UpdatedAt().IsZero() {
			t.Error("expected timestamps to be set")
		}
		if user.PasswordHash() != "hashed-password" {
			t.Errorf("passwordHash = %q, want the hash passed in", user.PasswordHash())
		}
	})

	t.Run("keeps both first and last name", func(t *testing.T) {
		user := mustRegister(t)

		if user.FirstName() != "John" {
			t.Errorf("firstName = %q, want John", user.FirstName())
		}
		if user.LastName() != "Doe" {
			t.Errorf("lastName = %q, want Doe", user.LastName())
		}
		fullName := user.FullName()
		if got := fullName.Value(); got != "John Doe" {
			t.Errorf("fullName = %q, want %q", got, "John Doe")
		}
	})

	t.Run("issues a verification token that expires in the future", func(t *testing.T) {
		user := mustRegister(t)
		verification := user.Verification()

		if verification.Token() == "" {
			t.Fatal("expected a verification token")
		}
		if verification.IsExpired(time.Now()) {
			t.Error("a freshly issued token must not be expired")
		}
		if verification.IsExpired(time.Now().Add(VerificationTokenTTL+time.Minute)) == false {
			t.Errorf("token should be expired past its %s TTL", VerificationTokenTTL)
		}
	})

	t.Run("issues a distinct, unpredictable token per registration", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 50; i++ {
			user := mustRegister(t)
			verification := user.Verification()
			token := verification.Token()
			if seen[token] {
				t.Fatalf("verification token %q was issued twice", token)
			}
			seen[token] = true
		}
	})

	t.Run("records UserRegistered carrying the token", func(t *testing.T) {
		user := mustRegister(t)

		event := lastEvent(t, user)
		if event.EventType() != "user.registered" {
			t.Errorf("event type = %s, want user.registered", event.EventType())
		}
		registered, ok := event.(UserRegistered)
		if !ok {
			t.Fatalf("event is %T, want UserRegistered", event)
		}
		if registered.UserID != entity.IDToString(user.ID()) {
			t.Errorf("event UserID = %s, want %s", registered.UserID, entity.IDToString(user.ID()))
		}
		verification := user.Verification()
		if registered.VerificationToken != verification.Token() {
			t.Error("event must carry the same verification token as the aggregate")
		}
		if registered.Email != "john.doe@curtz.com" {
			t.Errorf("event Email = %s, want john.doe@curtz.com", registered.Email)
		}
	})

	t.Run("rejects an invalid email", func(t *testing.T) {
		params := validRegisterParams()
		params.Email = "not-an-email"
		if _, err := Register(params); err == nil {
			t.Error("expected an error for an invalid email")
		}
	})

	t.Run("rejects an empty first name", func(t *testing.T) {
		params := validRegisterParams()
		params.FirstName = ""
		if _, err := Register(params); err == nil {
			t.Error("expected an error for an empty first name")
		}
	})

	t.Run("accepts an empty last name", func(t *testing.T) {
		params := validRegisterParams()
		params.LastName = ""
		user, err := Register(params)
		if err != nil {
			t.Fatalf("last name is optional, got error: %v", err)
		}
		fullName := user.FullName()
		if got := fullName.Value(); got != "John" {
			t.Errorf("fullName = %q, want %q with no trailing space", got, "John")
		}
	})
}

func TestUser_Verify(t *testing.T) {
	t.Run("marks the user verified and ACTIVE, recording UserVerified", func(t *testing.T) {
		user := mustRegister(t)
		verification := user.Verification()
		token := verification.Token()
		before := user.UpdatedAt()

		if err := user.Verify(token, time.Now()); err != nil {
			t.Fatalf("Verify: %v", err)
		}

		verified := user.Verification()
		if !verified.Verified() {
			t.Error("user should be verified")
		}
		if user.Status() != UserStatusActive {
			t.Errorf("status = %s, want ACTIVE", user.Status())
		}
		if !user.IsActive() {
			t.Error("a verified user should be active")
		}
		if !user.UpdatedAt().After(before) {
			t.Error("expected updatedAt to advance")
		}
		if event := lastEvent(t, user); event.EventType() != "user.verified" {
			t.Errorf("event type = %s, want user.verified", event.EventType())
		}
	})

	t.Run("rejects a token that does not match", func(t *testing.T) {
		user := mustRegister(t)

		err := user.Verify("some-other-token", time.Now())
		if !errors.Is(err, errdefs.ErrVerificationTokenInvalid) {
			t.Errorf("err = %v, want ErrVerificationTokenInvalid", err)
		}
		verification := user.Verification()
		if user.Status() != UserStatusInactive || verification.Verified() {
			t.Error("a rejected verification must not change the user")
		}
		if len(user.DomainEvents()) != 1 {
			t.Errorf("expected only the UserRegistered event, got %d", len(user.DomainEvents()))
		}
	})

	t.Run("rejects an expired token", func(t *testing.T) {
		user := mustRegister(t)
		verification := user.Verification()
		token := verification.Token()

		err := user.Verify(token, time.Now().Add(VerificationTokenTTL+time.Minute))
		if !errors.Is(err, errdefs.ErrVerificationTokenExpired) {
			t.Errorf("err = %v, want ErrVerificationTokenExpired", err)
		}
		afterAttempt := user.Verification()
		if afterAttempt.Verified() {
			t.Error("an expired verification must not verify the user")
		}
	})

	t.Run("rejects verifying twice", func(t *testing.T) {
		user := mustRegister(t)
		verification := user.Verification()
		token := verification.Token()

		if err := user.Verify(token, time.Now()); err != nil {
			t.Fatalf("first Verify: %v", err)
		}
		err := user.Verify(token, time.Now())
		if !errors.Is(err, errdefs.ErrUserAlreadyVerified) {
			t.Errorf("err = %v, want ErrUserAlreadyVerified", err)
		}
	})

	t.Run("rejects verification on a user hydrated without a token", func(t *testing.T) {
		user, err := NewUser(UserParams{
			Username:  "nobody",
			FirstName: "No",
			LastName:  "Body",
			Email:     "nobody@curtz.com",
			Status:    UserStatusInactive,
		})
		if err != nil {
			t.Fatalf("NewUser: %v", err)
		}

		if err := user.Verify("", time.Now()); !errors.Is(err, errdefs.ErrVerificationTokenInvalid) {
			t.Errorf("err = %v, want ErrVerificationTokenInvalid", err)
		}
	})
}

func TestUser_MarkDeleted(t *testing.T) {
	deletableFrom := []UserStatus{UserStatusActive, UserStatusInactive, UserStatusSuspended}

	for _, status := range deletableFrom {
		t.Run("deletes from "+string(status), func(t *testing.T) {
			user := mustRegister(t)
			user.status = status

			if err := user.MarkDeleted(); err != nil {
				t.Fatalf("MarkDeleted: %v", err)
			}
			if user.Status() != UserStatusDeleted {
				t.Errorf("status = %s, want DELETED", user.Status())
			}
			if event := lastEvent(t, user); event.EventType() != "user.deleted" {
				t.Errorf("event type = %s, want user.deleted", event.EventType())
			}
		})
	}

	t.Run("rejects deleting an already DELETED user", func(t *testing.T) {
		user := mustRegister(t)
		user.status = UserStatusDeleted

		err := user.MarkDeleted()
		if !errors.Is(err, errdefs.ErrInvalidUserStatusTransition) {
			t.Errorf("err = %v, want ErrInvalidUserStatusTransition", err)
		}
		if len(user.DomainEvents()) != 1 {
			t.Errorf("expected no event on a rejected transition, got %d", len(user.DomainEvents()))
		}
	})
}

// The WithX helpers are copy-on-write: the receiver must be left untouched so that callers
// holding the original are not surprised by a mutation they did not ask for.
func TestUser_WithHelpersDoNotMutateReceiver(t *testing.T) {
	t.Run("WithEmail", func(t *testing.T) {
		user := mustRegister(t)
		original := user.Email()

		updated, err := NewEmail("new.address@curtz.com")
		if err != nil {
			t.Fatalf("NewEmail: %v", err)
		}

		copied := user.WithEmail(updated)

		if got := copied.Email(); got.Value() != "new.address@curtz.com" {
			t.Errorf("copy email = %s, want the updated address", got.Value())
		}
		if got := user.Email(); got.Value() != original.Value() {
			t.Errorf("receiver email = %s, want it unchanged at %s", got.Value(), original.Value())
		}
	})

	t.Run("WithPasswordHash", func(t *testing.T) {
		user := mustRegister(t)

		copied := user.WithPasswordHash("a-new-hash")

		if copied.PasswordHash() != "a-new-hash" {
			t.Errorf("copy hash = %q, want a-new-hash", copied.PasswordHash())
		}
		if user.PasswordHash() != "hashed-password" {
			t.Errorf("receiver hash = %q, want it unchanged", user.PasswordHash())
		}
	})

	t.Run("WithFullName", func(t *testing.T) {
		user := mustRegister(t)

		updated, err := NewUserFullName("Jane", "Roe")
		if err != nil {
			t.Fatalf("NewUserFullName: %v", err)
		}

		copied := user.WithFullName(updated)

		if copied.FirstName() != "Jane" || copied.LastName() != "Roe" {
			t.Errorf("copy name = %s %s, want Jane Roe", copied.FirstName(), copied.LastName())
		}
		if user.FirstName() != "John" || user.LastName() != "Doe" {
			t.Errorf("receiver name = %s %s, want it unchanged at John Doe", user.FirstName(), user.LastName())
		}
	})
}
