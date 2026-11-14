package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rentflow/backend/internal/config"
)

func TestJSONDBValueNormalizesUUIDForms(t *testing.T) {
	bytes := [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	want := "00112233-4455-6677-8899-aabbccddeeff"

	cases := []struct {
		name  string
		value any
	}{
		{name: "array", value: bytes},
		{name: "byte slice", value: bytes[:]},
		{name: "pgtype UUID", value: pgtype.UUID{Bytes: bytes, Valid: true}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, ok := jsonDBValue(test.value).(string)
			if !ok || got != want {
				t.Fatalf("jsonDBValue(%T) = %q, want %q", test.value, got, want)
			}
		})
	}

	if got := jsonDBValue(int64(42)); got != int64(42) {
		t.Fatalf("non-UUID value changed: %#v", got)
	}
}

func TestAuthorizeDarajaCallback(t *testing.T) {
	secret := strings.Repeat("a", 32)
	tests := []struct {
		name   string
		config config.Config
		query  string
		want   bool
	}{
		{name: "development without token", config: config.Config{Env: "development"}, want: true},
		{name: "production without token", config: config.Config{Env: "production"}, want: false},
		{name: "production rejects wrong token", config: config.Config{Env: "production", DarajaCallbackToken: secret}, query: "token=" + strings.Repeat("b", 32), want: false},
		{name: "production accepts matching token", config: config.Config{Env: "production", DarajaCallbackToken: secret}, query: "token=" + secret, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := &Server{C: test.config}
			request := httptest.NewRequest("POST", "/api/v1/mpesa/c2b-confirmation?"+test.query, nil)
			response := httptest.NewRecorder()
			if got := server.authorizeDarajaCallback(response, request); got != test.want {
				t.Fatalf("authorizeDarajaCallback() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSecureSessionCookiesUseNoneAndSecure(t *testing.T) {
	server := &Server{C: config.Config{CookieSecure: true, AccessTTL: time.Minute, RefreshTTL: time.Hour}}
	response := httptest.NewRecorder()
	server.setCookies(response, "test-access", "test-refresh")
	for _, cookie := range response.Header().Values("Set-Cookie") {
		if !strings.Contains(cookie, "; Secure") || !strings.Contains(cookie, "; SameSite=None") {
			t.Fatal("session cookie lacks Secure/SameSite=None attributes")
		}
	}
}
