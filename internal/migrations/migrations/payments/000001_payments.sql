CREATE TABLE payment_intents (
    id uuid PRIMARY KEY,
    order_id uuid NOT NULL UNIQUE,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency text NOT NULL CHECK (currency = upper(currency) AND length(currency) = 3),
    status text NOT NULL CHECK (status IN ('requires_payment', 'captured', 'refunded')),
    provider_ref text NOT NULL UNIQUE,
    idempotency_key text NOT NULL UNIQUE CHECK (btrim(idempotency_key) <> ''),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- shortcut: this schema mirrors pgoutbox; update it when pgoutbox changes its schema.
CREATE TABLE outbox_jobs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    generation integer NOT NULL DEFAULT 0,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    scheduled_at timestamptz,
    leased_at timestamptz,
    lease_until timestamptz,
    leased_by text,
    processed_at timestamptz,
    dead_lettered_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outbox_jobs_kind_present CHECK (btrim(kind) <> ''),
    CONSTRAINT outbox_jobs_aggregate_present CHECK (btrim(aggregate_type) <> ''),
    CONSTRAINT outbox_jobs_generation_valid CHECK (generation >= 0),
    CONSTRAINT outbox_jobs_payload_valid CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT outbox_jobs_attempts_valid CHECK (attempts >= 0),
    CONSTRAINT outbox_jobs_lease_valid CHECK (
        (leased_at IS NULL) = (leased_by IS NULL)
        AND (lease_until IS NULL) = (leased_by IS NULL)
        AND (lease_until IS NULL OR lease_until > leased_at)
    ),
    CONSTRAINT outbox_jobs_processed_valid CHECK (processed_at IS NULL OR processed_at >= created_at),
    CONSTRAINT outbox_jobs_dead_lettered_valid CHECK (
        dead_lettered_at IS NULL
        OR (
            processed_at IS NOT NULL
            AND leased_at IS NULL
            AND lease_until IS NULL
            AND leased_by IS NULL
            AND dead_lettered_at >= created_at
        )
    ),
    CONSTRAINT outbox_jobs_aggregate_generation_unique UNIQUE (
        kind, aggregate_type, aggregate_id, generation
    )
);

CREATE INDEX outbox_jobs_due_idx
    ON outbox_jobs (available_at, lease_until, id)
    WHERE processed_at IS NULL;

CREATE INDEX outbox_jobs_dead_idx
    ON outbox_jobs (dead_lettered_at, id)
    WHERE dead_lettered_at IS NOT NULL;
