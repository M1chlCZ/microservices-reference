package invoicing

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the invoicing routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Get("/invoices/:orderId", lookupHandler(service))
}

func lookupHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		orderID, err := uuid.Parse(c.Params("orderId"))
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_order_id",
				"Invalid order ID", "The order ID must be a UUID.")
		}
		invoice, err := service.InvoiceForOrder(c.Context(), orderID)
		if errors.Is(err, ErrUnknownInvoice) {
			return fiberproblem.Write(c, fiber.StatusNotFound, "invoice_not_found",
				"Invoice not found", "No invoice exists for this order.")
		}
		if err != nil {
			return err
		}
		return c.JSON(invoice)
	}
}
