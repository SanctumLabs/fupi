package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfigTimeout(t *testing.T) {
	assert.Equal(t, DefaultOperationTimeout, Config{}.Timeout(), "zero value falls back to the default")
	assert.Equal(t, DefaultOperationTimeout, Config{OperationTimeout: -time.Second}.Timeout(), "negative falls back to the default")
	assert.Equal(t, 5*time.Second, Config{OperationTimeout: 5 * time.Second}.Timeout(), "a set value is kept")
}
