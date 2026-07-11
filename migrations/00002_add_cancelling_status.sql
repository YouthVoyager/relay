-- +goose Up
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
  CHECK (status IN ('pending','running','cancelling','waiting_approval',
                    'succeeded','failed','cancelled'));

-- +goose Down
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
  CHECK (status IN ('pending','running','waiting_approval',
                    'succeeded','failed','cancelled'));