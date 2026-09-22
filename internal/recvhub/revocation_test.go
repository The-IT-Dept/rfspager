package recvhub

import (
	"context"
	"database/sql"
	"errors"
	"github.com/the-it-dept/rfspager/internal/models"
	"testing"
)

type revocationLookup struct {
	system  *models.System
	revoked bool
}

func (s *revocationLookup) GetSystemByAPIKey(context.Context, string) (*models.System, error) {
	if s.revoked {
		return nil, errors.New("unauthorized")
	}
	return s.system, nil
}
func TestOpenReceiverConnectionRejectsPagesAfterRevocation(t *testing.T) {
	system := &models.System{ID: 7, APIKey: sql.NullString{String: "test-key", Valid: true}}
	store := &revocationLookup{system: system}
	connection := newConn(nil, nil, system)
	accepted := 0
	connection.OnPage = func(context.Context, *models.System, PagePayload) (string, error) { accepted++; return "ok", nil }
	frame := Frame{Type: "page", ID: "test", Data: mustJSON(PagePayload{Address: "123", Message: "synthetic page"})}
	connection.handleFrame(context.Background(), store, frame)
	if accepted != 1 {
		t.Fatal("valid receiver page rejected")
	}
	store.revoked = true
	connection.handleFrame(context.Background(), store, frame)
	if accepted != 1 {
		t.Fatal("revoked connection still injected a page")
	}
	select {
	case <-connection.closed:
	default:
		t.Fatal("revoked connection not closed")
	}
}
