CREATE TABLE event_reviews (
    id            uuid PRIMARY KEY,
    event_id      uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id       uuid NOT NULL,
    user_name     text NOT NULL DEFAULT '',
    rating        smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
    body          text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    photo_url     text NOT NULL DEFAULT '',
    helpful_count integer NOT NULL DEFAULT 0 CHECK (helpful_count >= 0),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,
    CONSTRAINT event_reviews_one_per_user UNIQUE (event_id, user_id)
);

CREATE INDEX event_reviews_event_idx ON event_reviews (event_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX event_reviews_recent_idx ON event_reviews (created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE event_going (
    event_id     uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL,
    display_name text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id)
);

CREATE INDEX event_going_event_idx ON event_going (event_id, created_at);
