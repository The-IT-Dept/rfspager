-- Create the capcode_suggestions table.
--
-- 018_capcode_suggestions_extend.sql was written on the assumption that this
-- table "exists from the Laravel era". It does not: there is no Laravel
-- migration, model or factory for it anywhere in laravel-old, and it is absent
-- from the production database. It only ever existed as a hand-made table in a
-- development database, so 018 fails with 42P01 on any environment that did not
-- get it by hand — which aborts Migrate() and stops the server booting.
--
-- This creates the base shape 018 expects to extend (018 then adds `note` and
-- `capcode_id`). Idempotent, so environments that already have the hand-made
-- table are left untouched.

CREATE TABLE IF NOT EXISTS capcode_suggestions (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address    VARCHAR(255) NOT NULL,
    alias      VARCHAR(255),
    agency     VARCHAR(255),
    icon       VARCHAR(255),
    colour     VARCHAR(255),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
