package mobile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/mail"
)

type receivedCall struct {
	to, name string
	app      mail.ReceiverApplication
}

type fakeApplicationMailer struct {
	calls chan receivedCall
	uuid  string
	err   error
}

func (f *fakeApplicationMailer) SendApplicationReceived(_ context.Context, to, name string, app mail.ReceiverApplication) (string, error) {
	f.calls <- receivedCall{to, name, app}
	return f.uuid, f.err
}

func newMockDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return sqlx.NewDb(conn, "sqlmock"), mock
}

func waitForExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := mock.ExpectationsWereMet()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func createSystemRequest(t *testing.T, h *Handlers) *httptest.ResponseRecorder {
	t.Helper()
	name := "Joseph"
	body := `{"name":" Budgewoi ","suburb":"Budgewoi","notes":"{{ .Foo }}","accepted_terms":true}`
	r := httptest.NewRequest(http.MethodPost, "/systems", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), UserContextKey, &AuthUser{ID: 6306, Name: &name, Email: "member@example.invalid"}))
	w := httptest.NewRecorder()
	h.CreateSystem(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	return w
}

func TestCreateSystemEmailsApplicantAndStoresConversation(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`INSERT INTO systems`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectExec(`UPDATE systems SET support_conversation_uuid = \$1 WHERE id = \$2`).
		WithArgs("conv-1", int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mailer := &fakeApplicationMailer{calls: make(chan receivedCall, 1), uuid: "conv-1"}
	h := NewHandlers(db)
	h.applications = mailer

	createSystemRequest(t, h)

	select {
	case c := <-mailer.calls:
		want := receivedCall{"member@example.invalid", "Joseph", mail.ReceiverApplication{SystemName: "Budgewoi", Suburb: "Budgewoi", Notes: "{{ .Foo }}"}}
		if c != want {
			t.Fatalf("mailer call = %+v, want %+v", c, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("thank-you email was not sent")
	}
	waitForExpectations(t, mock)
}

func TestCreateSystemSucceedsWhenEmailFails(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`INSERT INTO systems`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mailer := &fakeApplicationMailer{calls: make(chan receivedCall, 1), err: errors.New("Libredesk down")}
	h := NewHandlers(db)
	h.applications = mailer

	createSystemRequest(t, h)
	select {
	case <-mailer.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("thank-you email was not attempted")
	}
}

// After a failed email nothing is stored: the armed UPDATE stays unused.
func TestApplicationReceivedFailureStoresNothing(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`UPDATE systems`).WillReturnResult(sqlmock.NewResult(0, 1))
	mailer := &fakeApplicationMailer{calls: make(chan receivedCall, 1), err: errors.New("Libredesk down")}
	sendApplicationReceived(context.Background(), db, mailer, 42, "member@example.invalid", "", mail.ReceiverApplication{})
	if err := mock.ExpectationsWereMet(); err == nil {
		t.Fatal("stored a conversation after the email failed")
	}
}

func TestCreateSystemWithoutMailerSendsNothing(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`INSERT INTO systems`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectExec(`UPDATE systems`).WillReturnResult(sqlmock.NewResult(0, 1))
	createSystemRequest(t, NewHandlers(db))
	time.Sleep(50 * time.Millisecond)
	if err := mock.ExpectationsWereMet(); err == nil || !strings.Contains(err.Error(), "UPDATE systems") {
		t.Fatalf("want only the armed UPDATE unused, got %v", err)
	}
}
