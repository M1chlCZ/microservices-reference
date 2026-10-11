package notifications

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the notification routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/events", receiveHandler(service))
	app.Get("/notifications", listHandler(service))
}

func receiveHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request struct {
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
		}
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body", "The body must be a JSON object with kind and data.")
		}
		notification, err := service.Receive(c.Context(), request.Kind, request.Data)
		if errors.Is(err, ErrInvalidEvent) {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_event",
				"Invalid event", "kind is required and data must be a JSON object.")
		}
		if err != nil {
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(notification)
	}
}

func listHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		limit := 20
		if value := c.Query("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_limit",
					"Invalid limit", "The limit must be an integer.")
			}
			limit = parsed
		}
		notifications, err := service.List(c.Context(), limit)
		if err != nil {
			return err
		}
		return c.JSON(notifications)
	}
}
