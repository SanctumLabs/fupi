package identity

// UpdateUserRequest represents the request payload for updating an existing user
type UpdateUserRequest struct {
	ID           string  `json:"id"`
	Username     *string `json:"username,omitempty"`
	FirstName    *string `json:"first_name,omitempty"`
	LastName     *string `json:"last_name,omitempty"`
	Email        *string `json:"email,omitempty"`
	PasswordHash *string `json:"password_hash,omitempty"`
}

type UpdateUserMetadataVerificationRequest struct {
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type UpdateUserPasswordRequest struct {
	ID           string `json:"id"`
	PasswordHash string `json:"password_hash,omitempty"`
}
