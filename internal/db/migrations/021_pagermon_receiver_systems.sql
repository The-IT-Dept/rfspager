-- Reserved migration number: prior private builds seeded operational receivers.
-- Receiver rows and credentials must now be provisioned explicitly at cutover.
-- Existing databases are intentionally unchanged here; rotate their receiver
-- credentials before deploying the Go server or publishing any history.
SELECT 1;
