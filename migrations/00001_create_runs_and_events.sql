-- +goose Up
CREATE TABLE runs (
    id          TEXT PRIMARY KEY,
    goal        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'running', 'waiting_approval',
                                  'succeeded', 'failed', 'cancelled')),
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs(id),
    seq         INTEGER NOT NULL,
    type        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (run_id, seq)
);

CREATE INDEX idx_events_run_id_seq ON events (run_id, seq);
CREATE INDEX idx_runs_status ON runs (status);

-- +goose Down
DROP TABLE events;
DROP TABLE runs;