package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/rentflow/backend/internal/config"
	"github.com/rentflow/backend/internal/dbgen"
)

type Server struct {
	C                 config.Config
	DB                *pgxpool.Pool
	Queries           *dbgen.Queries
	Log               *slog.Logger
	HTTP              *http.Client
	SMS               SMSProvider
	proxy             http.Handler
	tokenMu           sync.Mutex
	darajaTokenValue  string
	darajaTokenExpiry time.Time
}
type Principal struct{ ID, Role, AccountID, TenantID, Name, Email string }
type ctxKey struct{}
type claims struct {
	Role      string `json:"role"`
	AccountID string `json:"accountId,omitempty"`
	TenantID  string `json:"tenantId,omitempty"`
	TokenID   string `json:"tokenId,omitempty"`
	jwt.RegisteredClaims
}
type envelope struct {
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(c config.Config, db *pgxpool.Pool, l *slog.Logger) *Server {
	return &Server{C: c, DB: db, Queries: dbgen.New(db), Log: l, HTTP: &http.Client{Timeout: 15 * time.Second}, SMS: newSMS(c, l)}
}

// Handler returns the public API and health routes only. Page proxying is a
// separate concern (see ProxyWeb) so the API can also run behind a managed
// platform that routes /api/v1 to this service and everything else to the
// Next.js frontend.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, middleware.Timeout(30*time.Second))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.Ping(r.Context()); err != nil {
			writeErr(w, 503, "database_unavailable", "database unavailable")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	api := chi.NewRouter()
	api.Use(s.csrf)
	api.Route("/auth", func(r chi.Router) {
		r.Post("/register", s.register)
		r.Post("/login", s.login)
		r.Post("/refresh", s.refresh)
		r.Post("/logout", s.logout)
		r.Post("/forgot-password", s.forgot)
		r.Post("/reset-password", s.resetPassword)
		r.Group(func(a chi.Router) { a.Use(s.auth); a.Get("/me", s.me) })
	})
	api.Group(func(a chi.Router) { a.Use(s.auth); a.Get("/me", s.me) })
	api.Post("/mpesa/stk-callback", s.stkCallback)
	api.Post("/mpesa/c2b-validation", s.c2bValidation)
	api.Post("/mpesa/c2b-confirmation", s.c2bConfirmation)
	api.Group(func(a chi.Router) {
		a.Use(s.auth)
		a.Get("/properties", s.listProperties)
		a.Post("/properties", s.createProperty)
		a.Get("/properties/{id}", s.getProperty)
		a.Patch("/properties/{id}", s.patchProperty)
		a.Delete("/properties/{id}", s.deleteProperty)
		a.Get("/units", s.listUnits)
		a.Post("/units", s.createUnit)
		a.Patch("/units/{id}", s.patchUnit)
		a.Delete("/units/{id}", s.deleteUnit)
		a.Post("/team/agents", s.createAgent)
		a.Get("/tenants", s.listTenants)
		a.Post("/tenants", s.createTenant)
		a.Patch("/tenants/{id}", s.patchTenant)
		a.Delete("/tenants/{id}", s.deleteTenant)
		a.Get("/leases", s.listLeases)
		a.Post("/leases", s.createLease)
		a.Patch("/leases/{id}", s.patchLease)
		a.Get("/dashboard", s.dashboard)
		a.Get("/invoices", s.listInvoices)
		a.Get("/arrears", s.overdueInvoices)
		a.Get("/payments", s.listPayments)
		a.Post("/payments", s.recordPayment)
		a.Get("/payments/unmatched", s.unmatched)
		a.Post("/payments/{id}/assign", s.assignPayment)
		a.Post("/payments/stk-push", s.stkPush)
		a.Post("/mpesa/c2b/register", s.registerC2B)
		a.Get("/reports/collections.csv", s.collectionsCSV)
		a.Get("/reports/arrears.csv", s.arrearsCSV)
	})
	r.Mount("/api/v1", api)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			writeErr(w, 404, "not_found", "route not found")
			return
		}
		if s.proxy == nil {
			writeErr(w, 404, "not_found", "this deployment serves the API only; open the frontend origin for pages")
			return
		}
		s.proxy.ServeHTTP(w, req)
	})
	return s.bind(r)
}

