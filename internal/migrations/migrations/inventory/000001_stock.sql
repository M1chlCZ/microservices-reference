CREATE TABLE stock (
    sku text PRIMARY KEY CHECK (btrim(sku) <> ''),
    available integer NOT NULL CHECK (available >= 0)
);

CREATE TABLE reservations (
    id uuid PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE reservation_items (
    reservation_id uuid NOT NULL REFERENCES reservations (id) ON DELETE CASCADE,
    sku text NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (reservation_id, sku)
);

INSERT INTO stock (sku, available) VALUES
    ('WIDGET-001', 25),
    ('WIDGET-002', 10),
    ('GADGET-100', 5);
