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
	Email             EmailProvider
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
	return &Server{C: c, DB: db, Queries: dbgen.New(db), Log: l, HTTP: &http.Client{Timeout: 15 * time.Second}, SMS: newSMS(c, l), Email: newEmailProvider(c, l)}
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
		r.Post("/activate", s.activateAccount)
		r.Post("/admin-reset", s.adminResetSuperAdmin)
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
		a.Post("/tenants/{id}/resend-invitation", s.resendTenantInvitation)
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
		a.Get("/platform/accounts", s.listPlatformAccounts)
		a.Get("/platform/overview", s.platformOverview)
		a.Get("/platform/subscription-payments", s.listPlatformSubscriptionPayments)
		a.Get("/platform/revenue", s.platformRevenue)
		a.Get("/platform/subscriptions", s.listPlatformSubscriptions)
		a.Get("/platform/audit-logs", s.listPlatformAuditLogs)
		s.registerMaintenanceRoutes(a)
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

// commonPasswords is a small local blocklist of commonly compromised passwords.
var commonPasswords = map[string]bool{
	"password": true, "password123": true, "123456789012": true,
	"qwertyuiop": true, "letmein12345": true, "welcome12345": true,
	"admin1234567": true, "rentflow1234": true,
}

// validatePassword enforces the RentFlow password policy.
func validatePassword(pw string) error {
	if len(pw) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	if len(pw) > 128 {
		return fmt.Errorf("password must be at most 128 characters")
	}
	if commonPasswords[strings.ToLower(strings.TrimSpace(pw))] {
		return fmt.Errorf("password is too common")
	}
	return nil
}

// checkRateLimit returns true if the request is allowed. Uses Postgres shared storage.
func (s *Server) checkRateLimit(r *http.Request, key string, limit int, window time.Duration) bool {
	now := time.Now()
	windowStart := now.Truncate(window)
	_, _ = s.DB.Exec(r.Context(), `DELETE FROM rate_limits WHERE expires_at < $1`, now)
	var count int
	err := s.DB.QueryRow(r.Context(), `INSERT INTO rate_limits(key,window_start,count,expires_at) VALUES($1,$2,1,$3) ON CONFLICT (key,window_start) DO UPDATE SET count=rate_limits.count+1 RETURNING count`, key, windowStart, windowStart.Add(window)).Scan(&count)
	if err != nil {
		return true // fail open
	}
	return count <= limit
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
	if p.Role != "landlord" && p.Role != "agent" && p.Role != "super_admin" {
		writeErr(w, 403, "forbidden", "landlord, agent, or super_admin access required")
		return false
	}
	// Super admins have platform-level access and are not bound to a single account.
	if p.Role == "super_admin" {
		return true
	}
	// Landlords and agents must belong to an active (non-suspended) account.
	if p.AccountID == "" {
		writeErr(w, 403, "forbidden", "account not associated with principal")
		return false
	}
	var status string
	e := s.DB.QueryRow(r.Context(), `SELECT subscription_status FROM accounts WHERE id=$1`, p.AccountID).Scan(&status)
	if e != nil {
		writeErr(w, 500, "database_error", "could not verify account status")
		return false
	}
	if status == "suspended" {
		writeErr(w, 403, "forbidden", "account is suspended")
		return false
	}
	return true
}
func (s *Server) account(r *http.Request) (string, bool) {
	p := principal(r)
	return p.AccountID, p.AccountID != ""
}

// isSuperAdmin returns true if the principal is a platform super_admin.
func isSuperAdmin(p Principal) bool { return p.Role == "super_admin" }

// isLandlordOrAgent returns true if the principal is a landlord or agent.
func isLandlordOrAgent(p Principal) bool { return p.Role == "landlord" || p.Role == "agent" }

