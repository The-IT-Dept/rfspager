package mobile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

func deleteAlertzoneRequest(t *testing.T, id string, setup func(sqlmock.Sqlmock)) int {
	t.Helper()
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	setup(mock)
	h := NewHandlers(sqlx.NewDb(conn, "sqlmock"))
	r := httptest.NewRequest(http.MethodDelete, "/alertzones/"+id, nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
	ctx = context.WithValue(ctx, UserContextKey, &AuthUser{ID: 6306})
	w := httptest.NewRecorder()
	h.DeleteAlertzone(w, r.WithContext(ctx))
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	return w.Code
}

func TestDeleteAlertzoneOnlyDeletesTheCallersZone(t *testing.T) {
	code := deleteAlertzoneRequest(t, "1224", func(m sqlmock.Sqlmock) {
		m.ExpectExec("DELETE FROM alertzones WHERE id = \\$1 AND user_id = \\$2").WithArgs(int64(1224), int64(6306)).WillReturnResult(sqlmock.NewResult(0, 1))
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
}

func TestDeleteAlertzoneReportsDatabaseFailure(t *testing.T) {
	code := deleteAlertzoneRequest(t, "1224", func(m sqlmock.Sqlmock) {
		m.ExpectExec("DELETE FROM alertzones").WillReturnError(errors.New("connection reset"))
	})
	if code != http.StatusInternalServerError {
		t.Fatalf("a failed delete must not report success: status = %d", code)
	}
}

func TestDeleteAlertzoneRejectsInvalidID(t *testing.T) {
	if code := deleteAlertzoneRequest(t, "not-a-number", func(sqlmock.Sqlmock) {}); code != http.StatusBadRequest {
		t.Fatalf("status = %d", code)
	}
}
