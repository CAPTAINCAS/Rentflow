package httpapi

import (
	"fmt"
	"net/smtp"
	"strings"

	"github.com/rentflow/backend/internal/config"
)

// EmailProvider sends transactional emails. Implementations must not log
// secrets or tokens in production.
type EmailProvider interface {
	Send(to, subject, body string) error
}

type mockEmailProvider struct {
	log interface{ Info(string, ...any) }
}

func newEmailProvider(c config.Config, log interface{ Info(string, ...any) }) EmailProvider {
	if c.EmailProvider == "smtp" && c.SMTPHost != "" {
		return &smtpEmailProvider{c: c}
	}
	return &mockEmailProvider{log: log}
}

func (m *mockEmailProvider) Send(to, subject, body string) error {
	if m.log != nil {
		m.log.Info("email (mock) would be sent", "to", to, "subject", subject)
	}
	return nil
}

type smtpEmailProvider struct{ c config.Config }

func (s *smtpEmailProvider) Send(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", s.c.SMTPHost, s.c.SMTPPort)
	auth := smtp.PlainAuth("", s.c.SMTPUsername, s.c.SMTPPassword, s.c.SMTPHost)
	msg := "From: " + s.c.SMTPFrom + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		body + "\r\n"
	return smtp.SendMail(addr, auth, s.c.SMTPFrom, []string{to}, []byte(msg))
}

// buildActivationLink constructs the frontend activation URL using the
// configured public app URL (never localhost in production).
func buildActivationLink(c config.Config, token string) string {
	base := strings.TrimRight(c.PublicAppURL, "/")
	return base + "/activate?token=" + token
}
