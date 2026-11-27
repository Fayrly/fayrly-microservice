ALTER TABLE event_ticket_orders
    ADD COLUMN IF NOT EXISTS payment_method text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS va_number      text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS qr_url         text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS deeplink_url   text NOT NULL DEFAULT '';
