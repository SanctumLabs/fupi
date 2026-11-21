package entity

import (
	"log/slog"

	"github.com/google/uuid"
)

type ID = uuid.UUID

// NewID generates a new time-ordered (UUIDv7) ID. IDs are generated in-process so an aggregate
// has its identity before it is persisted.
func NewID() ID {
	return ID(uuid.Must(uuid.NewV7()))
}

// IDToString returns the string representation of the ID
func IDToString(id ID) string {
	return uuid.UUID(id).String()
}

// IDToBytes converts an ID into a slice of Bytes
func IDToBytes(id ID) ([]byte, error) {
	v, err := id.MarshalBinary()
	if err != nil {
		slog.Error("failed to parse id", "id", id, "error", err.Error())
		return nil, err
	}

	return v, nil
}

// StringToID parses a string into an ID
func StringToID(idString string) (ID, error) {
	value, err := uuid.Parse(idString)
	if err != nil {
		slog.Error("failed to parse id", "id", idString, "error", err.Error())
		return ID{}, err
	}

	return ID(value), err
}

// BytesToID parses a slice of bytes to an ID
func BytesToID(idBytes []byte) (ID, error) {
	value, err := uuid.FromBytes(idBytes)
	if err != nil {
		slog.Error("failed to parse id to bytes", "id", idBytes, "error", err.Error())
		return ID{}, err
	}

	return ID(value), nil
}
