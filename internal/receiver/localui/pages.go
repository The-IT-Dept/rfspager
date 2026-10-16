package localui

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// Page is one decode as the local UI and webhooks see it: posted pages and
// the ones the content filter dropped, so the host can see the noise too.
type Page struct {
	ID       int64                `json:"id"`
	TS       int64                `json:"ts"` // unix ms
	Channel  string               `json:"channel"`
	FreqHz   uint32               `json:"freq_hz"`
	Baud     int                  `json:"baud"`
	Address  string               `json:"address"`
	Function int                  `json:"function"`
	Message  string               `json:"message"`
	Posted   bool                 `json:"posted"`
	Quality  *rxproto.PageQuality `json:"quality,omitempty"`
}

const (
	pagesFile = "pages.jsonl"
	// pagesKeep is how many pages stay in memory and survive a compaction;
	// the file is rewritten to that many once it reaches pagesCompactAt.
	pagesKeep      = 3000
	pagesCompactAt = 6000
)

// PageStore keeps the newest pages in a ring and appends each to
// pages.jsonl so the history survives a restart. Persistence fails soft:
// without a state dir the store is memory-only.
type PageStore struct {
	mu     sync.Mutex
	ring   []Page // oldest first, at most pagesKeep
	nextID int64
	path   string
	lines  int // lines in the file, for compaction
	file   *os.File

	// today counts posted pages since local midnight.
	day        string
	todayCount int
	lastPosted int64
}

// NewPageStore loads the history from dir (if any) and opens it for append.
func NewPageStore(dir string) *PageStore {
	s := &PageStore{nextID: 1}
	if dir == "" {
		return s
	}
	s.path = filepath.Join(dir, pagesFile)
	s.load()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		slog.Warn("page history not persisted", "path", s.path, "err", err)
		s.path = ""
		return s
	}
	s.file = f
	return s
}

func (s *PageStore) load() {
	f, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var p Page
		if json.Unmarshal(sc.Bytes(), &p) != nil {
			continue
		}
		s.lines++
		s.push(p)
		if p.ID >= s.nextID {
			s.nextID = p.ID + 1
		}
	}
}

// push appends to the ring and the day counter. Caller holds mu (or owns s).
func (s *PageStore) push(p Page) {
	s.ring = append(s.ring, p)
	if len(s.ring) > pagesKeep {
		s.ring = append(s.ring[:0:0], s.ring[len(s.ring)-pagesKeep:]...)
	}
	if !p.Posted {
		return
	}
	day := time.UnixMilli(p.TS).Format(time.DateOnly)
	if day != s.day {
		s.day, s.todayCount = day, 0
	}
	s.todayCount++
	s.lastPosted = p.TS
}

// Add assigns the page an id, stores it and returns it.
func (s *PageStore) Add(p Page) Page {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.ID = s.nextID
	s.nextID++
	s.push(p)
	if s.file != nil {
		b, _ := json.Marshal(p)
		if _, err := s.file.Write(append(b, '\n')); err != nil {
			slog.Warn("page history write failed; continuing memory-only", "err", err)
			s.file.Close()
			s.file = nil
		} else if s.lines++; s.lines >= pagesCompactAt {
			s.compactLocked()
		}
	}
	return p
}

// compactLocked rewrites the file to the in-memory ring via a temp file.
func (s *PageStore) compactLocked() {
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		slog.Warn("page history compaction failed", "err", err)
		return
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, p := range s.ring {
		_ = enc.Encode(p)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		slog.Warn("page history compaction failed", "err", err)
		return
	}
	f.Close()
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		slog.Warn("page history compaction failed", "err", err)
		return
	}
	s.file.Close()
	s.file, err = os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		slog.Warn("page history reopen failed; continuing memory-only", "err", err)
		s.file = nil
	}
	s.lines = len(s.ring)
}

// PageQuery selects pages for the API.
type PageQuery struct {
	Limit    int
	Before   int64 // 0 = newest
	Filtered bool  // include posted=false
	Q        string
}

// Query returns matching pages newest first and whether there are more.
func (s *PageStore) Query(q PageQuery) ([]Page, bool) {
	needle := strings.ToLower(strings.TrimSpace(q.Q))
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Page, 0, q.Limit)
	for i := len(s.ring) - 1; i >= 0; i-- {
		p := s.ring[i]
		if q.Before > 0 && p.ID >= q.Before {
			continue
		}
		if !q.Filtered && !p.Posted {
			continue
		}
		if needle != "" && !strings.Contains(p.Address, needle) && !strings.Contains(strings.ToLower(p.Message), needle) {
			continue
		}
		if len(out) == q.Limit {
			return out, true
		}
		out = append(out, p)
	}
	return out, false
}

// Today returns the posted pages since local midnight and the newest
// posted page time (0 when none).
func (s *PageStore) Today() (count int, last int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.day != time.Now().Format(time.DateOnly) {
		return 0, s.lastPosted
	}
	return s.todayCount, s.lastPosted
}

// Close flushes and closes the file.
func (s *PageStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
}
