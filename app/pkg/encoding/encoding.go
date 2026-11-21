package encoding

import (
	"crypto/rand"
	b64 "encoding/base64"
	"encoding/hex"
	"log"

	"github.com/sanctumlabs/fupi/app/pkg"
	"github.com/sanctumlabs/fupi/app/pkg/identifier"

	nanoid "github.com/matoous/go-nanoid"
)

// Encode encodes the identifier to a base64 string that is url-safe.
func Encode(id identifier.ID) string {
	return b64.RawURLEncoding.EncodeToString(id.Bytes())
}

// Decode decodes a base64 string to a raw identifier.
func Decode(id string) (identifier.ID, error) {
	dec, err := b64.RawURLEncoding.DecodeString(id)

	if err != nil {
		return identifier.ID{}, err
	}

	decoded, err := identifier.FromBytes(dec)
	if err != nil {
		return identifier.ID{}, err
	}

	return decoded, nil
}

// GetUniqueShortCode returns a random unique short code.
func GetUniqueShortCode() (string, error) {
	l := pkg.ShortCodeLength
	alphanumeric := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	return nanoid.Generate(alphanumeric, l)
}

// GenUniqueID returns a random but unique id.
func GenUniqueID() identifier.ID {
	return identifier.New()
}

// GenHexKey generates a crypto-random key with byte length len and hex-encodes it to a string.
func GenHexKey(len int) string {
	bytes := make([]byte, len)
	_, err := rand.Read(bytes)
	if err != nil {
		log.Fatal(err)
	}

	return hex.EncodeToString(bytes)
}
