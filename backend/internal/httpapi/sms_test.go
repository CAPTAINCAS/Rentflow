package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rentflow/backend/internal/config"
)

func TestAfricaTalkingSMSChecksRecipientResults(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError bool
	}{
		{name: "accepted recipient", body: `{"SMSMessageData":{"Recipients":[{"statusCode":101,"status":"Success"}]}}`},
		{name: "rejected recipient", body: `{"SMSMessageData":{"Recipients":[{"statusCode":403,"status":"Invalid Phone Number"}]}}`, wantError: true},
		{name: "missing delivery result", body: `{"SMSMessageData":{"Recipients":[]}}`, wantError: true},
		{name: "malformed response", body: `not-json`, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			provider := africaTalkingSMS{
				c:      config.Config{ATUsername: "sandbox", ATAPIKey: "test-key", ATBaseURL: server.URL},
				client: server.Client(),
			}
			err := provider.Send(context.Background(), []string{"254700000000"}, "Test receipt")
			if (err != nil) != test.wantError {
				t.Fatalf("Send() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}
