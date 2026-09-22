package mail

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestSMTPPasswordResetMailerRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		from string
		mode string
	}{
		{"invalid mode", "smtp.example.invalid", 587, "noreply@example.invalid", "opportunistic"},
		{"plain external host", "smtp.example.invalid", 25, "noreply@example.invalid", "plain"},
		{"header injection in from", "smtp.example.invalid", 587, "noreply@example.invalid\r\nBcc: victim@example.invalid", "starttls"},
		{"missing host", "", 587, "noreply@example.invalid", "starttls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSMTPPasswordResetMailer(tt.host, tt.port, "", "", tt.from, tt.mode); err == nil {
				t.Fatal("unsafe SMTP configuration was accepted")
			}
		})
	}
}

func TestSMTPPasswordResetMailerRejectsHeaderInjectionAtSend(t *testing.T) {
	mailer, err := NewSMTPPasswordResetMailer("localhost", 25, "", "", "noreply@example.invalid", "plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := mailer.SendPasswordReset(context.Background(), "member@example.invalid\r\nBcc: victim@example.invalid", "https://app.example/reset?token=x", "x"); err == nil {
		t.Fatal("recipient header injection was accepted")
	}
	if err := mailer.SendPasswordReset(context.Background(), "member@example.invalid", "https://app.example/reset\r\nBcc: victim@example.invalid", "x"); err == nil {
		t.Fatal("URL header injection was accepted")
	}
}

func TestSMTPPasswordResetMailerRequiresStartTLS(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go func() {
		defer serverConn.Close()
		_, _ = serverConn.Write([]byte("220 local test SMTP\r\n"))
		buf := make([]byte, 1024)
		_, _ = serverConn.Read(buf) // EHLO
		_, _ = serverConn.Write([]byte("250 local test SMTP\r\n"))
	}()
	mailer, err := NewSMTPPasswordResetMailer("localhost", 587, "", "", "noreply@example.invalid", "starttls")
	if err != nil {
		t.Fatal(err)
	}
	_, err = mailer.newClient(context.Background(), clientConn)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err=%v, want STARTTLS refusal", err)
	}
}
