package shipping

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the shipping routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/webhooks/carrier", carrierWebhookHandler(service))
	app.Get("/shipments/:orderId", getHandler(service))
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
