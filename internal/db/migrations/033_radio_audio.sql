-- P25 voice: recorded audio and transcripts for radio_calls (internal/radio
-- AudioPipeline). Receivers stream a call's decoded voice as p25.audio
-- frames; the server assembles them into one clip when the call ends, stores
-- it in the blob store and transcribes it.
--
-- audio_url (from 032) is the API path subscribers fetch the clip from
-- (/api/v2/mobile/radio/calls/{id}/audio), never the storage location, which
-- is audio_key: a key in the configured blob store (local directory or R2).

ALTER TABLE radio_calls
    ADD COLUMN IF NOT EXISTS audio_key           TEXT,
    ADD COLUMN IF NOT EXISTS audio_content_type  TEXT,
    ADD COLUMN IF NOT EXISTS audio_ms            INT,
    ADD COLUMN IF NOT EXISTS audio_bytes         INT,
    -- Transcription bookkeeping. A clip too short to transcribe is never
    -- attempted (attempts 0, transcribed_at NULL). A successful attempt sets
    -- transcribed_at; transcript is '' when no speech was recognised.
    ADD COLUMN IF NOT EXISTS transcript_attempts SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS transcript_error    TEXT;

-- Wakes the radio SSE broker when a call starts, ends, gets its audio or gets
-- its transcript, so subscribers see each stage of a call. Payload is the
-- call id; clients upsert by id. "UPDATE OF col" fires whenever col is in the
-- SET list, so the function also checks a value actually changed.
CREATE OR REPLACE FUNCTION notify_radio_call() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT'
       OR OLD.ended_at       IS DISTINCT FROM NEW.ended_at
       OR OLD.audio_url      IS DISTINCT FROM NEW.audio_url
       OR OLD.transcript     IS DISTINCT FROM NEW.transcript
       OR OLD.transcribed_at IS DISTINCT FROM NEW.transcribed_at THEN
        PERFORM pg_notify('radio_call', NEW.id::text);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_radio_call ON radio_calls;
CREATE TRIGGER trg_notify_radio_call
    AFTER INSERT OR UPDATE OF ended_at, audio_url, transcript, transcribed_at ON radio_calls
    FOR EACH ROW EXECUTE FUNCTION notify_radio_call();
