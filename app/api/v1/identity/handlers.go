package identityapi

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	identityapp "github.com/sanctumlabs/curtz/app/internal/application/identity"
)

// register creates a new account and triggers the verification email.
//
// Register godoc
// @Summary     Register a new user account
// @Description Creates an INACTIVE account and emails a verification link
// @Tags        identity
// @Accept      json
// @Produce     json
// @Success     201 {object} identityapi.userResponseDto
// @Failure     400 {object} identityapi.errorResponseDto
// @Failure     409 {object} identityapi.errorResponseDto
// @Failure     422 {object} identityapi.errorResponseDto
// @Router      /auth/register [post]
func (rtr *identityRouter) register(ctx *fiber.Ctx) error {
	var request registerRequestDto
	if err := ctx.BodyParser(&request); err != nil {
		return respondValidationError(ctx, "request body is not valid JSON")
	}

	// first_name is validated by the domain (UserFullName), which reports it as a 400.
	if request.Email == "" || request.Password == "" || request.Username == "" {
		return respondValidationError(ctx, "username, email and password are required")
	}

	user, err := rtr.svc.Register(ctx.UserContext(), identityapp.RegisterCommand{
		Username:  request.Username,
		FirstName: request.FirstName,
		LastName:  request.LastName,
		Email:     request.Email,
		Password:  request.Password,
	})
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Status(fiber.StatusCreated).JSON(toUserResponse(user))
}

// login authenticates an account and issues a token pair.
//
// Login godoc
// @Summary     Log in to a registered account
// @Tags        identity
// @Accept      json
// @Produce     json
// @Success     200 {object} identityapi.loginResponseDto
// @Failure     401 {object} identityapi.errorResponseDto
// @Failure     403 {object} identityapi.errorResponseDto
// @Failure     422 {object} identityapi.errorResponseDto
// @Router      /auth/login [post]
func (rtr *identityRouter) login(ctx *fiber.Ctx) error {
	var request loginRequestDto
	if err := ctx.BodyParser(&request); err != nil {
		return respondValidationError(ctx, "request body is not valid JSON")
	}

	if request.Email == "" || request.Password == "" {
		return respondValidationError(ctx, "email and password are required")
	}

	user, tokens, err := rtr.svc.Login(ctx.UserContext(), request.Email, request.Password)
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Status(fiber.StatusOK).JSON(loginResponseDto{
		userResponseDto: toUserResponse(user),
		AccessToken:     tokens.AccessToken,
		RefreshToken:    tokens.RefreshToken,
	})
}

// oauthToken exchanges a refresh token for a fresh token pair.
//
// OAuthToken godoc
// @Summary     Exchange a refresh token for a new access token
// @Tags        identity
// @Produce     json
// @Param       grant_type    query string true "must be refresh_token"
// @Param       refresh_token query string true "the refresh token"
// @Success     200 {object} identityapi.oauthTokenResponseDto
// @Failure     401 {object} identityapi.errorResponseDto
// @Router      /auth/oauth/token [post]
func (rtr *identityRouter) oauthToken(ctx *fiber.Ctx) error {
	if grantType := ctx.Query("grant_type"); grantType != "refresh_token" {
		return ctx.Status(fiber.StatusUnauthorized).JSON(errorResponseDto{
			Message: "unsupported grant_type",
		})
	}

	tokens, err := rtr.svc.Refresh(ctx.UserContext(), ctx.Query("refresh_token"))
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Status(fiber.StatusOK).JSON(oauthTokenResponseDto{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    "Bearer",
	})
}

// verify completes email verification from the link sent at registration.
//
// Verify godoc
// @Summary     Verify an account from an emailed link
// @Tags        identity
// @Produce     json
// @Param       v query string true "verification token"
// @Success     200 {object} identityapi.userResponseDto
// @Failure     400 {object} identityapi.errorResponseDto
// @Router      /auth/verify [get]
func (rtr *identityRouter) verify(ctx *fiber.Ctx) error {
	token := strings.TrimSpace(ctx.Query("v"))

	user, err := rtr.svc.VerifyEmail(ctx.UserContext(), token)
	if err != nil {
		return respondError(ctx, err)
	}

	return ctx.Status(fiber.StatusOK).JSON(toUserResponse(user))
}
