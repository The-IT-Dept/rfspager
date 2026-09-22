package db

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"regexp"
	"testing"
	"time"
)

func TestReceiverAuthenticationRequiresApprovalAndActiveOwner(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	d := &DB{conn: sqlx.NewDb(conn, "sqlmock").Unsafe()}
	q := `SELECT * FROM systems WHERE api_key = $1 AND deleted_at IS NULL AND approved_at IS NOT NULL AND user_id IS NOT NULL`
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("test-receiver-key").WillReturnRows(sqlmock.NewRows([]string{"id", "system_id", "user_id", "approved_at"}).AddRow(7, "test-system", 3, time.Now()))
	s, err := d.GetSystemByAPIKey(context.Background(), "test-receiver-key")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != 7 {
		t.Fatalf("wrong system %d", s.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
