package catalog

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/m1chlcz/fiber-problem"
)

// RegisterRoutes adds the catalog routes to the app.
func RegisterRoutes(app *fiber.App, service *Service) {
	app.Post("/products", createHandler(service))
	app.Get("/products", listHandler(service))
	app.Get("/products/:id", getHandler(service))
	app.Patch("/products/:id", patchHandler(service))
}

func createHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request CreateRequest
		if err := c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body",
				"The body must be a JSON object with sku, name and priceMinor.")
		}
		product, err := service.Create(c.Context(), request)
		switch {
		case errors.Is(err, ErrInvalidProduct):
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_product",
				"Invalid product",
				"sku and name are required and priceMinor must be zero or greater.")
		case errors.Is(err, ErrDuplicateSKU):
			return fiberproblem.Write(c, fiber.StatusConflict, "sku_conflict",
				"SKU conflict", "A product with this SKU already exists.")
		case err != nil:
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(product)
	}
}

func listHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		limit := defaultLimit
		if value := c.Query("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_limit",
					"Invalid limit", "The limit must be an integer.")
			}
			limit = parsed
		}
		products, err := service.List(c.Context(), limit, c.Query("cursor"))
		if err != nil {
			return err
		}
		return c.JSON(products)
	}
}

func getHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		productID, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_product_id",
				"Invalid product ID", "The product ID must be a UUID.")
		}
		product, err := service.Get(c.Context(), productID)
		if errors.Is(err, ErrUnknownProduct) {
			return fiberproblem.Write(c, fiber.StatusNotFound, "product_not_found",
				"Product not found", "No product exists for this ID.")
		}
		if err != nil {
			return err
		}
		return c.JSON(product)
	}
}

func patchHandler(service *Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		productID, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_product_id",
				"Invalid product ID", "The product ID must be a UUID.")
		}
		var request PatchRequest
		if err = c.Bind().Body(&request); err != nil {
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_body",
				"Invalid request body",
				"The body must be a JSON object with name, priceMinor or status.")
		}
		product, err := service.Patch(c.Context(), productID, request)
		switch {
		case errors.Is(err, ErrInvalidProduct):
			return fiberproblem.Write(c, fiber.StatusUnprocessableEntity, "invalid_product",
				"Invalid product", "name must not be empty and priceMinor must be zero or greater.")
		case errors.Is(err, ErrUnknownProduct):
			return fiberproblem.Write(c, fiber.StatusNotFound, "product_not_found",
				"Product not found", "No product exists for this ID.")
		case errors.Is(err, ErrInvalidTransition):
			return fiberproblem.Write(c, fiber.StatusConflict, "invalid_transition",
				"Invalid transition", "A product moves from draft to active to archived and never back.")
		case err != nil:
			return err
		}
		return c.JSON(product)
	}
}
