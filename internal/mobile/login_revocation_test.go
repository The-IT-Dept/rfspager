package mobile

import (
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordLoginAfterSessionRevocation(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	db := sqlx.NewDb(conn, "sqlmock")
	hash, _ := bcrypt.GenerateFromPassword([]byte("valid-password-for-test"), bcrypt.MinCost)
	mock.ExpectQuery("SELECT[\\s\\S]*FROM users WHERE email").WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "email", "password", "subscription_valid", "subscription_product_id", "subscription_expiry", "will_renew", "subscription_platform", "superuser", "token_version"}).AddRow(7, "uuid", "User", "user@example.invalid", string(hash), false, nil, nil, nil, nil, false, 5))
	h := NewAuthHandler(db, "local-test-secret", "")
	w := httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("POST", "/login", strings.NewReader(`{"email":"user@example.invalid","password":"valid-password-for-test"}`)))
	if w.Code != 200 {
		t.Fatalf("login after revocation failed: %d", w.Code)
	}
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT[\\s\\S]*FROM users WHERE id").WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "email", "subscription_valid", "subscription_product_id", "subscription_expiry", "will_renew", "subscription_platform", "superuser", "token_version"}).AddRow(7, "uuid", "User", "user@example.invalid", false, nil, nil, nil, nil, false, 5))
	request := httptest.NewRequest("GET", "/me", nil)
	request.Header.Set("Authorization", "Bearer "+response.Token)
	authenticated := httptest.NewRecorder()
	JWTUserAuth(db, "local-test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(authenticated, request)
	if authenticated.Code != 204 {
		t.Fatalf("fresh token rejected after revocation: %d", authenticated.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