// agentAssignedPropertyIDs returns the property IDs an agent is assigned to.
// Returns nil (no restriction) for landlords, super_admins, and tenants.
// Returns an error if the query fails.
func (s *Server) agentAssignedPropertyIDs(r *http.Request) ([]string, error) {
	p := principal(r)
	if p.Role != "agent" {
		return nil, nil
	}
	rows, e := s.DB.Query(r.Context(), `SELECT property_id FROM property_assignments WHERE agent_id=$1 AND account_id=$2`, p.ID, p.AccountID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// effectiveAccountID resolves the account context for the current request.
// Landlords and agents always use their own account_id.
// Super admins must specify an account_id query parameter (?account_id=) for
// account-scoped operations; without one, the operation returns empty results
// (platform-scoped access is not implied). Tenants use their own account_id.
func (s *Server) effectiveAccountID(r *http.Request) (string, error) {
	p := principal(r)
	if p.Role != "super_admin" {
		return p.AccountID, nil
	}
	// Super admin: look for account_id in query parameter only.
	// Reading the request body here would consume it for later decoding.
	if aid := r.URL.Query().Get("account_id"); aid != "" {
		return aid, nil
	}
	return "", nil
}

// checkPropertyAccess returns true if the principal can access the given property.
// Landlords and super_admins always have access.
// Agents must have an assignment in property_assignments.
// Tenants have access if the property is related to their active lease.
func (s *Server) checkPropertyAccess(r *http.Request, propertyID string) (bool, error) {
	p := principal(r)
	if p.Role == "landlord" || p.Role == "super_admin" {
		return true, nil
	}
	if p.Role == "agent" {
		var exists bool
		e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM property_assignments WHERE agent_id=$1 AND account_id=$2 AND property_id=$3)`, p.ID, p.AccountID, propertyID).Scan(&exists)
		return exists, e
	}
	// tenant: check if property is related to their active lease
	if p.TenantID == "" {
		return false, nil
	}
	var exists bool
	e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM leases l JOIN units u ON u.id=l.unit_id WHERE l.tenant_id=$1 AND u.property_id=$2 AND l.status='active')`, p.TenantID, propertyID).Scan(&exists)
	return exists, e
}

// checkPropertyAccessByUnit returns true if the principal can access the unit's property.
// Resolves the unit's property_id and checks agent assignment.
func (s *Server) checkPropertyAccessByUnit(r *http.Request, unitID, accountID string) (bool, error) {
	p := principal(r)
	if p.Role == "landlord" || p.Role == "super_admin" {
		return true, nil
	}
	if p.Role == "agent" {
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT property_id FROM units WHERE account_id=$1 AND id=$2`, accountID, unitID).Scan(&propertyID)
		if e != nil {
			return false, e
		}
		return s.checkPropertyAccess(r, propertyID)
	}
	if p.Role == "tenant" {
		if p.TenantID == "" {
			return false, nil
		}
		var exists bool
		e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM leases l JOIN units u ON u.id=l.unit_id WHERE l.tenant_id=$1 AND l.account_id=$2 AND l.id=$3 AND l.status='active')`, p.TenantID, accountID, unitID).Scan(&exists)
		return exists, e
	}
	return false, nil
}

// checkPropertyAccessByLease returns true if the principal can access the lease's property.
// Resolves the lease's property_id through units and checks agent assignment.
func (s *Server) checkPropertyAccessByLease(r *http.Request, leaseID, accountID string) (bool, error) {
	p := principal(r)
	if p.Role == "landlord" || p.Role == "super_admin" {
		return true, nil
	}
	if p.Role == "agent" {
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT u.property_id FROM leases l JOIN units u ON u.id=l.unit_id WHERE l.account_id=$1 AND l.id=$2`, accountID, leaseID).Scan(&propertyID)
		if e != nil {
			return false, e
		}
		return s.checkPropertyAccess(r, propertyID)
	}
	if p.Role == "tenant" {
		if p.TenantID == "" {
			return false, nil
		}
		var exists bool
		e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM leases l WHERE l.tenant_id=$1 AND l.account_id=$2 AND l.id=$3 AND l.status='active')`, p.TenantID, accountID, leaseID).Scan(&exists)
		return exists, e
	}
	return false, nil
}

