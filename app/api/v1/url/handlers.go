package urlapi

import (
	"github.com/gofiber/fiber/v2"
	apimiddleware "github.com/sanctumlabs/fupi/app/api/middleware"
	urlapp "github.com/sanctumlabs/fupi/app/internal/application/url"
)

// callerID returns the id of the authenticated caller the auth middleware put on the context.
func callerID(ctx *fiber.Ctx) (string, bool) {
	userID, ok := ctx.Locals(apimiddleware.UserIDContextKey).(string)
	return userID, ok && userID != ""
}

// create shortens a url for the caller.
//
// Create godoc
// @Summary     Shorten a url
// @Tags        urls
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body urlapi.createUrlRequestDto true "the url to shorten"
// @Success     201 {object} urlapi.urlResponseDto
// @Failure     400 {object} urlapi.errorResponseDto
// @Failure     409 {object} urlapi.errorResponseDto
// @Failure     422 {object} urlapi.errorResponseDto
// @Router      /api/v1/fupi/urls [post]
func (rtr *urlRouter) create(ctx *fiber.Ctx) error {
	userID, ok := callerID(ctx)
	if !ok {
		return ctx.SendStatus(fiber.StatusUnauthorized)
	}

	var request createUrlRequestDto
	if err := ctx.BodyParser(&request); err != nil {
		return respondValidationError(ctx, "request body is not valid JSON")
	}
	if request.OriginalUrl == "" || request.ExpiresOn == nil {
		return respondValidationError(ctx, "original_url and expires_on are required")
	}

	created, err := rtr.svc.Shorten(ctx.UserContext(), urlapp.ShortenCommand{
		UserID:      userID,
		OriginalURL: request.OriginalUrl,
		CustomAlias: request.CustomAlias,
		ExpiresOn:   *request.ExpiresOn,
		Keywords:    request.Keywords,
	})
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Status(fiber.StatusCreated).JSON(toUrlResponse(rtr.shortBaseURL, created))
}

// list returns a page of the caller's urls.
//
// List godoc
// @Summary     List the caller's urls
// @Tags        urls
// @Produce     json
// @Security    BearerAuth
// @Param       page       query int    false "page number, from 1"          default(1)   minimum(1)
// @Param       size       query int    false "page size"                    default(100) minimum(1) maximum(100)
// @Param       order_by   query string false "field to order by"            Enums(created_at, updated_at)
// @Param       sort_order query string false "sort direction"               Enums(ASC, DESC)
// @Param       status     query string false "only urls in this status"     Enums(ACTIVE, EXPIRED, SUSPENDED)
// @Success     200 {object} urlapi.urlListResponseDto
// @Failure     422 {object} urlapi.errorResponseDto
// @Router      /api/v1/fupi/urls [get]
func (rtr *urlRouter) list(ctx *fiber.Ctx) error {
	userID, ok := callerID(ctx)
	if !ok {
		return ctx.SendStatus(fiber.StatusUnauthorized)
	}

	params, paramsErr := parseListParams(ctx)
	if paramsErr != nil {
		return respondValidationError(ctx, paramsErr.Error())
	}

	page, err := rtr.svc.List(ctx.UserContext(), userID, params)
	if err != nil {
		return respondError(ctx, err)
	}

	records := make([]urlResponseDto, 0, len(page.Records))
	for _, record := range page.Records {
		records = append(records, toUrlResponse(rtr.shortBaseURL, record))
	}

	return ctx.Status(fiber.StatusOK).JSON(urlListResponseDto{
		Records: records,
		Total:   page.Total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// get returns one of the caller's urls.
//
// Get godoc
// @Summary     Get one of the caller's urls
// @Tags        urls
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "url id"
// @Success     200 {object} urlapi.urlResponseDto
// @Failure     404 {object} urlapi.errorResponseDto
// @Router      /api/v1/fupi/urls/{id} [get]
func (rtr *urlRouter) get(ctx *fiber.Ctx) error {
	userID, ok := callerID(ctx)
	if !ok {
		return ctx.SendStatus(fiber.StatusUnauthorized)
	}

	found, err := rtr.svc.Get(ctx.UserContext(), userID, ctx.Params("id"))
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Status(fiber.StatusOK).JSON(toUrlResponse(rtr.shortBaseURL, found))
}

// update changes the expiry and/or the keywords of one of the caller's urls.
//
// Update godoc
// @Summary     Change the expiry or keywords of one of the caller's urls
// @Tags        urls
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "url id"
// @Param       request body urlapi.updateUrlRequestDto true "the fields to change"
// @Success     200 {object} urlapi.urlResponseDto
// @Failure     400 {object} urlapi.errorResponseDto
// @Failure     404 {object} urlapi.errorResponseDto
// @Failure     409 {object} urlapi.errorResponseDto
// @Failure     422 {object} urlapi.errorResponseDto
// @Router      /api/v1/fupi/urls/{id} [patch]
func (rtr *urlRouter) update(ctx *fiber.Ctx) error {
	userID, ok := callerID(ctx)
	if !ok {
		return ctx.SendStatus(fiber.StatusUnauthorized)
	}

	var request updateUrlRequestDto
	if err := ctx.BodyParser(&request); err != nil {
		return respondValidationError(ctx, "request body is not valid JSON")
	}
	if request.ExpiresOn == nil && request.Keywords == nil {
		return respondValidationError(ctx, "expires_on or keywords is required")
	}

	updated, err := rtr.svc.Update(ctx.UserContext(), urlapp.UpdateCommand{
		UserID:    userID,
		ID:        ctx.Params("id"),
		ExpiresOn: request.ExpiresOn,
		Keywords:  request.Keywords,
	})
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Status(fiber.StatusOK).JSON(toUrlResponse(rtr.shortBaseURL, updated))
}

// delete deletes one of the caller's urls. It stops redirecting at once.
//
// Delete godoc
// @Summary     Delete one of the caller's urls
// @Tags        urls
// @Security    BearerAuth
// @Param       id path string true "url id"
// @Success     204
// @Failure     404 {object} urlapi.errorResponseDto
// @Router      /api/v1/fupi/urls/{id} [delete]
func (rtr *urlRouter) delete(ctx *fiber.Ctx) error {
	userID, ok := callerID(ctx)
	if !ok {
		return ctx.SendStatus(fiber.StatusUnauthorized)
	}

	if err := rtr.svc.Delete(ctx.UserContext(), userID, ctx.Params("id")); err != nil {
		return respondError(ctx, err)
	}

	return ctx.SendStatus(fiber.StatusNoContent)
}

// redirect sends the visitor of a short url to its target. It is public.
//
// Redirect godoc
// @Summary     Follow a short url
// @Tags        redirect
// @Param       code path string true "short code or custom alias"
// @Success     302 "redirects to the original url"
// @Failure     404 {object} urlapi.errorResponseDto
// @Router      /{code} [get]
//
// A 302 rather than a 301: a permanent redirect is cached by browsers indefinitely, so a url that is later deleted,
// expired or suspended would keep redirecting.
func (rtr *urlRouter) redirect(ctx *fiber.Ctx) error {
	target, err := rtr.svc.Resolve(ctx.UserContext(), ctx.Params("code"))
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Redirect(target.Value(), fiber.StatusFound)
}
