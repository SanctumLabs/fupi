package identityapi

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

// respondError maps a domain or application error onto an HTTP status.
//
// Only errors that have been deliberately classified reach the client with their own message; an
// unclassified error is reported as a generic 500 so internal detail (SQL, driver errors) never
// leaks into a response body.
func respondError(ctx *fiber.Ctx, err error) error {
	switch {
	case errdefs.IsInvalidParameter(err):
		return ctx.Status(fiber.StatusBadRequest).JSON(errorResponseDto{Message: err.Error()})
	case errdefs.IsUnauthorized(err):
		return ctx.Status(fiber.StatusUnauthorized).JSON(errorResponseDto{Message: err.Error()})
	case errdefs.IsForbidden(err):
		return ctx.Status(fiber.StatusForbidden).JSON(errorResponseDto{Message: err.Error()})
	case errdefs.IsNotFound(err):
		return ctx.Status(fiber.StatusNotFound).JSON(errorResponseDto{Message: err.Error()})
	case errdefs.IsConflict(err):
		return ctx.Status(fiber.StatusConflict).JSON(errorResponseDto{Message: "an account with those details already exists"})
	default:
		slog.ErrorContext(ctx.UserContext(), "identity request failed", "error", err, "path", ctx.Path())
		return ctx.Status(fiber.StatusInternalServerError).JSON(errorResponseDto{Message: "something went wrong"})
	}
}

// respondValidationError reports a malformed or incomplete payload.
func respondValidationError(ctx *fiber.Ctx, message string) error {
	return ctx.Status(fiber.StatusUnprocessableEntity).JSON(errorResponseDto{Message: message})
}
