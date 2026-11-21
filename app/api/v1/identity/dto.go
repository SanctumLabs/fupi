package identityapi

import "time"

// registerRequestDto is the payload for creating a new account.
//
// Unlike the pre-v2 API, which took only email and password, it carries a username and first
// name: users.username is NOT NULL UNIQUE and the domain requires a first name. See ADR-0007.
type registerRequestDto struct {
	Username  string `json:"username" validate:"required"`
	FirstName string `json:"first_name" validate:"required"`
	LastName  string `json:"last_name"`
	Email     string `json:"email" validate:"required,email"`
	Password  string `json:"password" validate:"required"`
}

type loginRequestDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type userResponseDto struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name,omitempty"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	Verified  bool      `json:"verified"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type loginResponseDto struct {
	userResponseDto
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type oauthTokenResponseDto struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

// errorResponseDto is the single error shape returned by every identity endpoint.
type errorResponseDto struct {
	Message string `json:"message"`
}
