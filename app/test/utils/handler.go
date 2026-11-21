//go:build legacy

package utils

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin"
)

// MockRequestBody mocks a request body marshalling content into the gin context
func MockRequestBody(c *gin.Context, content interface{}) {
	jsonBytes, err := json.Marshal(content)
	if err != nil {
		panic(err)
	}

	// the request body must be an io.ReadCloser
	// the bytes buffer though doesn't implement io.Closer,
	// so you wrap it in a no-op closer
	c.Request.Body = io.NopCloser(bytes.NewBuffer(jsonBytes))
}
