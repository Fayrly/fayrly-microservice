CREATE TABLE event_ticket_types (
    id          uuid PRIMARY KEY,
    event_id    uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    price       bigint NOT NULL CHECK (price > 0),
    capacity    integer NOT NULL CHECK (capacity > 0),
    sold        integer NOT NULL DEFAULT 0 CHECK (sold >= 0),
    reserved    integer NOT NULL DEFAULT 0 CHECK (reserved >= 0),
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (event_id, name),
    CHECK (sold + reserved <= capacity)
);
CREATE INDEX event_ticket_types_event_idx ON event_ticket_types (event_id, active);

CREATE TABLE event_ticket_orders (
    id                    uuid PRIMARY KEY,
    request_key           text NOT NULL,
    event_id              uuid NOT NULL REFERENCES events(id),
    ticket_type_id        uuid NOT NULL REFERENCES event_ticket_types(id),
    user_id               uuid NOT NULL,
    event_title           text NOT NULL,
    ticket_name           text NOT NULL,
    quantity              integer NOT NULL CHECK (quantity > 0),
    unit_price            bigint NOT NULL CHECK (unit_price > 0),
    gross_amount          bigint NOT NULL CHECK (gross_amount > 0),
    currency              char(3) NOT NULL,
    status                text NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'paid', 'failed', 'expired', 'cancelled')),
    midtrans_transaction_id text NOT NULL DEFAULT '',
    payment_type          text NOT NULL DEFAULT '',
    snap_token            text NOT NULL DEFAULT '',
    redirect_url          text NOT NULL DEFAULT '',
    expires_at            timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_ticket_orders_user_idx ON event_ticket_orders (user_id, created_at DESC);
CREATE INDEX event_ticket_orders_status_idx ON event_ticket_orders (status, created_at);
CREATE UNIQUE INDEX event_ticket_orders_idempotency_idx ON event_ticket_orders (user_id, request_key);
