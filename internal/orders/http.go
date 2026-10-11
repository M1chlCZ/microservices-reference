package orders

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the order routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/orders", createHandler(service))
	app.Get("/orders/:id", getHandler(service))
}

func createHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request CreateRequest
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body",
				"The body must be a JSON object with customerRef and lines.")
		}
		order, err := service.Create(c.Context(), request)
		switch {
		case errors.Is(err, ErrInvalidOrder):
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_order",
				"Invalid order",
				"customerRef is required. Every line needs a SKU, a positive quantity and a price.")
		case errors.Is(err, ErrStockUnavailable):
			return fiberproblem.Write(c, fiber.StatusConflict, "stock_unavailable",
				"Stock unavailable", "The inventory service refused the reservation.")
		case err != nil:
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(order)
	}
}

func getHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		orderID, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_order_id",
				"Invalid order ID", "The order ID must be a UUID.")
		}
		order, err := service.Get(c.Context(), orderID)
		if errors.Is(err, ErrUnknownOrder) {
			return fiberproblem.Write(c, fiber.StatusNotFound, "order_not_found",
				"Order not found", "No order exists for this ID.")
		}
		if err != nil {
			return err
		}
		return c.JSON(order)
	}
}
