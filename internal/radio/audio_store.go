package radio

import (
	"context"
)

// Database side of call audio (migration 033).

// StoredAudio is where a call's recording was put.
type StoredAudio struct {
	Key         string // blob store key
	ContentType string
	URL         string // API path, see AudioURL
	MS          int
	Bytes       int
}

// CallIDByUID returns the id of the call with the receiver's call_id, or
// sql.ErrNoRows.
func (s *Store) CallIDByUID(ctx context.Context, uid string) (int64, error) {
	var id int64
	err := s.db.GetContext(ctx, &id, `SELECT id FROM radio_calls WHERE call_uid = $1`, uid)
	return id, err
}

// callMeta looks up the row a call's audio belongs to, for the live stream.
func (s *Store) callMeta(ctx context.Context, uid string) (CallMeta, error) {
	var row struct {
		ID      int64 `db:"id"`
		TGID    int   `db:"tgid"`
		Hidden  bool  `db:"hidden"`
		PatchOf int64 `db:"patch_of"`
	}
	err := s.db.GetContext(ctx, &row,
		`SELECT c.id, COALESCE(c.tgid, 0) AS tgid, COALESCE(tg.hidden, false) AS hidden,
		        COALESCE(c.patch_of, 0) AS patch_of
		   FROM radio_calls c
		   LEFT JOIN radio_talkgroups tg ON tg.sysid = c.sysid AND tg.tgid = c.tgid
		  WHERE c.call_uid = $1`, uid)
	if err != nil {
		return CallMeta{}, err
	}
	return CallMeta{Known: true, ID: row.ID, TGID: row.TGID, Hidden: row.Hidden, PatchOf: row.PatchOf}, nil
}

// SetCallAudio records a call's stored recording. Changing audio_url fires
// the radio_call NOTIFY, so subscribers get the call again with it.
func (s *Store) SetCallAudio(ctx context.Context, id int64, a StoredAudio) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE radio_calls
		    SET audio_key = $2, audio_content_type = $3, audio_url = $4, audio_ms = $5, audio_bytes = $6
		  WHERE id = $1`,
		id, a.Key, a.ContentType, a.URL, a.MS, a.Bytes)
	return err
}

// SetTranscript records a successful transcription (empty for no speech).
func (s *Store) SetTranscript(ctx context.Context, id int64, text string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE radio_calls
		    SET transcript = $2, transcribed_at = NOW(), transcript_error = NULL,
		        transcript_attempts = LEAST(transcript_attempts + 1, 32767)
		  WHERE id = $1`, id, text)
	return err
}

// SetTranscriptError records a failed transcription. The call keeps its
// audio; transcript stays NULL.
func (s *Store) SetTranscriptError(ctx context.Context, id int64, msg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE radio_calls
		    SET transcript_error = $2, transcript_attempts = LEAST(transcript_attempts + 1, 32767)
		  WHERE id = $1`, id, msg)
	return err
}

// CallAudio is a call's recording as the audio endpoint needs it.
type CallAudio struct {
	Key         string `db:"audio_key"`
	ContentType string `db:"audio_content_type"`
	Bytes       int    `db:"audio_bytes"`
}

// GetCallAudio returns sql.ErrNoRows for an unknown call, one on a hidden
// talkgroup, or one without a recording.
func (s *Store) GetCallAudio(ctx context.Context, id int64) (*CallAudio, error) {
	var a CallAudio
	err := s.db.GetContext(ctx, &a,
		`SELECT c.audio_key, COALESCE(c.audio_content_type, '') AS audio_content_type,
		        COALESCE(c.audio_bytes, 0) AS audio_bytes
		   FROM radio_calls c
		   LEFT JOIN radio_talkgroups tg ON tg.sysid = c.sysid AND tg.tgid = c.tgid
		  WHERE c.id = $1 AND c.audio_key IS NOT NULL AND NOT COALESCE(tg.hidden, false)`, id)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
