-- Deduplicate existing rows and add the uniqueness the application already
-- assumes. Both writes below are check-then-act with no constraint behind
-- them, so concurrent (and, for subscriptions/filters, even repeated) writes
-- produce duplicate rows.
--
-- subscriptions/filters use `ON CONFLICT DO NOTHING`, which silently does
-- nothing unless a matching unique index exists; their (user_id, capcode_id)
-- indexes are non-unique, so a user double-tapping Subscribe stored the
-- capcode twice and DELETE removed both copies behind a single toggle.
--
-- responding is created by a SELECT EXISTS + INSERT/DELETE toggle with no
-- transaction, so concurrent toggles all observe "not responding" and all
-- insert. A single user then appears N times in the responder list and
-- inflates responder_count.

-- Preserve the lowest responding.id per (message_id, user_id); the delete
-- cascade mirrors the existing FK.
DELETE FROM responding r
 USING responding keep
 WHERE r.message_id = keep.message_id
   AND r.user_id = keep.user_id
   AND r.id > keep.id;

ALTER TABLE responding
    ADD CONSTRAINT responding_message_id_user_id_key UNIQUE (message_id, user_id);

DELETE FROM subscriptions s
 USING subscriptions keep
 WHERE s.user_id = keep.user_id
   AND s.capcode_id = keep.capcode_id
   AND s.id > keep.id;

CREATE UNIQUE INDEX subscriptions_user_id_capcode_id_key
    ON subscriptions (user_id, capcode_id);

DELETE FROM filters f
 USING filters keep
 WHERE f.user_id = keep.user_id
   AND f.capcode_id = keep.capcode_id
   AND f.id > keep.id;

CREATE UNIQUE INDEX filters_user_id_capcode_id_key
    ON filters (user_id, capcode_id);
