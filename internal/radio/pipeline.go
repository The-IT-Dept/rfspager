package radio

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/the-it-dept/rfspager/internal/blobstore"
	"github.com/the-it-dept/rfspager/internal/p25"
)

// AudioURL is the API path a call's recording is served from (see
// mobile.RadioRouter, mounted at /api/v2/mobile/radio). It is what
// radio_calls.audio_url holds: subscribers never see storage locations.
func AudioURL(callID int64) string {
	return fmt.Sprintf("/api/v2/mobile/radio/calls/%d/audio", callID)
}

// Transcriber turns a WAV file into text; *asr.Client implements it.
type Transcriber interface {
	Transcribe(ctx context.Context, filename string, audio []byte) (string, error)
}

// audioRecorder is the database side of the pipeline; *Store implements it.
type audioRecorder interface {
	CallIDByUID(ctx context.Context, uid string) (int64, error)
	SetCallAudio(ctx context.Context, id int64, a StoredAudio) error
	SetTranscript(ctx context.Context, id int64, text string) error
	SetTranscriptError(ctx context.Context, id int64, msg string) error
}

type PipelineConfig struct {
	Blobs   blobstore.Store
	Encoder AudioEncoder // default WAVEncoder
	ASR     Transcriber  // nil: no transcription

	StoreWorkers int // default 2
	ASRWorkers   int // default 2
	QueueSize    int // per stage; default 64
	// Clips shorter than this aren't worth transcribing (key-ups, squelch
	// tails). Default 500ms.
	MinTranscribe time.Duration
	// StoreTimeout bounds storing one clip (blob put + row update).
	// Default 60s. Transcription has its own timeouts in the ASR client.
	StoreTimeout time.Duration
}

// AudioPipeline stores finished clips and transcribes them, off the
// websocket read loop. Two bounded stages, each with its own workers, so a
// slow ASR endpoint never holds up recordings landing. A full queue drops
// the clip (logged) rather than blocking ingest. Queued work is in memory
// and lost on restart.
type AudioPipeline struct {
	db  audioRecorder
	cfg PipelineConfig

	clips chan *Clip
	asr   chan asrJob
}

type asrJob struct {
	callID int64
	wav    []byte
}

func NewAudioPipeline(store *Store, cfg PipelineConfig) *AudioPipeline {
	return newAudioPipeline(store, cfg)
}

func newAudioPipeline(db audioRecorder, cfg PipelineConfig) *AudioPipeline {
	if cfg.Encoder == nil {
		cfg.Encoder = WAVEncoder{}
	}
	if cfg.StoreWorkers <= 0 {
		cfg.StoreWorkers = 2
	}
	if cfg.ASRWorkers <= 0 {
		cfg.ASRWorkers = 2
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 64
	}
	if cfg.MinTranscribe <= 0 {
		cfg.MinTranscribe = 500 * time.Millisecond
	}
	if cfg.StoreTimeout <= 0 {
		cfg.StoreTimeout = 60 * time.Second
	}
	return &AudioPipeline{
		db:    db,
		cfg:   cfg,
		clips: make(chan *Clip, cfg.QueueSize),
		asr:   make(chan asrJob, cfg.QueueSize),
	}
}

// Enqueue hands a clip to the store stage without blocking. False if the
// queue is full and the clip was dropped.
func (p *AudioPipeline) Enqueue(c *Clip) bool {
	select {
	case p.clips <- c:
		return true
	default:
		slog.Warn("radio audio: store queue full, dropping clip", "call_uid", c.UID, "bytes", len(c.PCM))
		return false
	}
}

// Run starts the workers and blocks until ctx is cancelled and they stop.
func (p *AudioPipeline) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < p.cfg.StoreWorkers; i++ {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case c := <-p.clips:
					if err := p.store(ctx, c); err != nil {
						slog.Warn("radio audio: store failed", "call_uid", c.UID, "error", err)
					}
				}
			}
		})
	}
	if p.cfg.ASR != nil {
		for i := 0; i < p.cfg.ASRWorkers; i++ {
			wg.Go(func() {
				for {
					select {
					case <-ctx.Done():
						return
					case j := <-p.asr:
						p.transcribe(ctx, j)
					}
				}
			})
		}
	}
	wg.Wait()
}

// store encodes a clip, puts it in the blob store, points the call row at
// it and queues it for transcription.
func (p *AudioPipeline) store(ctx context.Context, c *Clip) error {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.StoreTimeout)
	defer cancel()

	id := c.Meta.ID
	if id == 0 {
		// Never tied to a row while buffering (no call_start seen); its
		// call_end has inserted one since, unless the call was evicted.
		var err error
		if id, err = p.db.CallIDByUID(ctx, c.UID); err != nil {
			if IsNotFound(err) {
				return fmt.Errorf("no call row for %d bytes of audio", len(c.PCM))
			}
			return err
		}
	}
	enc, err := p.cfg.Encoder.Encode(c.PCM, p25.AudioSampleRate)
	if err != nil {
		return err
	}
	date := c.StartedAt
	if date.IsZero() {
		date = time.Now()
	}
	key := fmt.Sprintf("radio/%s/%d%s", date.UTC().Format("2006/01/02"), id, enc.Ext)
	if _, err := p.cfg.Blobs.Put(ctx, key, enc.ContentType, bytes.NewReader(enc.Data), int64(len(enc.Data))); err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	ms := pcmDurationMS(len(c.PCM), p25.AudioSampleRate)
	if err := p.db.SetCallAudio(ctx, id, StoredAudio{
		Key: key, ContentType: enc.ContentType, URL: AudioURL(id), MS: ms, Bytes: len(enc.Data),
	}); err != nil {
		return err
	}
	slog.Debug("radio audio stored", "call", id, "ms", ms, "chunks", c.Chunks, "missing", c.Missing,
		"truncated", c.Truncated, "evicted", c.Evicted)

	if p.cfg.ASR == nil || time.Duration(ms)*time.Millisecond < p.cfg.MinTranscribe {
		return nil
	}
	wav := enc.Data
	if enc.ContentType != "audio/wav" {
		wav = EncodeWAV(c.PCM, p25.AudioSampleRate)
	}
	select {
	case p.asr <- asrJob{callID: id, wav: wav}:
	default:
		slog.Warn("radio audio: transcription queue full, skipping", "call", id)
		_ = p.db.SetTranscriptError(ctx, id, "transcription queue full")
	}
	return nil
}

func (p *AudioPipeline) transcribe(ctx context.Context, j asrJob) {
	text, err := p.cfg.ASR.Transcribe(ctx, fmt.Sprintf("call-%d.wav", j.callID), j.wav)
	if ctx.Err() != nil {
		return // shutting down; leave the call untranscribed
	}
	// The ASR client has its own deadlines; this bounds the row update.
	dbctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err != nil {
		slog.Warn("radio audio: transcription failed", "call", j.callID, "error", err)
		msg := err.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if err := p.db.SetTranscriptError(dbctx, j.callID, msg); err != nil {
			slog.Warn("radio audio: record transcription error", "call", j.callID, "error", err)
		}
		return
	}
	if err := p.db.SetTranscript(dbctx, j.callID, text); err != nil {
		slog.Warn("radio audio: store transcript", "call", j.callID, "error", err)
	}
}
