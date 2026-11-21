package entity

import (
	"log"

	"github.com/segmentio/ksuid"
)

// KeyID is a unique key useful for entities that allows sorting
type KeyID = ksuid.KSUID

// NewKeyId creates a new Key ID which is a wrapper around KSUID
func NewKeyID() KeyID {
	return KeyID(ksuid.New())
}

// KeyIdToString returns the string representation of the Key
func KeyIdToString(keyId KeyID) string {
	return keyId.String()
}

// KeyIdToBytes converts a key ID into a slice of Bytes
func KeyIdToBytes(id KeyID) []byte {
	return id.Bytes()
}

// StringToKeyID parses a string into an ID
func StringToKeyID(idString string) (KeyID, error) {
	value, err := ksuid.Parse(idString)
	if err != nil {
		log.Println("failed to parse key id: ", err.Error())
		return KeyID{}, err
	}

	return KeyID(value), err
}

// BytesToKeyID parses a slice of bytes to an ID
func BytesToKeyID(keyBytes []byte) (KeyID, error) {
	value, err := ksuid.FromBytes(keyBytes)
	if err != nil {
		return KeyID{}, err
	}

	return KeyID(value), nil
}
