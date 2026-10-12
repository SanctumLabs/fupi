package urlapi

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
)

// respondError maps a domain or application error onto an HTTP status.
//
// Only errors that have been deliberately classified reach the client with their own message; an unclassified error is
// reported as a generic 500 so internal detail (SQL, driver errors) never leaks into a response body. A NotFound never
// says why: a url that is someone else's, deleted or expired looks the same as one that never existed.
func respondError(ctx *fiber.Ctx, err error) error {
	switch {
	case errdefs.IsInvalidParameter(err):
		return ctx.Status(fiber.StatusBadRequest).JSON(errorResponseDto{Message: err.Error()})
	case errdefs.IsNotFound(err):
		return ctx.Status(fiber.StatusNotFound).JSON(errorResponseDto{Message: "url not found"})
	case errdefs.IsConflict(err):
		return ctx.Status(fiber.StatusConflict).JSON(errorResponseDto{Message: conflictMessage(err)})
	default:
		slog.ErrorContext(ctx.UserContext(), "url request failed", "error", err, "path", ctx.Path())
		return ctx.Status(fiber.StatusInternalServerError).JSON(errorResponseDto{Message: "something went wrong"})
	}
}

// conflictMessage names the conflict without echoing the database error, which carries the constraint and the value.
func conflictMessage(err error) string {
	switch {
	case errors.Is(err, errdefs.ErrURLAlreadyExists):
		return "this url has already been shortened"
	case errors.Is(err, errdefs.ErrURLNotActive), errors.Is(err, errdefs.ErrInvalidStatusTransition):
		return "the url is not active"
	default:
		return "the custom alias or a keyword is already in use"
	}
}

// respondValidationError reports a malformed or incomplete payload.
func respondValidationError(ctx *fiber.Ctx, message string) error {
	return ctx.Status(fiber.StatusUnprocessableEntity).JSON(errorResponseDto{Message: message})
}
