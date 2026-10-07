// Package mail contains narrowly-scoped outbound email transports. It does
// not depend on HTTP handlers or mobile authentication packages.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

const smtpTimeout = 15 * time.Second

// SMTPPasswordResetMailer sends password recovery links over a deliberately
// configured SMTP transport. It implements mobile.PasswordResetMailer without
// importing that package.
type SMTPPasswordResetMailer struct {
	host     string
	port     int
	username string
	password string
	from     string
	mode     string
}

// NewSMTPPasswordResetMailer validates SMTP transport settings. mode must be
// "starttls", "tls", or "plain"; plain is restricted to loopback hosts for
// local development.
func NewSMTPPasswordResetMailer(host string, port int, username, password, from, mode string) (*SMTPPasswordResetMailer, error) {
	host = strings.TrimSpace(host)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if host == "" || strings.ContainsAny(host, "\r\n") {
		return nil, errors.New("SMTP host is required")
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("SMTP port must be between 1 and 65535")
	}
	if (username == "") != (password == "") {
		return nil, errors.New("SMTP username and password must both be configured")
	}
	if mode != "starttls" && mode != "tls" && mode != "plain" {
		return nil, errors.New("SMTP mode must be starttls, tls, or plain")
	}
	if mode == "plain" && !isLoopbackHost(host) {
		return nil, errors.New("plain SMTP is allowed only for loopback hosts")
	}
	if strings.ContainsAny(from, "\r\n") {
		return nil, errors.New("SMTP from address contains an invalid newline")
	}
	parsedFrom, err := stdmail.ParseAddress(from)
	if err != nil || parsedFrom.Address != from {
		return nil, errors.New("SMTP from must be a plain email address")
	}
	return &SMTPPasswordResetMailer{host: host, port: port, username: username, password: password, from: parsedFrom.Address, mode: mode}, nil
}

// SendPasswordReset sends fullResetURL as the recovery destination. token is
// intentionally not included in headers or logs; it is accepted to match the
// application mailer interface and may be used by template-aware transports.
func (m *SMTPPasswordResetMailer) SendPasswordReset(ctx context.Context, recipient, _, fullResetURL, token string) error {
	if m == nil {
		return errors.New("password reset mailer is not configured")
	}
	if token == "" {
		return errors.New("password reset token is required")
	}
	recipient, err := safeEmailAddress(recipient)
	if err != nil {
		return err
	}
	if err := safeResetURL(fullResetURL); err != nil {
		return err
	}

	address := net.JoinHostPort(m.host, fmt.Sprintf("%d", m.port))
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dialing SMTP server: %w", err)
	}
	defer conn.Close()
	stopClose := closeOnContext(ctx, conn)
	defer stopClose()
	if err := setSMTPDeadline(ctx, conn); err != nil {
		return fmt.Errorf("setting SMTP deadline: %w", err)
	}

	client, err := m.newClient(ctx, conn)
	if err != nil {
		return err
	}
	defer client.Close()
	if m.username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.username, m.password, m.host)); err != nil {
			return fmt.Errorf("authenticating SMTP session: %w", err)
		}
	}
	if err := client.Mail(m.from); err != nil {
		return fmt.Errorf("setting SMTP sender: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("setting SMTP recipient: %w", err)
	}
	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("opening SMTP message body: %w", err)
	}
	message := "From: " + m.from + "\r\n" +
		"To: " + recipient + "\r\n" +
		"Subject: Reset your RFSPager password\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		"Use this link to reset your RFSPager password:\r\n" + fullResetURL + "\r\n\r\n" +
		"This link expires in one hour. If you did not request a password reset, you can ignore this email.\r\n"
	if _, err := io.WriteString(body, message); err != nil {
		_ = body.Close()
		return fmt.Errorf("writing SMTP message: %w", err)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("finishing SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("closing SMTP session: %w", err)
	}
	return nil
}

func (m *SMTPPasswordResetMailer) newClient(ctx context.Context, conn net.Conn) (*smtp.Client, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.host}
	if m.mode == "tls" {
		tlsConn := tls.Client(conn, config)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("starting SMTP TLS: %w", err)
		}
		if !tlsConn.ConnectionState().HandshakeComplete || len(tlsConn.ConnectionState().VerifiedChains) == 0 {
			return nil, errors.New("SMTP TLS certificate was not verified")
		}
		client, err := smtp.NewClient(tlsConn, m.host)
		if err != nil {
			return nil, fmt.Errorf("creating SMTP client: %w", err)
		}
		return client, nil
	}
	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return nil, fmt.Errorf("creating SMTP client: %w", err)
	}
	if m.mode != "starttls" {
		return client, nil
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		_ = client.Close()
		return nil, errors.New("SMTP server does not support required STARTTLS")
	}
	if err := client.StartTLS(config); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("starting SMTP TLS: %w", err)
	}
	state, ok := client.TLSConnectionState()
	if !ok || !state.HandshakeComplete || len(state.VerifiedChains) == 0 {
		_ = client.Close()
		return nil, errors.New("SMTP TLS certificate was not verified")
	}
	return client, nil
}

func safeEmailAddress(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("email address contains an invalid newline")
	}
	parsed, err := stdmail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", errors.New("invalid recipient email address")
	}
	return parsed.Address, nil
}

func safeResetURL(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("password reset URL contains an invalid newline")
	}
	u, err := url.ParseRequestURI(value)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
		return errors.New("invalid password reset URL")
	}
	return nil
}

func setSMTPDeadline(ctx context.Context, conn net.Conn) error {
	deadline := time.Now().Add(smtpTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return conn.SetDeadline(deadline)
}

func closeOnContext(ctx context.Context, conn net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
