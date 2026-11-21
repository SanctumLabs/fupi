package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// NewIdempotencyKey creates a consistent hash string from input data
// suitable for use as an idempotency key
func NewIdempotencyKey(input string) string {
	// Create a new SHA-256 hash
	hasher := sha256.New()

	// Write the input string to the hasher
	hasher.Write([]byte(input))

	// Get the hash sum and convert to hex string
	hashBytes := hasher.Sum(nil)
	hashString := hex.EncodeToString(hashBytes)

	return hashString
}

// NewIdempotencyKeyWithPrefix creates a hash with a custom prefix
// useful for namespacing idempotency keys
func NewIdempotencyKeyWithPrefix(input, prefix string) string {
	hash := NewIdempotencyKey(input)
	return fmt.Sprintf("%s_%s", prefix, hash)
}

// NewIdempotencyKeyShort creates a shorter hash (first 16 characters)
// for cases where you need a more compact idempotency key
func NewIdempotencyKeyShort(input string) string {
	fullHash := NewIdempotencyKey(input)
	if len(fullHash) >= 16 {
		return fullHash[:16]
	}
	return fullHash
}
