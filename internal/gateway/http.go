package gateway

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the gateway routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/reservations", reserveHandler(service))
}

func reserveHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request ReservationRequest
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body", "The body must be a JSON object with an items array.")
		}
		reservation, err := service.Reserve(c.Context(), request)
		switch {
		case errors.Is(err, ErrInvalidRequest):
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_reservation",
				"Invalid reservation",
				"Every item needs a SKU and a positive quantity. SKUs must be unique.")
		case errors.Is(err, ErrStockUnavailable):
			return fiberproblem.Write(c, fiber.StatusConflict, "stock_conflict",
				"Stock conflict", "At least one item does not exist or exceeds the available stock.")
		case err != nil:
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(reservation)
	}
}