// checkPropertyAccessByTenant returns true if the principal can access the tenant's property.
// Resolves the tenant's active lease -> unit -> property and checks agent assignment.
func (s *Server) checkPropertyAccessByTenant(r *http.Request, tenantID, accountID string) (bool, error) {
	p := principal(r)
	if p.Role == "landlord" || p.Role == "super_admin" {
		return true, nil
	}
	if p.Role == "agent" {
		var propertyID string
		e := s.DB.QueryRow(r.Context(), `SELECT u.property_id FROM tenants t JOIN leases l ON l.tenant_id=t.id AND l.account_id=t.account_id JOIN units u ON u.id=l.unit_id WHERE t.id=$1 AND t.account_id=$2 AND l.status='active' LIMIT 1`, tenantID, accountID).Scan(&propertyID)
		if e != nil {
			return false, e
		}
		return s.checkPropertyAccess(r, propertyID)
	}
	if p.Role == "tenant" {
		if p.TenantID == "" {
			return false, nil
		}
		return p.TenantID == tenantID, nil
	}
	return false, nil
}

// filterByAgentProperties removed: it was a no-op and unused. Agent property
// filtering is performed explicitly in each handler via agentAssignedPropertyIDs.
func uuidParam(r *http.Request) string { return chi.URLParam(r, "id") }
func validID(v string) bool {
	_, e := strconv.ParseUint(strings.ReplaceAll(v, "-", ""), 16, 128)
	return e == nil && len(v) == 36
}

