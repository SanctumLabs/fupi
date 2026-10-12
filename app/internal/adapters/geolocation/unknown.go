// Package geolocation implements ports.GeoLocator.
package geolocation

import (
	"context"

	"github.com/sanctumlabs/fupi/app/internal/ports"
)

var _ ports.GeoLocator = Unknown{}

// Unknown is the GeoLocator used until a GeoIP database is chosen: it resolves no country, so events carry an empty
// country code.
type Unknown struct{}

// Country always returns "".
func (Unknown) Country(context.Context, string) (string, error) { return "", nil }