// ProxyWeb builds the single-container gateway handler: the API plus a reverse
// proxy for everything else to the Next.js process. Deployments where the
// platform routes /api/v1 to the API and pages to the frontend should use
// Handler instead.
func (s *Server) ProxyWeb() (http.Handler, error) {
	target, err := url.Parse(s.C.WebOrigin)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("WEB_ORIGIN is not a valid http(s) web origin: %q", s.C.WebOrigin)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	old := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalHost := req.Host
		old(req)
		req.Host = target.Host
		req.Header.Set("X-Forwarded-Host", originalHost)
		req.Header.Set("X-Forwarded-Proto", scheme(req))
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		s.Log.Error("web proxy failed", "error", e)
		http.Error(w, "web application unavailable", http.StatusBadGateway)
	}
	s.proxy = proxy
	return s.Handler(), nil
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		return v
	}
	return "http"
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeData(w http.ResponseWriter, status int, v any) { writeJSON(w, status, envelope{Data: v}) }
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, envelope{Error: &apiError{code, msg}})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func principal(r *http.Request) Principal { p, _ := r.Context().Value(ctxKey{}).(Principal); return p }
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" {
			if c, e := r.Cookie("rf_access"); e == nil {
				token = c.Value
			}
		}
		p, e := s.parseAccess(token)
		if e != nil {
			writeErr(w, 401, "unauthorized", "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}
func (s *Server) parseAccess(t string) (Principal, error) {
	var out Principal
	if t == "" || s.C.JWTSecret == "" {
		return out, errors.New("missing token configuration")
	}
	c := &claims{}
	tok, e := jwt.ParseWithClaims(t, c, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("bad signing method")
		}
		return []byte(s.C.JWTSecret), nil
	})
	if e != nil || !tok.Valid || c.Subject == "" || c.Role == "" {
		return out, errors.New("invalid token")
	}
	out = Principal{ID: c.Subject, Role: c.Role, AccountID: c.AccountID, TenantID: c.TenantID}
	return out, nil
}
func (s *Server) manager(w http.ResponseWriter, r *http.Request) bool {
	p := principal(r)
	if p.Role != "landlord" && p.Role != "agent" {
		writeErr(w, 403, "forbidden", "landlord or agent access required")
		return false
	}
	return true
}
func (s *Server) account(r *http.Request) (string, bool) {
	p := principal(r)
	return p.AccountID, p.AccountID != ""
}
func uuidParam(r *http.Request) string { return chi.URLParam(r, "id") }
func validID(v string) bool {
	_, e := strconv.ParseUint(strings.ReplaceAll(v, "-", ""), 16, 128)
	return e == nil && len(v) == 36
}

