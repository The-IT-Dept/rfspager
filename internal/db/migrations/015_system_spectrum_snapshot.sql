-- One row per system holding the most recent waterfall slice from the
-- receiver agent. Used to pre-pump the admin/mobile spectrum stream so a
-- newly-opened detail page already has the last ~minute of history before
-- live frames start flowing.
--
-- We store the payload as jsonb (compact and easy to consume from the
-- admin web client). At ~256 bins × ~60 frames × 8 bytes ≈ 120 KB per
-- system per row — bounded, no need for time-series storage.

CREATE TABLE IF NOT EXISTS system_spectrum_snapshot (
    system_id   bigint      PRIMARY KEY REFERENCES systems(id) ON DELETE CASCADE,
    updated_at  timestamptz NOT NULL DEFAULT NOW(),
    payload     jsonb       NOT NULL
);
