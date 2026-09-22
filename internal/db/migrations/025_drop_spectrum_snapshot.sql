-- Drop the waterfall snapshot table.
--
-- The spectrum/waterfall feature is gone: the receiver no longer computes the
-- per-buffer FFT or sends spectrum frames, and the admin, mobile and public
-- stream endpoints that consumed them have been removed. Migration 015 created
-- this table to pre-pump a newly-opened detail page with the last ~minute of
-- history; with nothing producing or reading frames it is dead weight (up to
-- ~120 KB of jsonb per system).
--
-- Safe to drop outright: it held only a transient display cache, never a
-- source of record.

DROP TABLE IF EXISTS system_spectrum_snapshot;
