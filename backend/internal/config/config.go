package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default local development database URL. Production deployments must always
// provide an explicit DATABASE_URL.
const defaultDevelopmentDatabaseURL = "postgres://rentflow:change-me@localhost:5432/rentflow?sslmode=disable"

// minJWTSecretLength is the minimum accepted length for JWT_SECRET.
const minJWTSecretLength = 32

type Config struct {
	Env, Port, DatabaseURL, WebOrigin, PublicAppURL, JWTSecret                                                                                                 string
	AccessTTL, RefreshTTL                                                                                                                                      time.Duration
	CookieSecure                                                                                                                                               bool
	CookieDomain                                                                                                                                               string
	DarajaBaseURL, DarajaKey, DarajaSecret, DarajaShortCode, DarajaPasskey, DarajaCallbackURL, DarajaValidationURL, DarajaConfirmationURL, DarajaCallbackToken string
	SMSProvider, ATBaseURL, ATUsername, ATAPIKey, ATSenderID                                                                                                   string
}

// Load reads configuration from the process environment. It never fails: use
// Validate to produce operator-facing startup errors.
func Load() Config {
	// WEB_ORIGIN has no default on purpose. When it is empty the API runs as an
	// API-only service (the managed deployment shape where the platform routes
	// /api/v1 to this process and page requests to the Next.js frontend). Set it
	// only for the single-container gateway image, where the Go process also
	// reverse-proxies page requests to the Next.js process.
	c := Config{Env: env("APP_ENV", "development"), Port: env("PORT", "3000"), DatabaseURL: env("DATABASE_URL", defaultDevelopmentDatabaseURL), WebOrigin: strings.TrimRight(env("WEB_ORIGIN", ""), "/"), PublicAppURL: strings.TrimRight(env("PUBLIC_APP_URL", "http://127.0.0.1:3000"), "/"), JWTSecret: env("JWT_SECRET", ""), CookieDomain: env("COOKIE_DOMAIN", ""), DarajaBaseURL: mpesaBase(), DarajaKey: env("MPESA_CONSUMER_KEY", ""), DarajaSecret: env("MPESA_CONSUMER_SECRET", ""), DarajaShortCode: env("MPESA_SHORTCODE", ""), DarajaPasskey: env("MPESA_PASSKEY", ""), DarajaCallbackURL: env("MPESA_CALLBACK_URL", ""), DarajaValidationURL: env("MPESA_C2B_VALIDATION_URL", ""), DarajaConfirmationURL: env("MPESA_C2B_CONFIRMATION_URL", ""), DarajaCallbackToken: env("MPESA_CALLBACK_TOKEN", ""), SMSProvider: strings.ToLower(env("SMS_PROVIDER", "mock")), ATBaseURL: strings.TrimRight(env("AFRICASTALKING_BASE_URL", "https://api.sandbox.africastalking.com"), "/"), ATUsername: env("AFRICASTALKING_USERNAME", ""), ATAPIKey: env("AFRICASTALKING_API_KEY", ""), ATSenderID: env("AFRICASTALKING_SENDER_ID", "")}
	c.AccessTTL = duration("ACCESS_TOKEN_TTL", 15*time.Minute)
	c.RefreshTTL = duration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	c.CookieSecure = boolEnv("COOKIE_SECURE", true)
	return c
}

// Validate returns every configuration problem that would prevent a safe
// startup. Authentication and database security are never relaxed silently.
func (c Config) Validate() error {
	var problems []string
	if len(c.JWTSecret) < minJWTSecretLength {
		problems = append(problems, fmt.Sprintf("JWT_SECRET must be configured with at least %d characters (set it in the deployment environment; generate one with: openssl rand -base64 48)", minJWTSecretLength))
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		problems = append(problems, "DATABASE_URL must be configured")
	} else if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		problems = append(problems, err.Error())
	}
	if c.Env != "development" && c.DatabaseURL == defaultDevelopmentDatabaseURL {
		problems = append(problems, "DATABASE_URL is still the local development default; set the deployment database URL")
	}
	if c.Env != "development" && strings.Contains(c.DatabaseURL, "sslmode=disable") {
		problems = append(problems, "DATABASE_URL must use sslmode=require (or a stronger mode) outside development")
	}
	if c.AccessTTL <= 0 || c.RefreshTTL <= 0 {
		problems = append(problems, "ACCESS_TOKEN_TTL and REFRESH_TOKEN_TTL must be positive durations")
	}
	if c.AccessTTL >= c.RefreshTTL {
		problems = append(problems, "ACCESS_TOKEN_TTL must be shorter than REFRESH_TOKEN_TTL")
	}
	if c.Env != "development" && !c.CookieSecure {
		problems = append(problems, "COOKIE_SECURE must be true outside development so session cookies are only sent over HTTPS")
	}
	if c.WebOrigin != "" {
		if parsed, err := url.Parse(c.WebOrigin); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "WEB_ORIGIN must be an absolute http(s) origin such as http://127.0.0.1:3001, or empty for an API-only deployment")
		}
	}
	if c.PublicAppURL != "" {
		if parsed, err := url.Parse(c.PublicAppURL); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "PUBLIC_APP_URL must be an absolute http(s) origin such as https://app.example.com")
		}
	}
	if c.Env != "development" && len(strings.TrimSpace(c.DarajaCallbackToken)) < minJWTSecretLength {
		problems = append(problems, fmt.Sprintf("MPESA_CALLBACK_TOKEN must be configured with at least %d characters outside development", minJWTSecretLength))
	}
	if c.SMSProvider != "mock" && c.SMSProvider != "africastalking" {
		problems = append(problems, "SMS_PROVIDER must be either mock or africastalking")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// validateDatabaseURL checks that DATABASE_URL is a PostgreSQL URL with the
// settings pgx needs. It reports problems without echoing credentials.
func validateDatabaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("DATABASE_URL is not a valid URL: %v", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("DATABASE_URL must use the postgres:// or postgresql:// scheme")
	}
	if parsed.Host == "" {
		return fmt.Errorf("DATABASE_URL must include a host")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return fmt.Errorf("DATABASE_URL must include a database name")
	}
	return nil
}

func mpesaBase() string { return "https://sandbox.safaricom.co.ke" }
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func duration(k string, d time.Duration) time.Duration {
	v, e := time.ParseDuration(os.Getenv(k))
	if e == nil && v > 0 {
		return v
	}
	return d
}
func boolEnv(k string, d bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	b, e := strconv.ParseBool(v)
	if e != nil {
		return d
	}
	return b
}
