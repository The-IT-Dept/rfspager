-- Give every Pagermon receiver a system row of its own.
--
-- Migration 011 seeded the five Pagermon API keys, but it attached them by
-- name (LOWER(name) LIKE 'raymond-terrace-1%' and friends). In production only
-- one of those patterns matches anything: `systems` holds just RFSPager.com,
-- hrng.io and RFSPAGER-1 (FRNSW 148.9625M). Four of the five keys therefore
-- stayed NULL, and a receiver presenting one of them gets a 401 from
-- ProcessPagermon — including raymond-terrace-1, which is the only receiver
-- currently transmitting.
--
-- Today every Pagermon receiver is funnelled through Pagermon's SimpleWebhook
-- into the single RFSPAGER-1 system, so per-receiver attribution is lost. One
-- row per key restores it. `systems.api_key` is uniquely indexed, so one key
-- per row is the only shape available anyway.
--
-- Idempotent three ways: skip if the key is already attached, adopt a
-- matching key-less row if one exists, otherwise insert.

DO $$
DECLARE
    owner_id  BIGINT;
    rec       RECORD;
    target_id BIGINT;
BEGIN
    -- Inherit the owner of the existing systems rather than assuming user 1.
    SELECT user_id INTO owner_id FROM systems WHERE deleted_at IS NULL ORDER BY id LIMIT 1;
    IF owner_id IS NULL THEN
        SELECT id INTO owner_id FROM users ORDER BY id LIMIT 1;
    END IF;
    IF owner_id IS NULL THEN
        RAISE NOTICE 'no users present; skipping Pagermon receiver seeding';
        RETURN;
    END IF;

    FOR rec IN
        SELECT * FROM (VALUES
            ('rfspager-2 (Ash - Illawarra)',           '7Q030VBGGDG07MYFKNCERYC0', 'cedee83a-fced-43de-b262-58deba31a2f0'::uuid),
            ('rfspager-3 (bellbirdweb - The Hills)',   '5IC65K95TDW07V9CW3ROF0K0', '5033b5be-42d2-4aeb-b46a-cf42aca30766'::uuid),
            ('raymond-terrace-1',                      '3II74R1MFF8014VR5ZHVPWLC', '7d994c06-e066-4e85-a51b-42538eec89d4'::uuid),
            ('narrabeen-1 (tweekin)',                  '84SAODIIRVW03JHOJTUSP720', '8c35cda8-afc7-4c3b-aa6b-249cc32eeef5'::uuid)
        ) AS t(name, api_key, system_uuid)
    LOOP
        PERFORM 1 FROM systems WHERE api_key = rec.api_key;
        IF FOUND THEN
            CONTINUE;
        END IF;

        SELECT id INTO target_id
          FROM systems
         WHERE deleted_at IS NULL AND api_key IS NULL AND LOWER(name) = LOWER(rec.name)
         ORDER BY id LIMIT 1;

        IF target_id IS NOT NULL THEN
            UPDATE systems
               SET api_key = rec.api_key, updated_at = NOW()
             WHERE id = target_id;
        ELSE
            INSERT INTO systems
                (system_id, user_id, name, api_key, frequency, steal_capcodes,
                 sync, approved_at, created_at, updated_at)
            VALUES
                (rec.system_uuid, owner_id, rec.name, rec.api_key, 1, '0',
                 false, NOW(), NOW(), NOW());
        END IF;
    END LOOP;
END $$;
