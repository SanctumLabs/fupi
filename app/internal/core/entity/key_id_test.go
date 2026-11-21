package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewKeyID(t *testing.T) {
	t.Parallel()

	id := NewKeyID()
	assert.NotNil(t, id)
}

func TestStringToKeyID(t *testing.T) {
	t.Parallel()

	_, err := StringToKeyID("0ujsszwN8NRY24YaXiTIE2VWDTS")

	assert.Nil(t, err)
}