// Authentication
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Phone, Password, Role string }
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") || len(in.Password) < 10 {
		writeErr(w, 400, "invalid_request", "name, valid email, and password of at least 10 characters are required")
		return
	}
	role := in.Role
	if role == "" {
		role = "landlord"
	}
	if role != "landlord" && role != "agent" {
		writeErr(w, 400, "invalid_role", "registration is available for landlord or agent accounts only")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if e != nil {
		writeErr(w, 500, "internal", "could not secure password")
		return
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		writeErr(w, 500, "internal", "database error")
		return
	}
	defer tx.Rollback(r.Context())
	var aid, uid string
	e = tx.QueryRow(r.Context(), `INSERT INTO accounts(name) VALUES($1) RETURNING id`, strings.TrimSpace(in.Name)+" account").Scan(&aid)
	if e == nil {
		e = tx.QueryRow(r.Context(), `INSERT INTO users(account_id,email,full_name,phone,password_hash,role) VALUES($1,lower($2),$3,$4,$5,$6) RETURNING id`, aid, strings.TrimSpace(in.Email), in.Name, in.Phone, string(hash), role).Scan(&uid)
	}
	if e != nil {
		writeErr(w, 409, "account_exists", "email already registered or account could not be created")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeErr(w, 500, "internal", "database error")
		return
	}
	s.issueSession(w, r, Principal{ID: uid, Role: role, AccountID: aid, Name: in.Name, Email: strings.ToLower(in.Email)})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if decode(r, &in) != nil {
		writeErr(w, 400, "invalid_request", "email and password are required")
		return
	}
	var p Principal
	var hash string
	e := s.DB.QueryRow(r.Context(), `SELECT id,role,coalesce(account_id::text,''),coalesce(tenant_id::text,''),full_name,email,password_hash FROM users WHERE lower(email)=lower($1)`, in.Email).Scan(&p.ID, &p.Role, &p.AccountID, &p.TenantID, &p.Name, &p.Email, &hash)
	if e != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		writeErr(w, 401, "invalid_credentials", "email or password is incorrect")
		return
	}
	s.issueSession(w, r, p)
}
func tokenHash(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, p Principal) {
	if s.C.JWTSecret == "" {
		writeErr(w, 500, "configuration", "JWT_SECRET is not configured")
		return
	}
	now := time.Now()
	access, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims{Role: p.Role, AccountID: p.AccountID, TenantID: p.TenantID, RegisteredClaims: jwt.RegisteredClaims{Subject: p.ID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.C.AccessTTL)), Issuer: "rentflow"}}).SignedString([]byte(s.C.JWTSecret))
	id := fmt.Sprintf("%x", now.UnixNano())
	refresh, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims{Role: p.Role, AccountID: p.AccountID, TenantID: p.TenantID, TokenID: id, RegisteredClaims: jwt.RegisteredClaims{Subject: p.ID, ID: id, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.C.RefreshTTL)), Issuer: "rentflow"}}).SignedString([]byte(s.C.JWTSecret))
	_, e := s.DB.Exec(r.Context(), `INSERT INTO refresh_tokens(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, p.ID, tokenHash(refresh), now.Add(s.C.RefreshTTL))
	if e != nil {
		writeErr(w, 500, "internal", "could not create session")
		return
	}
	s.setCookies(w, access, refresh)
	writeData(w, 200, map[string]any{"user": map[string]any{"id": p.ID, "name": p.Name, "email": p.Email, "role": p.Role, "accountId": p.AccountID, "tenantId": p.TenantID}, "expiresIn": int(s.C.AccessTTL.Seconds())})
}
func cookieSameSite(secure bool) http.SameSite {
	if secure {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}
func (s *Server) setCookies(w http.ResponseWriter, a, rf string) {
	same := cookieSameSite(s.C.CookieSecure)
	csrf := make([]byte, 32)
	if _, e := rand.Read(csrf); e == nil {
		http.SetCookie(w, &http.Cookie{Name: "rf_csrf", Value: base64.RawURLEncoding.EncodeToString(csrf), Path: "/", Secure: s.C.CookieSecure, SameSite: same, Domain: s.C.CookieDomain})
	}
	base := http.Cookie{Path: "/api/v1", HttpOnly: true, Secure: s.C.CookieSecure, SameSite: same, Domain: s.C.CookieDomain}
	base.Name = "rf_access"
	base.Value = a
	base.MaxAge = int(s.C.AccessTTL.Seconds())
	w.Header().Add("Set-Cookie", base.String())
	base.Name = "rf_refresh"
	base.Value = rf
	base.Path = "/api/v1/auth"
	base.MaxAge = int(s.C.RefreshTTL.Seconds())
	w.Header().Add("Set-Cookie", base.String())
}
func (s *Server) clearCookies(w http.ResponseWriter) {
	for _, name := range []string{"rf_access", "rf_refresh", "rf_csrf"} {
		p := "/api/v1"
		if name == "rf_refresh" {
			p = "/api/v1/auth"
		} else if name == "rf_csrf" {
			p = "/"
		}
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: p, Domain: s.C.CookieDomain, HttpOnly: name != "rf_csrf", Secure: s.C.CookieSecure, MaxAge: -1, SameSite: cookieSameSite(s.C.CookieSecure)})
	}
}

func (s *Server) authorizeDarajaCallback(w http.ResponseWriter, r *http.Request) bool {
	secret := strings.TrimSpace(s.C.DarajaCallbackToken)
	if secret == "" && strings.EqualFold(s.C.Env, "development") {
		return true
	}
	if len(secret) < 32 {
		writeErr(w, http.StatusServiceUnavailable, "callback_unconfigured", "payment callbacks are not configured securely")
		return false
	}
	provided := r.URL.Query().Get("token")
	if len(provided) != len(secret) || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
		writeErr(w, http.StatusForbidden, "callback_unauthorized", "payment callback authorization failed")
		return false
	}
	return true
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refreshToken"`
	}
	_ = decode(r, &in)
	if in.RefreshToken == "" {
		if c, e := r.Cookie("rf_refresh"); e == nil {
			in.RefreshToken = c.Value
		}
	}
	c := &claims{}
	t, e := jwt.ParseWithClaims(in.RefreshToken, c, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("bad signing method")
		}
		return []byte(s.C.JWTSecret), nil
	})
	if e != nil || !t.Valid || c.ID == "" {
		writeErr(w, 401, "invalid_refresh", "refresh token is invalid or expired")
		return
	}
	tag, e := s.DB.Exec(r.Context(), `UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, tokenHash(in.RefreshToken))
	if e != nil || tag.RowsAffected() != 1 {
		writeErr(w, 401, "invalid_refresh", "refresh token was already used or revoked")
		return
	}
	var p Principal
	e = s.DB.QueryRow(r.Context(), `SELECT id,role,coalesce(account_id::text,''),coalesce(tenant_id::text,''),full_name,email FROM users WHERE id=$1`, c.Subject).Scan(&p.ID, &p.Role, &p.AccountID, &p.TenantID, &p.Name, &p.Email)
	if e != nil {
		writeErr(w, 401, "invalid_refresh", "account no longer exists")
		return
	}
	s.issueSession(w, r, p)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refreshToken"`
	}
	_ = decode(r, &in)
	if in.RefreshToken == "" {
		if c, e := r.Cookie("rf_refresh"); e == nil {
			in.RefreshToken = c.Value
		}
	}
	if in.RefreshToken != "" {
		_, _ = s.DB.Exec(r.Context(), `UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, tokenHash(in.RefreshToken))
	}
	s.clearCookies(w)
	writeData(w, 200, map[string]string{"message": "logged out"})
}
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unsafe := r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodDelete
		callback := strings.HasSuffix(r.URL.Path, "/mpesa/stk-callback") || strings.HasSuffix(r.URL.Path, "/mpesa/c2b-validation") || strings.HasSuffix(r.URL.Path, "/mpesa/c2b-confirmation")
		_, accessErr := r.Cookie("rf_access")
		_, refreshErr := r.Cookie("rf_refresh")
		hasCookieSession := accessErr == nil || refreshErr == nil
		if unsafe && !callback && r.Header.Get("Authorization") == "" && hasCookieSession {
			cookie, err := r.Cookie("rf_csrf")
			header := r.Header.Get("X-CSRF-Token")
			if err != nil || header == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
				writeErr(w, http.StatusForbidden, "csrf_failed", "cookie-authenticated mutation requires a matching X-CSRF-Token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Server) forgot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Email) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "email is required")
		return
	}
	var id, email, phone string
	err := s.DB.QueryRow(r.Context(), `SELECT id,email,phone FROM users WHERE lower(email)=lower($1)`, in.Email).Scan(&id, &email, &phone)
	if err == nil {
		raw, tokenErr := randomToken()
		if tokenErr == nil {
			_, tokenErr = s.DB.Exec(r.Context(), `INSERT INTO password_resets(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')`, id, tokenHash(raw))
		}
		if tokenErr == nil {
			link := s.C.PublicAppURL + "/reset-password?token=" + url.QueryEscape(raw)
			if s.C.Env == "development" {
				s.Log.Info("local password reset link issued", "email", email, "url", link)
			}
			if phone != "" {
				if sendErr := s.SMS.Send(r.Context(), []string{normalizePhone(phone)}, "Reset your RentFlow password: "+link); sendErr != nil {
					s.Log.Warn("password reset SMS could not be sent", "error", sendErr)
				}
			}
		}
	}
	writeData(w, http.StatusOK, map[string]string{"message": "If the account exists, password reset instructions will be sent."})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil || in.Token == "" || len(in.Password) < 10 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "valid reset token and password of at least 10 characters are required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "could not secure password")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "database_error", "could not reset password")
		return
	}
	defer tx.Rollback(r.Context())
	var uid string
	err = tx.QueryRow(r.Context(), `SELECT user_id FROM password_resets WHERE token_hash=$1 AND used_at IS NULL AND expires_at>now() FOR UPDATE`, tokenHash(in.Token)).Scan(&uid)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_reset_token", "reset token is invalid or expired")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2 WHERE id=$1`, uid, string(hash)); err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE password_resets SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL`, tokenHash(in.Token))
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "database_error", "could not reset password")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "database_error", "could not reset password")
		return
	}
	writeData(w, http.StatusOK, map[string]string{"message": "password reset successfully"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	var name, email, phone string
	e := s.DB.QueryRow(r.Context(), `SELECT full_name,email,phone FROM users WHERE id=$1`, p.ID).Scan(&name, &email, &phone)
	if e != nil {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	writeData(w, 200, map[string]any{"id": p.ID, "name": name, "email": email, "phone": phone, "role": p.Role, "accountId": p.AccountID, "tenantId": p.TenantID})
}

// Generic row serialization for read-only endpoints.
func rows(w http.ResponseWriter, r *http.Request, query string, args ...any) {
	rs, e := dbQuery(r, query, args...)
	if e != nil {
		writeErr(w, 500, "database_error", "could not read records")
		return
	}
	defer rs.Close()
	cols := rs.FieldDescriptions()
	out := make([]map[string]any, 0)
	for rs.Next() {
		vals, e := rs.Values()
		if e != nil {
			writeErr(w, 500, "database_error", "could not read records")
			return
		}
		m := map[string]any{}
		for i, c := range cols {
			m[string(c.Name)] = jsonDBValue(vals[i])
		}
		out = append(out, m)
	}
	if rs.Err() != nil {
		writeErr(w, 500, "database_error", "could not read records")
		return
	}
	writeData(w, 200, out)
}

func jsonDBValue(value any) any {
	switch id := value.(type) {
	case [16]byte:
		return uuidText(id[:])
	case []byte:
		if len(id) == 16 {
			return uuidText(id)
		}
	case pgtype.UUID:
		if id.Valid {
			return uuidText(id.Bytes[:])
		}
	}
	return value
}

func uuidText(value []byte) string {
	encoded := hex.EncodeToString(value)
	if len(encoded) != 32 {
		return ""
	}
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func dbQuery(r *http.Request, q string, args ...any) (pgx.Rows, error) {
	s := r.Context().Value(serverKey{}).(*Server)
	return s.DB.Query(r.Context(), q, args...)
}

type serverKey struct{}

func (s *Server) bind(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serverKey{}, s)))
	})
}
