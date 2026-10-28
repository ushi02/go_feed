-- +goose Up
-- disabled_at is set when a feed is judged dead (HTTP 410, or 10 failures in a
-- row). The Worker skips it until a user refreshes or subscribes again.
-- A separate column, not in_pool, because the pool import sets in_pool back.
ALTER TABLE feeds ADD COLUMN disabled_at DATETIME(6) NULL;

-- +goose Down
ALTER TABLE feeds DROP COLUMN disabled_at;
