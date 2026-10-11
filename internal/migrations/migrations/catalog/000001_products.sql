CREATE TABLE products (
    id uuid PRIMARY KEY,
    sku text NOT NULL UNIQUE CHECK (btrim(sku) <> ''),
    name text NOT NULL CHECK (btrim(name) <> ''),
    price_minor bigint NOT NULL CHECK (price_minor >= 0),
    status text NOT NULL CHECK (status IN ('draft', 'active', 'archived')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
