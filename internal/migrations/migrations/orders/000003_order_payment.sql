ALTER TABLE orders
    ADD COLUMN status text NOT NULL DEFAULT 'pending_payment'
        CHECK (status IN ('pending_payment', 'paid')),
    ADD COLUMN payment_intent_id uuid,
    ADD COLUMN provider_ref text;
