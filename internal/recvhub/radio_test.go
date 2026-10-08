package recvhub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/p25"
)

type countingLookup struct {
	revocationLookup
	calls int
}

func (s *countingLookup) GetSystemByAPIKey(ctx context.Context, key string) (*models.System, error) {
	s.calls++
	return s.revocationLookup.GetSystemByAPIKey(ctx, key)
}

func TestRadioFramesReachOnRadioWithoutAcks(t *testing.T) {
	system := &models.System{ID: 7, APIKey: sql.NullString{String: "test-key", Valid: true}}
	store := &countingLookup{revocationLookup: revocationLookup{system: system}}
	connection := newConn(nil, nil, system)
	var got []string
	connection.OnRadio = func(_ context.Context, sys *models.System, frameType string, data json.RawMessage) error {
		if sys != system {
			t.Errorf("OnRadio got system %v", sys)
		}
		got = append(got, frameType)
		return nil
	}
	for _, typ := range []string{p25.FrameSite, p25.FrameCallStart, p25.FrameCallEnd, p25.FrameUnit, p25.FrameAudio} {
		connection.handleFrame(context.Background(), store, Frame{Type: typ, ID: "x", Data: json.RawMessage(`{}`)})
	}
	if len(got) != 5 {
		t.Fatalf("OnRadio saw %v, want all five p25 frame types", got)
	}
	if store.calls != 1 {
		t.Errorf("authorization looked up %d times for 5 frames, want 1 (cached)", store.calls)
	}
	if n := len(connection.sendCh); n != 0 {
		t.Errorf("%d frames queued back to the receiver, want none (radio frames are not acked)", n)
	}
}

func TestRadioFrameErrorKeepsSocketOpen(t *testing.T) {
	system := &models.System{ID: 7, APIKey: sql.NullString{String: "test-key", Valid: true}}
	connection := newConn(nil, nil, system)
	connection.OnRadio = func(context.Context, *models.System, string, json.RawMessage) error {
		return errors.New("bad payload")
	}
	connection.handleFrame(context.Background(), &revocationLookup{system: system},
		Frame{Type: p25.FrameCallStart, Data: json.RawMessage(`"nonsense"`)})
	select {
	case <-connection.closed:
		t.Fatal("a rejected radio frame closed the connection")
	default:
	}
}

func TestRadioFramesStopAfterRevocation(t *testing.T) {
	system := &models.System{ID: 7, APIKey: sql.NullString{String: "test-key", Valid: true}}
	store := &revocationLookup{system: system}
	connection := newConn(nil, nil, system)
	accepted := 0
	connection.OnRadio = func(context.Context, *models.System, string, json.RawMessage) error { accepted++; return nil }
	frame := Frame{Type: p25.FrameUnit, Data: json.RawMessage(`{}`)}

	connection.handleFrame(context.Background(), store, frame)
	if accepted != 1 {
		t.Fatal("valid receiver radio frame rejected")
	}
	store.revoked = true
	// Once the cached check expires the revocation is seen.
	connection.radioAuthAt = time.Now().Add(-radioAuthTTL - time.Second)
	connection.handleFrame(context.Background(), store, frame)
	if accepted != 1 {
		t.Fatal("revoked connection still stored a radio frame")
	}
	select {
	case <-connection.closed:
	default:
		t.Fatal("revoked connection not closed")
	}
}
