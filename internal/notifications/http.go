package notifications

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the notification routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Get("/notifications", listHandler(service))
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
