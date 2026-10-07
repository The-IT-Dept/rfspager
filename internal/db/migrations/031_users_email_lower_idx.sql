-- Login, registration and password recovery match emails case-insensitively
-- (lower(email) = lower($1)). This can't be a unique index yet: 16 pairs of
-- accounts differ only in the case of their email, created before matching
-- ignored case. Merge those first if uniqueness is ever wanted.
CREATE INDEX IF NOT EXISTS users_email_lower_idx ON users (lower(email));
