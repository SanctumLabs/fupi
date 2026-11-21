//go:build legacy

package entities

import (
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/encoding"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
)

// Token contains token information for a user
type Token struct {
	APIKey               identifier.ID
	ResetPasswordExpires *time.Time
	ResetPasswordToken   *identifier.ID
	VerificationExpires  time.Time
	VerificationToken    identifier.ID
}

func NewToken() Token {
	var now time.Time = time.Now()
	verificationToken := encoding.GenUniqueID()
	return Token{
		VerificationToken:   verificationToken,
		VerificationExpires: now.Add(time.Minute * 15).UTC().Round(time.Microsecond), //expires 15 mins later
	}
}
