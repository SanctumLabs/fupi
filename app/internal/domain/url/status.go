package url

// URLStatus drives the redirect decision. Only ACTIVE URLs redirect.
type URLStatus string

const (
	URLStatusActive    URLStatus = "ACTIVE"
	URLStatusExpired   URLStatus = "EXPIRED"
	URLStatusSuspended URLStatus = "SUSPENDED" // failed safety scan
	URLStatusDeleted   URLStatus = "DELETED"
)
