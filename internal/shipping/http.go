package shipping

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

const eventOrderPaid = "order.paid"

// RegisterRoutes adds the shipping routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/events", receiveEventHandler(service))
	app.Post("/webhooks/carrier", carrierWebhookHandler(service))
	app.Get("/shipments/:orderId", getHandler(service))
}

func receiveEventHandler(service *Service) fiber.Handler {
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
			OrderID string `json:"orderId"`
		}
		if err := json.Unmarshal(request.Data, &data); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_event",
				"Invalid event", "The data must contain an orderId.")
		}
		orderID, err := uuid.Parse(data.OrderID)
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_event",
				"Invalid event", "The data must contain an orderId.")
		}
		shipment, duplicate, err := service.CreateForOrder(c.Context(), orderID)
		if err != nil {
			return err
		}
		if duplicate {
			return c.Status(fiber.StatusOK).JSON(shipment)
		}
		return c.Status(fiber.StatusCreated).JSON(shipment)
	}
}

func carrierWebhookHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request struct {
			TrackingCode string `json:"trackingCode"`
			Status       string `json:"status"`
		}
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body", "The body must be a JSON object with trackingCode and status.")
		}
		if strings.TrimSpace(request.TrackingCode) == "" || strings.TrimSpace(request.Status) == "" {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body", "The body must be a JSON object with trackingCode and status.")
		}
		shipment, err := service.UpdateStatus(c.Context(), request.TrackingCode, request.Status)
		switch {
		case errors.Is(err, ErrUnknownShipment):
			return fiberproblem.Write(c, fiber.StatusNotFound, "shipment_not_found",
				"Shipment not found", "No shipment exists for this tracking code.")
		case errors.Is(err, ErrInvalidState):
			return fiberproblem.Write(c, fiber.StatusConflict, "invalid_transition",
				"Invalid transition", "The carrier status is backwards or unknown.")
		case err != nil:
			return err
		}
		return c.JSON(fiber.Map{"status": shipment.Status})
	}
}

func getHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		orderID, err := uuid.Parse(c.Params("orderId"))
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_order_id",
				"Invalid order ID", "The order ID must be a UUID.")
		}
		shipment, err := service.ShipmentForOrder(c.Context(), orderID)
		if errors.Is(err, ErrUnknownShipment) {
			return fiberproblem.Write(c, fiber.StatusNotFound, "shipment_not_found",
				"Shipment not found", "No shipment exists for this order.")
		}
		if err != nil {
			return err
		}
		return c.JSON(shipment)
	}
}
