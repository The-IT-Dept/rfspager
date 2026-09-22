-- A deleted account may have contributed system history that remains useful to
-- other users. Disown those systems rather than deleting shared history.
ALTER TABLE systems
    ALTER COLUMN user_id DROP NOT NULL;
