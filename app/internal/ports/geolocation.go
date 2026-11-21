package ports

import "context"

// GeoLocator resolves where a visitor is from.
type GeoLocator interface {
	// Country returns the ISO 3166-1 alpha-2 code of the country the IP address is in, or "" when it is not known.
	Country(ctx context.Context, ip string) (string, error)
}