// Authentication
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Phone, Password, Role string }
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") || validatePassword(in.Password) != nil {
		writeErr(w, 400, "invalid_request", "name, valid email, and password of at least 10 characters are required")
		return
	}
	role := in.Role
	if role == "" {
		role = "landlord"
	}
	if role != "landlord" {
		writeErr(w, 400, "invalid_role", "public registration is available for landlord accounts only")
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
	ip := r.RemoteAddr
	if !s.checkRateLimit(r, "login:ip:"+ip, 10, time.Minute) {
		writeErr(w, 429, "rate_limited", "too many attempts, try again later")
		return
	}
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
	// Reject login for suspended accounts (landlords and agents only;
	// super_admins are platform-level and not bound to a single account).
	if p.Role != "super_admin" && p.AccountID != "" {
		var status string
		if e2 := s.DB.QueryRow(r.Context(), `SELECT subscription_status FROM accounts WHERE id=$1`, p.AccountID).Scan(&status); e2 == nil && status == "suspended" {
			writeErr(w, 403, "forbidden", "account is suspended")
			return
		}
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
	// Reject refresh for suspended accounts (landlords and agents only;
	// super_admins are platform-level and not bound to a single account).
	if p.Role != "super_admin" && p.AccountID != "" {
		var status string
		if e2 := s.DB.QueryRow(r.Context(), `SELECT subscription_status FROM accounts WHERE id=$1`, p.AccountID).Scan(&status); e2 == nil && status == "suspended" {
			writeErr(w, 403, "forbidden", "account is suspended")
			return
		}
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

func (s *Server) activateAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil || in.Token == "" || validatePassword(in.Password) != nil {
		writeErr(w, 400, "invalid_request", "valid token and password of at least 10 characters are required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "internal", "could not secure password")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", "database error")
		return
	}
	defer tx.Rollback(r.Context())
	var uid string
	err = tx.QueryRow(r.Context(), `SELECT user_id FROM tenant_invitations WHERE token_hash=$1 AND used_at IS NULL AND expires_at>now() FOR UPDATE`, tokenHash(in.Token)).Scan(&uid)
	if err != nil {
		writeErr(w, 400, "invalid_invitation", "invitation is invalid or expired")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2 WHERE id=$1`, uid, string(hash)); err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE tenant_invitations SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL`, tokenHash(in.Token))
	}
	if err != nil {
		writeErr(w, 500, "internal", "could not activate account")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeErr(w, 500, "internal", "could not activate account")
		return
	}
	writeData(w, 200, map[string]string{"message": "account activated"})
}
func (s *Server) adminResetSuperAdmin(w http.ResponseWriter, r *http.Request) {
	if s.C.AdminResetToken == "" || len(s.C.AdminResetToken) < 32 {
		writeErr(w, 503, "unconfigured", "admin reset is not configured")
		return
	}
	var in struct {
		ResetToken string `json:"reset_token"`
		Password   string `json:"password"`
		Email      string `json:"email"`
	}
	if decode(r, &in) != nil || in.ResetToken == "" || validatePassword(in.Password) != nil {
		writeErr(w, 400, "invalid_request", "reset_token and valid password required")
		return
	}
	if len(in.ResetToken) != len(s.C.AdminResetToken) || subtle.ConstantTimeCompare([]byte(in.ResetToken), []byte(s.C.AdminResetToken)) != 1 {
		writeErr(w, 403, "forbidden", "invalid reset token")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "internal", "could not secure password")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(s.C.SuperAdminEmail))
	}
	tag, err := s.DB.Exec(r.Context(), `UPDATE users SET password_hash=$1, email=lower($2) WHERE role='super_admin'`, string(hash), email)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "no super_admin account found")
		return
	}
	writeData(w, 200, map[string]string{"message": "super_admin password reset"})
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Server) forgot(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if !s.checkRateLimit(r, "forgot:ip:"+ip, 5, time.Minute) {
		writeErr(w, 429, "rate_limited", "too many attempts, try again later")
		return
	}
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
	ip := r.RemoteAddr
	if !s.checkRateLimit(r, "reset:ip:"+ip, 5, time.Minute) {
		writeErr(w, 429, "rate_limited", "too many attempts, try again later")
		return
	}
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil || in.Token == "" || validatePassword(in.Password) != nil {
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

// BootstrapSuperAdmin creates the first platform super_admin account from
// configuration environment variables. It is idempotent: if any super_admin
// user already exists, it does nothing. The bootstrap password is never
// logged. Call this after migrations have run and the server is constructed.
// All account and user creation happens inside a single database transaction
// so a partial failure cannot leave an orphaned account.
func (s *Server) BootstrapSuperAdmin() error {
	if s.C.SuperAdminEmail == "" || s.C.SuperAdminPassword == "" {
		return nil
	}
	if err := validatePassword(s.C.SuperAdminPassword); err != nil {
		s.Log.Error("super_admin bootstrap password does not meet policy", "error", err.Error())
		return err
	}
	tx, e := s.DB.Begin(context.Background())
	if e != nil {
		s.Log.Warn("super_admin bootstrap could not start transaction", "error", e)
		return e
	}
	defer tx.Rollback(context.Background())
	var exists bool
	e = tx.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM users WHERE role='super_admin')`).Scan(&exists)
	if e != nil {
		s.Log.Warn("super_admin bootstrap could not check existing users", "error", e)
		return e
	}
	if exists {
		return nil
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(s.C.SuperAdminPassword), bcrypt.DefaultCost)
	if e != nil {
		s.Log.Warn("super_admin bootstrap could not hash password", "error", e)
		return e
	}
	var uid string
	e = tx.QueryRow(context.Background(), `INSERT INTO users(account_id,email,full_name,phone,password_hash,role) VALUES(NULL,lower($1),$2,'',$3,'super_admin') RETURNING id`, s.C.SuperAdminEmail, s.C.SuperAdminName, string(hash)).Scan(&uid)
	if e != nil {
		s.Log.Warn("super_admin bootstrap could not create user", "error", e)
		return e
	}
	if e = tx.Commit(context.Background()); e != nil {
		s.Log.Warn("super_admin bootstrap could not commit transaction", "error", e)
		return e
	}
	s.Log.Info("super_admin bootstrap created platform account", "user_id", uid)
	return nil
}
