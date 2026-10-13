package recvhub

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

type fakeStatsStore struct {
	inserted [][]byte
	updated  [][]byte
	recent   []db.SystemStatsRow
}

func (f *fakeStatsStore) InsertSystemStats(_ context.Context, _ int64, _ time.Time, stats []byte) error {
	f.inserted = append(f.inserted, stats)
	return nil
}

func (f *fakeStatsStore) UpdateSystemConnectionStats(_ context.Context, _ int64, stats []byte, _ time.Time) error {
	f.updated = append(f.updated, stats)
	return nil
}

func (f *fakeStatsStore) RecentSystemStats(_ context.Context, _ int64, _ int) ([]db.SystemStatsRow, error) {
	return f.recent, nil
}

type statsEnvelope struct {
	TS    int64         `json:"ts"`
	Stats rxproto.Stats `json:"stats"`
}

func TestStatsBrokerPersistsAndFansOutEnvelope(t *testing.T) {
	store := &fakeStatsStore{}
	b := NewStatsBroker(store)
	ch, cancel := b.Subscribe(7)
	other, cancelOther := b.Subscribe(8)
	defer cancelOther()

	before := time.Now().UnixMilli()
	b.Publish(context.Background(), 7, rxproto.Stats{
		IntervalS: 15,
		Dongle:    rxproto.DongleStats{Open: true, GainTenthsDB: 280},
		Channels:  []rxproto.ChannelStats{{Label: "RFS", SNRDB: 12.5}},
	})

	var env statsEnvelope
	select {
	case raw := <-ch:
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("envelope %s: %v", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber received nothing")
	}
	if env.TS < before || env.Stats.Dongle.GainTenthsDB != 280 || len(env.Stats.Channels) != 1 || env.Stats.Channels[0].Label != "RFS" {
		t.Fatalf("envelope = %+v", env)
	}
	select {
	case raw := <-other:
		t.Fatalf("system 8 subscriber received system 7 stats: %s", raw)
	default:
	}
	if len(store.inserted) != 1 || len(store.updated) != 1 || !bytes.Equal(store.inserted[0], store.updated[0]) {
		t.Fatalf("store writes: inserted=%d updated=%d", len(store.inserted), len(store.updated))
	}

	cancel()
	if _, open := <-ch; open {
		t.Fatal("cancel did not close the subscriber channel")
	}
	// Publishing after the last subscriber left still persists.
	b.Publish(context.Background(), 7, rxproto.Stats{IntervalS: 15})
	if len(store.inserted) != 2 {
		t.Fatalf("inserted = %d after unsubscribed publish", len(store.inserted))
	}
}

func TestStatsBrokerDropsForSlowSubscribers(t *testing.T) {
	b := NewStatsBroker(&fakeStatsStore{})
	ch, cancel := b.Subscribe(1)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			b.Publish(context.Background(), 1, rxproto.Stats{IntervalS: i})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a slow subscriber")
	}
	if n := len(ch); n != cap(ch) {
		t.Fatalf("buffered %d envelopes, want the full buffer %d", n, cap(ch))
	}
}

func TestStatsBrokerRecentWrapsStoredRows(t *testing.T) {
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	store := &fakeStatsStore{recent: []db.SystemStatsRow{
		{TS: at, Stats: []byte(`{"interval_s":15}`)},
		{TS: at.Add(15 * time.Second), Stats: nil},
	}}
	envelopes, err := NewStatsBroker(store).Recent(context.Background(), 1, 60)
	if err != nil || len(envelopes) != 2 {
		t.Fatalf("recent = %d err=%v", len(envelopes), err)
	}
	var env struct {
		TS    int64           `json:"ts"`
		Stats json.RawMessage `json:"stats"`
	}
	if err := json.Unmarshal(envelopes[0], &env); err != nil || env.TS != at.UnixMilli() || string(env.Stats) != `{"interval_s":15}` {
		t.Fatalf("first = %s (%v)", envelopes[0], err)
	}
	if err := json.Unmarshal(envelopes[1], &env); err != nil || string(env.Stats) != "null" {
		t.Fatalf("empty stats should serialise as null: %s (%v)", envelopes[1], err)
	}
}
