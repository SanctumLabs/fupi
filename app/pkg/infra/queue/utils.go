package queue

import (
	"encoding/json"

	"github.com/pkg/errors"
)

// ToBytes converts a given message to bytes
func ToBytes(message Message) ([]byte, error) {
	bytes, err := json.Marshal(message)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert message %s to bytes", message)
	}

	return bytes, nil
}
