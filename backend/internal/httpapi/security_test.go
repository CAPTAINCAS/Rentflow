package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rentflow/backend/internal/config"
)

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{"0712345678": "254712345678", "+254 712-345-678": "254712345678", "254712345678": "254712345678"}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q)=%q want %q", in, got, want)
		}
	}
}
func TestUnitCodeAccountReferenceLimit(t *testing.T) {
	if !validUnitCode("B-101") {
		t.Fatal("ordinary unit code should be valid")
	}
	if validUnitCode("1234567890123") {
		t.Fatal("unit code longer than Daraja's 12-character reference must be rejected")
	}
	if validUnitCode("   ") {
		t.Fatal("blank unit code must be rejected")
	}
}
func TestCookiePolicy(t *testing.T) {
	s := &Server{C: config.Config{CookieSecure: true, AccessTTL: 0, RefreshTTL: 0}}
	rr := httptest.NewRecorder()
	s.setCookies(rr, "access", "refresh")
	cookies := rr.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("got %d cookies", len(cookies))
	}
	for _, c := range cookies {
		if !c.Secure || c.SameSite != http.SameSiteNoneMode {
			t.Errorf("cookie security attributes wrong: %+v", c)
		}
		if c.Name == "rf_csrf" {
			if c.HttpOnly || c.Path != "/" {
				t.Errorf("CSRF cookie must be browser-readable at the site root: %+v", c)
			}
		} else if !c.HttpOnly {
			t.Errorf("session cookie must be HttpOnly: %+v", c)
		}
	}
}

func TestCookieMutationRequiresMatchingCSRFHeader(t *testing.T) {
	s := &Server{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := s.csrf(next)
	request := func(header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/properties", nil)
		r.AddCookie(&http.Cookie{Name: "rf_access", Value: "session"})
		r.AddCookie(&http.Cookie{Name: "rf_csrf", Value: "csrf-token"})
		if header != "" {
			r.Header.Set("X-CSRF-Token", header)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		return rr
	}
	if rr := request(""); rr.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF header returned %d, want 403", rr.Code)
	}
	if rr := request("csrf-token"); rr.Code != http.StatusNoContent {
		t.Fatalf("matching CSRF header returned %d, want 204", rr.Code)
	}
}
