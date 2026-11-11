CREATE TABLE events (
    id              uuid PRIMARY KEY,
    slug            text NOT NULL UNIQUE,
    title           text NOT NULL,
    description     text NOT NULL DEFAULT '',
    type            text NOT NULL CHECK (type IN ('concert', 'charity', 'community')),
    status          text NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft', 'published', 'ongoing', 'completed', 'cancelled')),
    venue_name      text NOT NULL DEFAULT '',
    venue_address   text NOT NULL DEFAULT '',
    city            text NOT NULL DEFAULT '',
    country         text NOT NULL DEFAULT 'ID',
    starts_at       timestamptz NOT NULL,
    ends_at         timestamptz,
    capacity        integer NOT NULL DEFAULT 0 CHECK (capacity >= 0),
    price_min       bigint NOT NULL DEFAULT 0 CHECK (price_min >= 0),
    price_max       bigint NOT NULL DEFAULT 0 CHECK (price_max >= 0),
    currency        char(3) NOT NULL DEFAULT 'IDR',
    cover_image_url text NOT NULL DEFAULT '',
    organizer_name  text NOT NULL DEFAULT '',
    contact_email   text NOT NULL DEFAULT '',
    contact_phone   text NOT NULL DEFAULT '',
    tags            text[] NOT NULL DEFAULT '{}',
    beneficiary     text NOT NULL DEFAULT '',
    donation_target bigint NOT NULL DEFAULT 0 CHECK (donation_target >= 0),
    donation_raised bigint NOT NULL DEFAULT 0 CHECK (donation_raised >= 0),
    created_by      uuid NOT NULL,
    created_by_name text NOT NULL DEFAULT '',
    updated_by      uuid,
    published_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,

    CONSTRAINT events_time_order CHECK (ends_at IS NULL OR ends_at >= starts_at),
    CONSTRAINT events_price_order CHECK (price_max = 0 OR price_max >= price_min),
    CONSTRAINT events_charity_fields CHECK (type <> 'charity' OR beneficiary <> '')
);

CREATE INDEX events_status_idx ON events (status, starts_at) WHERE deleted_at IS NULL;
CREATE INDEX events_type_idx ON events (type) WHERE deleted_at IS NULL;
CREATE INDEX events_city_idx ON events (city) WHERE deleted_at IS NULL;
CREATE INDEX events_created_by_idx ON events (created_by) WHERE deleted_at IS NULL;
