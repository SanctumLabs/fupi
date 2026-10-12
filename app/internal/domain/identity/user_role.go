package identity

// UserRole says what a user may do beyond managing their own urls. It is set in the datastore only (ADR-0022).
type UserRole string

const (
	UserRoleUser  UserRole = "user"
	UserRoleAdmin UserRole = "admin"
)
