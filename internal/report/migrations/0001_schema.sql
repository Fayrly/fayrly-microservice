CREATE TABLE reports (
    id               uuid PRIMARY KEY,
    title            text NOT NULL,
    type             text NOT NULL CHECK (type IN ('financial', 'attendance', 'incident', 'impact', 'other')),
    event_id         uuid,
    event_title      text NOT NULL DEFAULT '',
    period_start     date,
    period_end       date,
    summary          text NOT NULL DEFAULT '',
    metrics          jsonb NOT NULL DEFAULT '{}'::jsonb,
    status           text NOT NULL DEFAULT 'draft'
                     CHECK (status IN ('draft', 'submitted', 'approved', 'rejected')),
    created_by       uuid NOT NULL,
    created_by_name  text NOT NULL DEFAULT '',
    submitted_at     timestamptz,
    reviewed_by      uuid,
    reviewed_by_name text NOT NULL DEFAULT '',
    reviewed_at      timestamptz,
    review_notes     text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz,

    CONSTRAINT reports_period_order CHECK (
        period_start IS NULL OR period_end IS NULL OR period_end >= period_start
    )
);

CREATE INDEX reports_status_idx ON reports (status, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX reports_event_idx ON reports (event_id) WHERE deleted_at IS NULL AND event_id IS NOT NULL;
CREATE INDEX reports_created_by_idx ON reports (created_by) WHERE deleted_at IS NULL;
