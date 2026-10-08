package radio

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/p25"
)

// maxAudioChunk bounds one p25.audio chunk's PCM (8 s at 8 kHz). Receivers
// send fractions of a second; this only stops a bad frame hogging memory.
const maxAudioChunk = 128 << 10

// Ingest is the receiver-facing entry point for p25.* frames: it stores
// call, site and unit frames through Store, and assembles p25.audio into
// recordings (Assembler → AudioPipeline) while forwarding it live
// (LiveBroker). Its HandleFrame matches recvhub.RadioHandler.
//
// Everything audio does on the websocket read loop is in memory and
// non-blocking, apart from one indexed lookup per call to tie its audio to
// its row. Storage and transcription happen on the pipeline's workers.
type Ingest struct {
	store *Store
	asm   *Assembler
	live  *LiveBroker
	pipe  *AudioPipeline // nil: audio is forwarded live but not recorded
	now   func() time.Time
}

func NewIngest(store *Store, asm *Assembler, live *LiveBroker, pipe *AudioPipeline) *Ingest {
	return &Ingest{store: store, asm: asm, live: live, pipe: pipe, now: time.Now}
}

// sweepInterval is how often calls that never ended are looked for.
const sweepInterval = 5 * time.Second

// Run runs the audio pipeline and evicts abandoned calls until ctx is
// cancelled. Calls still buffered then are lost.
func (in *Ingest) Run(ctx context.Context) {
	done := make(chan struct{})
	if in.pipe != nil {
		go func() { defer close(done); in.pipe.Run(ctx) }()
	} else {
		close(done)
	}
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			<-done
			return
		case <-t.C:
			for _, clip := range in.asm.Sweep(in.now()) {
				slog.Info("radio audio: call never ended, saving what we have",
					"call_uid", clip.UID, "bytes", len(clip.PCM))
				in.finish(clip)
			}
		}
	}
}

// HandleFrame stores or assembles one p25.* frame.
func (in *Ingest) HandleFrame(ctx context.Context, sys *models.System, frameType string, data json.RawMessage) error {
	var receiverID int64
	if sys != nil {
		receiverID = sys.ID
	}
	switch frameType {
	case p25.FrameAudio:
		return in.handleAudio(ctx, data)
	case p25.FrameCallStart:
		var p p25.CallPayload
		if err := decode(data, &p); err != nil {
			return err
		}
		if _, err := in.store.StartCall(ctx, receiverID, p); err != nil {
			return err
		}
		// Encrypted calls have no voice for us; don't track them.
		if !p.Encrypted {
			meta, err := in.store.callMeta(ctx, p.CallID)
			if err != nil {
				slog.Warn("radio audio: look up started call", "call_uid", p.CallID, "error", err)
			}
			if meta.PatchOf != 0 {
				// A copy of a call we're already recording: close it now
				// so its audio is dropped as it arrives.
				in.asm.Finish(p.CallID, in.now())
				return nil
			}
			in.asm.Begin(p.CallID, meta, in.now())
		}
		return nil
	case p25.FrameCallEnd:
		var p p25.CallPayload
		if err := decode(data, &p); err != nil {
			return err
		}
		_, err := in.store.EndCall(ctx, receiverID, p)
		// Finish whatever happened to the row: the audio is already here,
		// and the pipeline looks the row up again before storing.
		clip := in.asm.Finish(p.CallID, in.now())
		if clip != nil {
			// Only now known to be a copy (its talker arrived with the end).
			if meta, merr := in.store.callMeta(ctx, p.CallID); merr == nil && meta.PatchOf != 0 {
				clip = nil
			}
		}
		in.finish(clip)
		return err
	default:
		return in.store.HandleFrame(ctx, sys, frameType, data)
	}
}

func (in *Ingest) handleAudio(ctx context.Context, data json.RawMessage) error {
	var p p25.AudioPayload
	if err := decode(data, &p); err != nil {
		return err
	}
	switch {
	case p.CallID == "" || len(p.CallID) > maxCallUIDLen:
		return fmt.Errorf("%w: audio with a bad call_id", ErrInvalidPayload)
	case p.Seq < 0:
		return fmt.Errorf("%w: audio with negative seq", ErrInvalidPayload)
	case len(p.PCM) == 0:
		return nil
	case len(p.PCM)%2 != 0 || len(p.PCM) > maxAudioChunk:
		return fmt.Errorf("%w: audio chunk of %d bytes", ErrInvalidPayload, len(p.PCM))
	}
	res := in.asm.Append(p.CallID, p.Seq, p.PCM, in.now())
	if res.New {
		// Audio for a call whose start we never saw (we restarted, say).
		// Its row may exist anyway; tie it up once so it can go out live.
		if meta, err := in.store.callMeta(ctx, p.CallID); err == nil {
			in.asm.SetMeta(p.CallID, meta)
			res.Meta = meta
		} else if !IsNotFound(err) {
			slog.Warn("radio audio: look up call", "call_uid", p.CallID, "error", err)
		}
	}
	if res.Accepted {
		in.live.PublishAudio(res.Meta, p.CallID, p.Seq, p.PCM)
	}
	return nil
}

// finish tells live listeners a call is over and queues its clip, if any.
func (in *Ingest) finish(clip *Clip) {
	if clip == nil {
		return
	}
	in.live.PublishEnd(clip.Meta, clip.UID)
	if in.pipe != nil {
		in.pipe.Enqueue(clip)
	}
}
