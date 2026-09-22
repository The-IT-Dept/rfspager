package db

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
)

// Several receivers hearing the same page post it within milliseconds of each
// other. The old check-then-act let every one of them through: production ran
// at 6.7% duplicate rows over a week on three receivers, and after cutover each
// surplus row is a surplus push.
func TestCreateMessageDedupedIsAtomicUnderConcurrency(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	systemID := seedSystemForDedupe(t, d)

	const racers = 8
	when := time.Now().UTC()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them together to maximise the overlap
			msg := &models.Message{
				SystemID:  systemID,
				Address:   "0107211",
				Message:   "FRINC TYPE: MVA TURNOUT: 72 INC: 188154-22092026",
				CreatedAt: when,
			}
			ok, err := d.CreateMessageDeduped(context.Background(), msg)
			if err != nil {
				t.Errorf("insert: %v", err)
				return
			}
			if ok {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if created != 1 {
		t.Errorf("%d of %d concurrent inserts reported success, want exactly 1", created, racers)
	}
	var rows int
	if err := d.conn.GetContext(context.Background(), &rows,
		`SELECT COUNT(*) FROM messages WHERE address = '0107211'`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d rows written for one page, want 1", rows)
	}
}

// The prefix pass matches on the first 30 characters, so two colliding rows can
// have different hashes. That is why the lock is keyed on the address alone.
func TestCreateMessageDedupedCatchesPrefixCollisionsUnderConcurrency(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	systemID := seedSystemForDedupe(t, d)

	when := time.Now().UTC()
	// Same first 30 chars, different tails => different md5.
	bodies := []string{
		"STRUCTURE FIRE 12 SMITH ST TURNOUT ALPHA",
		"STRUCTURE FIRE 12 SMITH ST TURNOUT BRAVO",
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	start := make(chan struct{})
	for _, body := range bodies {
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(b string) {
				defer wg.Done()
				<-start
				ok, err := d.CreateMessageDeduped(context.Background(), &models.Message{
					SystemID: systemID, Address: "0108811", Message: b, CreatedAt: when,
				})
				if err != nil {
					t.Errorf("insert: %v", err)
					return
				}
				if ok {
					mu.Lock()
					created++
					mu.Unlock()
				}
			}(body)
		}
	}
	close(start)
	wg.Wait()

	if created != 1 {
		t.Errorf("%d inserts succeeded, want 1 — the prefix pass should collapse both variants", created)
	}
}

func seedSystemForDedupe(t *testing.T, d *DB) int64 {
	t.Helper()
	ctx := context.Background()
	var userID int64
	if err := d.conn.GetContext(ctx, &userID,
		`INSERT INTO users (user_id, name, email, password, created_at, updated_at)
		 VALUES (gen_random_uuid(), 'dedupe', 'dedupe@example.invalid', 'x', NOW(), NOW())
		 RETURNING id`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var systemID int64
	if err := d.conn.GetContext(ctx, &systemID,
		`INSERT INTO systems (system_id, user_id, name, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, 'dedupe-system', NOW(), NOW()) RETURNING id`, userID); err != nil {
		t.Fatalf("seed system: %v", err)
	}
	return systemID
}
