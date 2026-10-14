package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

type approvedCall struct {
	conversation, to, name, systemName, command string
}

type fakeApprovalMailer struct {
	calls []approvedCall
	uuid  string
	err   error
}

func (f *fakeApprovalMailer) SendApplicationApproved(_ context.Context, conversation, to, name, systemName, command string) (string, error) {
	f.calls = append(f.calls, approvedCall{conversation, to, name, systemName, command})
	return f.uuid, f.err
}

const ownerQuery = `SELECT u.email, COALESCE\(u.name, ''\) AS name, s.name AS system_name`

func approvalMock(t *testing.T, conversation string) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	mock.ExpectQuery(ownerQuery).WithArgs(int64(42)).WillReturnRows(
		sqlmock.NewRows([]string{"email", "name", "system_name", "conversation"}).
			AddRow("member@example.invalid", "Joseph", "Budgewoi", conversation))
	return sqlx.NewDb(conn, "sqlmock"), mock
}

func TestApprovalEmailRepliesInExistingConversation(t *testing.T) {
	db, mock := approvalMock(t, "conv-9")
	mock.ExpectExec(`UPDATE systems`).WillReturnResult(sqlmock.NewResult(0, 1))
	mailer := &fakeApprovalMailer{uuid: "conv-9"}
	sendApplicationApproved(context.Background(), db, mailer, 42, "curl x | sudo bash -s -- KEY")
	want := approvedCall{"conv-9", "member@example.invalid", "Joseph", "Budgewoi", "curl x | sudo bash -s -- KEY"}
	if len(mailer.calls) != 1 || mailer.calls[0] != want {
		t.Fatalf("calls = %+v, want %+v", mailer.calls, want)
	}
	// Same conversation: nothing to store, so the armed UPDATE stays unused.
	if err := mock.ExpectationsWereMet(); err == nil || !strings.Contains(err.Error(), "UPDATE systems") {
		t.Fatalf("want only the UPDATE unused, got %v", err)
	}
}

func TestApprovalEmailStoresNewConversation(t *testing.T) {
	db, mock := approvalMock(t, "")
	mock.ExpectExec(`UPDATE systems SET support_conversation_uuid = \$1 WHERE id = \$2`).
		WithArgs("conv-2", int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mailer := &fakeApprovalMailer{uuid: "conv-2"}
	sendApplicationApproved(context.Background(), db, mailer, 42, "curl x")
	if len(mailer.calls) != 1 || mailer.calls[0].conversation != "" {
		t.Fatalf("calls = %+v", mailer.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalEmailFailureStoresNothing(t *testing.T) {
	db, mock := approvalMock(t, "")
	mock.ExpectExec(`UPDATE systems`).WillReturnResult(sqlmock.NewResult(0, 1))
	sendApplicationApproved(context.Background(), db, &fakeApprovalMailer{err: errors.New("Libredesk down")}, 42, "curl x")
	if err := mock.ExpectationsWereMet(); err == nil || !strings.Contains(err.Error(), "UPDATE systems") {
		t.Fatalf("want only the UPDATE unused, got %v", err)
	}
}

func TestApprovalEmailSkippedWhenOwnerLookupFails(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectQuery(ownerQuery).WillReturnError(errors.New("no rows"))
	mailer := &fakeApprovalMailer{}
	sendApplicationApproved(context.Background(), sqlx.NewDb(conn, "sqlmock"), mailer, 42, "curl x")
	if len(mailer.calls) != 0 {
		t.Fatalf("emailed without an owner: %+v", mailer.calls)
	}
}
