package invoicing

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

const eventOrderPaid = "order.paid"

// RegisterRoutes adds the invoicing routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/events", eventHandler(service))
	app.Get("/invoices/:orderId", lookupHandler(service))
}

func eventHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request struct {
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
		}
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body", "The body must be a JSON object with kind and data.")
		}
		if request.Kind != eventOrderPaid {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_event",
				"Invalid event", "The event kind must be order.paid.")
		}
		var data struct {
			OrderID    string `json:"orderId"`
			TotalMinor int64  `json:"totalMinor"`
		}
		if err := json.Unmarshal(request.Data, &data); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_event",
				"Invalid event", "data must be a JSON object with an orderId UUID and a totalMinor.")
		}
		orderID, err := uuid.Parse(data.OrderID)
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_event",
				"Invalid event", "data must be a JSON object with an orderId UUID and a totalMinor.")
		}
		invoice, duplicate, err := service.Issue(c.Context(), orderID, data.TotalMinor)
		if errors.Is(err, ErrInvalidIssue) {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_event",
				"Invalid event", "data must hold a non-negative totalMinor.")
		}
		if err != nil {
			return err
		}
		status := fiber.StatusCreated
		if duplicate {
			status = fiber.StatusOK
		}
		return c.Status(status).JSON(invoice)
	}
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
