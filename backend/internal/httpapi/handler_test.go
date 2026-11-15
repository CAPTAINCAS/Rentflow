package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rentflow/backend/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestHandlerServesAPIWithoutWebProxy proves the API handler works without a
// WEB_ORIGIN (the managed deployment shape) and never panics on a missing
// reverse-proxy target.
func TestHandlerServesAPIWithoutWebProxy(t *testing.T) {
	server := &Server{C: config.Config{Env: "test", WebOrigin: ""}, Log: testLogger()}
	handler := server.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown API route returned %d, want 404", recorder.Code)
	}
	var body envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error == nil || body.Error.Code != "not_found" {
		t.Fatalf("unknown API route body = %s, want a not_found error envelope", recorder.Body.String())
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if page.Code != http.StatusNotFound {
		t.Fatalf("page request without proxy returned %d, want 404", page.Code)
	}
	if strings.Contains(page.Body.String(), "panic") {
		t.Fatalf("page request without proxy leaked a panic: %s", page.Body.String())
	}
}

// TestProxyWebRejectsInvalidWebOrigin keeps a misconfigured gateway from
// starting with an unusable proxy target.
func TestProxyWebRejectsInvalidWebOrigin(t *testing.T) {
	for _, origin := range []string{"", "not-a-url", "ftp://example.com", "http://"} {
		server := &Server{C: config.Config{Env: "test", WebOrigin: origin}, Log: testLogger()}
		if _, err := server.ProxyWeb(); err == nil {
			t.Fatalf("ProxyWeb(%q) = nil error, want a configuration error", origin)
		}
	}
}

// TestProxyWebForwardsPageRequests verifies the single-container gateway still
// proxies non-API requests to the configured Next.js origin.
func TestProxyWebForwardsPageRequests(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("frontend:" + r.URL.Path))
	}))
	defer web.Close()

	server := &Server{C: config.Config{Env: "test", WebOrigin: web.URL}, Log: testLogger()}
	handler, err := server.ProxyWeb()
	if err != nil {
		t.Fatalf("ProxyWeb() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "frontend:/dashboard" {
		t.Fatalf("proxied page = %d %q, want 200 frontend:/dashboard", recorder.Code, recorder.Body.String())
	}
}
