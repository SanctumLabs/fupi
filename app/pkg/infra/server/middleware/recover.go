package middleware

import (
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/gofiber/fiber/v2"
	fiberRecover "github.com/gofiber/fiber/v2/middleware/recover"
)

// RecoverMiddleware captures errors anywhere in the stack of the application and handles it gracefully
func RecoverMiddleware() fiber.Handler {
	// TODO: setup config to enable sending errors to a centralized reporting tool
	// return recover.New(recover.Config{
	// 	StackTraceHandler: func(c *fiber.Ctx, err interface{}) {
	// 		c.Status(fiber.StatusInternalServerError)
	// 		// TODO: send this to an error reporting tool
	// 	},
	// })
	return fiberRecover.New()
}

// GlobalRecover handles panics at the application level
func GlobalRecover() {
	if r := recover(); r != nil {
		// Get stack trace
		stackTrace := debug.Stack()

		// Log the panic with stack trace
		errorMsg := fmt.Sprintf("PANIC RECOVERED: %v\n%s", r, stackTrace)
		slog.Warn(errorMsg)

		// TODO: notify administrators via email, Slack, etc.
		// notifyAdmins(errorMsg)

		// Depending on your application, you might choose to:
		// 1. Continue execution with degraded functionality
		// 2. Restart the affected goroutine
		// 3. Exit the application (in critical scenarios)
		slog.Warn("GlobalRecover> The application encountered an unexpected error and has recovered.")
	}
}
