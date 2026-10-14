-- The Libredesk conversation for a receiver application: the thank-you email
-- on apply starts it, and the approval email (with the install command) is a
-- reply in the same thread. NULL = no conversation yet (applied before these
-- emails existed, Libredesk not configured, or the first email failed).
-- Design in docs/plans/2026-10-14-public-coverage-and-hosting.md.
ALTER TABLE systems
    ADD COLUMN IF NOT EXISTS support_conversation_uuid TEXT NULL;
