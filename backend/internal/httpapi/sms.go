package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rentflow/backend/internal/config"
)

type SMSProvider interface {
	Send(context.Context, []string, string) error
}
type mockSMS struct{ log *slog.Logger }

func (m mockSMS) Send(_ context.Context, to []string, message string) error {
	m.log.Info("mock SMS accepted; no message sent", "recipients", len(to), "message_length", len(message))
	return nil
}

type africaTalkingSMS struct {
	c      config.Config
	client *http.Client
}

func (a africaTalkingSMS) Send(ctx context.Context, to []string, message string) error {
	if a.c.ATUsername == "" || a.c.ATAPIKey == "" {
		return fmt.Errorf("Africa's Talking credentials are not configured")
	}
	body := map[string]any{"username": a.c.ATUsername, "phoneNumbers": to, "message": message}
	if a.c.ATSenderID != "" {
		body["senderId"] = a.c.ATSenderID
	}
	raw, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(a.c.ATBaseURL, "/")+"/version1/messaging/bulk", bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apiKey", a.c.ATAPIKey)
	res, e := a.client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Africa's Talking returned HTTP %d", res.StatusCode)
	}
	var result struct {
		SMSMessageData struct {
			Recipients []struct {
				StatusCode int    `json:"statusCode"`
				Status     string `json:"status"`
			} `json:"Recipients"`
		} `json:"SMSMessageData"`
	}
	if json.NewDecoder(res.Body).Decode(&result) != nil || len(result.SMSMessageData.Recipients) == 0 {
		return fmt.Errorf("Africa's Talking returned no recipient delivery results")
	}
	for _, recipient := range result.SMSMessageData.Recipients {
		if recipient.StatusCode != 100 && recipient.StatusCode != 101 && recipient.StatusCode != 102 {
			return fmt.Errorf("Africa's Talking rejected a recipient with status %q", recipient.Status)
		}
	}
	return nil
}
func newSMS(c config.Config, l *slog.Logger) SMSProvider {
	if c.SMSProvider == "africastalking" {
		return africaTalkingSMS{c: c, client: &http.Client{Timeout: 15_000_000_000}}
	}
	return mockSMS{log: l}
}
