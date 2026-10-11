package payments

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

const idempotencyKeyHeader = "Idempotency-Key"

// RegisterRoutes adds the payment routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/payment-intents", createIntentHandler(service))
	app.Post("/webhooks/provider", providerWebhookHandler(service))
	app.Get("/payment-intents/:id", intentHandler(service))
}

func createIntentHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request CreateIntentRequest
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body",
				"The body must be a JSON object with orderId, amountMinor and currency.")
		}
		intent, duplicate, err := service.CreateIntent(c.Context(), request.OrderID,
			request.AmountMinor, request.Currency, c.Get(idempotencyKeyHeader))
		switch {
		case errors.Is(err, ErrInvalidIntent):
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_payment_intent",
				"Invalid payment intent",
				"amountMinor is positive, currency is three uppercase letters and Idempotency-Key is required.")
		case errors.Is(err, ErrIdempotencyConflict):
			return fiberproblem.Write(c, fiber.StatusConflict, "idempotency_conflict",
				"Idempotency conflict", "The idempotency key was used with a different request.")
		case err != nil:
			return err
		}
		status := fiber.StatusCreated
		if duplicate {
			status = fiber.StatusOK
		}
		return c.Status(status).JSON(intent)
	}
}

func providerWebhookHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request ProviderWebhookRequest
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body",
				"The body must be a JSON object with providerRef and status.")
		}
		if strings.TrimSpace(request.ProviderRef) == "" || request.Status != "paid" {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_webhook",
				"Invalid webhook body", "providerRef is required and status must be paid.")
		}
		_, _, err := service.Capture(c.Context(), request.ProviderRef)
		switch {
		case errors.Is(err, ErrUnknownIntent):
			return fiberproblem.Write(c, fiber.StatusNotFound, "payment_intent_not_found",
				"Payment intent not found", "No payment intent exists for this provider reference.")
		case errors.Is(err, ErrInvalidState):
			return fiberproblem.Write(c, fiber.StatusConflict, "invalid_state",
				"Invalid payment intent state", "The payment intent is not capturable.")
		case err != nil:
			return err
		}
		return c.JSON(fiber.Map{"status": "paid"})
	}
}

func intentHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		intentID, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_payment_intent_id",
				"Invalid payment intent ID", "The payment intent ID must be a UUID.")
		}
		intent, err := service.Intent(c.Context(), intentID)
		if errors.Is(err, ErrUnknownIntent) {
			return fiberproblem.Write(c, fiber.StatusNotFound, "payment_intent_not_found",
				"Payment intent not found", "No payment intent exists for this ID.")
		}
		if err != nil {
			return err
		}
		return c.JSON(intent)
	}
}
