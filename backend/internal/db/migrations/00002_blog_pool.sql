-- +goose Up
-- in_pool marks feeds from the Chinese independent blog pool
-- (timqian/chinese-independent-blogs). They exist even without subscribers.
ALTER TABLE feeds ADD COLUMN in_pool BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE feeds DROP COLUMN in_pool;
