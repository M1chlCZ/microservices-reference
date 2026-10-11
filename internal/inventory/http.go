package inventory

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the inventory routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Get("/stock/:sku", stockHandler(service))
	app.Post("/reservations", reserveHandler(service))
	app.Delete("/reservations/:id", releaseHandler(service))
}

func stockHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		item, err := service.Item(c.Context(), c.Params("sku"))
		if errors.Is(err, ErrUnknownSKU) {
			return fiberproblem.Write(c, fiber.StatusNotFound, "stock_not_found",
				"Stock position not found", "No stock position exists for this SKU.")
		}
		if err != nil {
			return err
		}
		return c.JSON(item)
	}
}

func reserveHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request ReservationRequest
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body", "The body must be a JSON object with an items array.")
		}
		reservationID, err := service.Reserve(c.Context(), request)
		switch {
		case errors.Is(err, ErrInvalidReservation):
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_reservation",
				"Invalid reservation",
				"Every item needs a SKU and a positive quantity. SKUs must be unique.")
		case errors.Is(err, ErrUnknownSKU):
			return fiberproblem.Write(c, fiber.StatusConflict, "stock_conflict",
				"Stock conflict", "At least one SKU does not exist.")
		case errors.Is(err, ErrInsufficientStock):
			return fiberproblem.Write(c, fiber.StatusConflict, "stock_conflict",
				"Stock conflict", "At least one item exceeds the available stock.")
		case err != nil:
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"reservationId": reservationID.String(),
		})
	}
}

func releaseHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		reservationID, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_reservation_id",
				"Invalid reservation ID", "The reservation ID must be a UUID.")
		}
		if err = service.Release(c.Context(), reservationID); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}
