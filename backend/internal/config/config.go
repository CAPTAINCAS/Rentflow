package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env, Port, DatabaseURL, WebOrigin, PublicAppURL, JWTSecret                                                                                                 string
	AccessTTL, RefreshTTL                                                                                                                                      time.Duration
	CookieSecure                                                                                                                                               bool
	CookieDomain                                                                                                                                               string
	DarajaBaseURL, DarajaKey, DarajaSecret, DarajaShortCode, DarajaPasskey, DarajaCallbackURL, DarajaValidationURL, DarajaConfirmationURL, DarajaCallbackToken string
	SMSProvider, ATBaseURL, ATUsername, ATAPIKey, ATSenderID                                                                                                   string
}

func Load() Config {
	c := Config{Env: env("APP_ENV", "development"), Port: env("PORT", "3000"), DatabaseURL: env("DATABASE_URL", "postgres://rentflow:change-me@localhost:5432/rentflow?sslmode=disable"), WebOrigin: env("WEB_ORIGIN", "http://127.0.0.1:3001"), PublicAppURL: strings.TrimRight(env("PUBLIC_APP_URL", "http://127.0.0.1:3000"), "/"), JWTSecret: env("JWT_SECRET", ""), CookieDomain: env("COOKIE_DOMAIN", ""), DarajaBaseURL: mpesaBase(), DarajaKey: env("MPESA_CONSUMER_KEY", ""), DarajaSecret: env("MPESA_CONSUMER_SECRET", ""), DarajaShortCode: env("MPESA_SHORTCODE", ""), DarajaPasskey: env("MPESA_PASSKEY", ""), DarajaCallbackURL: env("MPESA_CALLBACK_URL", ""), DarajaValidationURL: env("MPESA_C2B_VALIDATION_URL", ""), DarajaConfirmationURL: env("MPESA_C2B_CONFIRMATION_URL", ""), DarajaCallbackToken: env("MPESA_CALLBACK_TOKEN", ""), SMSProvider: strings.ToLower(env("SMS_PROVIDER", "mock")), ATBaseURL: strings.TrimRight(env("AFRICASTALKING_BASE_URL", "https://api.sandbox.africastalking.com"), "/"), ATUsername: env("AFRICASTALKING_USERNAME", ""), ATAPIKey: env("AFRICASTALKING_API_KEY", ""), ATSenderID: env("AFRICASTALKING_SENDER_ID", "")}
	c.AccessTTL = duration("ACCESS_TOKEN_TTL", 15*time.Minute)
	c.RefreshTTL = duration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	c.CookieSecure = boolEnv("COOKIE_SECURE", true)
	return c
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
