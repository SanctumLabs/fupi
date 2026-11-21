//go:build legacy

package middleware

import "github.com/gin-gonic/gin"

// Middleware is a type to allow the use of ordinary functions as API filters.
// Any struct that has the appropriate signature can be registered as a middleware.
type Middleware gin.HandlerFunc

// // Middleware is an interface to allow the use of ordinary functions as API filters.
// // Any struct that has the appropriate signature can be registered as a middleware.
// type Middleware interface {
// 	WrapHandler() gin.HandlerFunc
// }
