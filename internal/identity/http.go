package identity

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the identity routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/accounts", createAccountHandler(service))
	app.Post("/sessions", createSessionHandler(service))
	app.Get("/sessions/me", meHandler(service))
	app.Delete("/sessions", deleteSessionHandler(service))
	app.Post("/accounts/totp", provisionTOTPHandler(service))
	app.Post("/accounts/totp/verify", verifyTOTPHandler(service))
}

func createAccountHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_account",
				"Invalid account", "The body must be a JSON object with email and password.")
		}
		account, err := service.Register(c.Context(), request.Email, request.Password)
		switch {
		case errors.Is(err, ErrInvalidAccount):
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_account",
				"Invalid account",
				"The email must contain an @. The password must have 10 to 200 characters.")
		case errors.Is(err, ErrEmailTaken):
			return fiberproblem.Write(c, fiber.StatusConflict, "email_taken",
				"Email taken", "An account already exists for this email.")
		case err != nil:
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(account)
	}
}

func createSessionHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body", "The body must be a JSON object with email and password.")
		}
		session, err := service.Login(c.Context(), request.Email, request.Password)
		if errors.Is(err, ErrInvalidCredentials) {
			return invalidCredentials(c)
		}
		if err != nil {
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(session)
	}
}

func meHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		account, err := service.Authenticate(c.Context(), bearerToken(c))
		if errors.Is(err, ErrInvalidSession) {
			return invalidSession(c)
		}
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"id": account.ID, "email": account.Email})
	}
}

func deleteSessionHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := service.Logout(c.Context(), bearerToken(c)); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func provisionTOTPHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		account, err := service.Authenticate(c.Context(), bearerToken(c))
		if errors.Is(err, ErrInvalidSession) {
			return invalidSession(c)
		}
		if err != nil {
			return err
		}
		provisioning, err := service.ProvisionTOTP(c.Context(), account.ID)
		switch {
		case errors.Is(err, ErrTOTPConfirmed):
			return fiberproblem.Write(c, fiber.StatusConflict, "totp_confirmed",
				"TOTP confirmed", "The account already confirmed TOTP.")
		case errors.Is(err, ErrInvalidSession):
			return invalidSession(c)
		case err != nil:
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(provisioning)
	}
}

func verifyTOTPHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		account, err := service.Authenticate(c.Context(), bearerToken(c))
		if errors.Is(err, ErrInvalidSession) {
			return invalidSession(c)
		}
		if err != nil {
			return err
		}
		var request struct {
			Code string `json:"code"`
		}
		if err = c.Bind().Body(&request); err != nil {
			return invalidCode(c)
		}
		err = service.ConfirmTOTP(c.Context(), account.ID, request.Code)
		switch {
		case errors.Is(err, ErrInvalidCode):
			return invalidCode(c)
		case errors.Is(err, ErrInvalidSession):
			return invalidSession(c)
		case err != nil:
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func bearerToken(c fiber.Ctx) string {
	const prefix = "Bearer "
	header := c.Get(fiber.HeaderAuthorization)
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return header[len(prefix):]
}

func invalidCredentials(c fiber.Ctx) error {
	return fiberproblem.Write(c, fiber.StatusUnauthorized, "invalid_credentials",
		"Invalid credentials", "The email and password do not match an account.")
}

func invalidSession(c fiber.Ctx) error {
	return fiberproblem.Write(c, fiber.StatusUnauthorized, "invalid_session",
		"Authentication required", "The bearer token is missing, unknown or expired.")
}

func invalidCode(c fiber.Ctx) error {
	return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_code",
		"Invalid code", "The TOTP code is not valid for this account.")
}
