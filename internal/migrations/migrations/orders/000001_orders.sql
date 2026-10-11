CREATE TABLE orders (
    id uuid PRIMARY KEY,
    customer_ref text NOT NULL CHECK (btrim(customer_ref) <> ''),
    total_minor bigint NOT NULL CHECK (total_minor >= 0),
    created_at timestamptz NOT NULL
);

CREATE TABLE order_items (
    order_id uuid NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    sku text NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
    PRIMARY KEY (order_id, sku)
);
