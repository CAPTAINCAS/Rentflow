package config

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		Env:                 "production",
		Port:                "3000",
		DatabaseURL:         "postgres://user:secret@db.example.com:5432/rentflow?sslmode=require",
		JWTSecret:           strings.Repeat("s", 48),
		AccessTTL:           15_000_000_000,
		RefreshTTL:          720_000_000_000,
		CookieSecure:        true,
		DarajaCallbackToken: strings.Repeat("c", 40),
		SMSProvider:         "mock",
	}
}

func TestValidateAcceptsCompleteProductionConfiguration(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsShortJWTSecret(t *testing.T) {
	c := validConfig()
	c.JWTSecret = "too-short"
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("Validate() = %v, want JWT_SECRET error", err)
	}
}

func TestValidateRejectsMissingAndUnsafeDatabaseURL(t *testing.T) {
	cases := map[string]Config{
		"missing":      func() Config { c := validConfig(); c.DatabaseURL = ""; return c }(),
		"wrong scheme": func() Config { c := validConfig(); c.DatabaseURL = "mysql://user@host:3306/rentflow"; return c }(),
		"no host":      func() Config { c := validConfig(); c.DatabaseURL = "postgres:///rentflow"; return c }(),
		"no database":  func() Config { c := validConfig(); c.DatabaseURL = "postgres://user@host:5432"; return c }(),
		"development default in production": func() Config {
			c := validConfig()
			c.DatabaseURL = defaultDevelopmentDatabaseURL
			return c
		}(),
		"TLS disabled in production": func() Config {
			c := validConfig()
			c.DatabaseURL = "postgres://user:secret@db.example.com:5432/rentflow?sslmode=disable"
			return c
		}(),
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := c.Validate(); err == nil {
				t.Fatal("Validate() = nil, want a configuration error")
			}
		})
	}
}

func TestValidateRejectsInsecureCookieAndCallbackSettings(t *testing.T) {
	insecureCookies := validConfig()
	insecureCookies.CookieSecure = false
	if err := insecureCookies.Validate(); err == nil || !strings.Contains(err.Error(), "COOKIE_SECURE") {
		t.Fatalf("Validate() = %v, want COOKIE_SECURE error", err)
	}

	shortCallback := validConfig()
	shortCallback.DarajaCallbackToken = "short"
	if err := shortCallback.Validate(); err == nil || !strings.Contains(err.Error(), "MPESA_CALLBACK_TOKEN") {
		t.Fatalf("Validate() = %v, want MPESA_CALLBACK_TOKEN error", err)
	}

	badProvider := validConfig()
	badProvider.SMSProvider = "carrier-pigeon"
	if err := badProvider.Validate(); err == nil || !strings.Contains(err.Error(), "SMS_PROVIDER") {
		t.Fatalf("Validate() = %v, want SMS_PROVIDER error", err)
	}
}

func TestValidateAllowsLocalDevelopmentDefaults(t *testing.T) {
	c := Config{
		Env:          "development",
		DatabaseURL:  defaultDevelopmentDatabaseURL,
		JWTSecret:    strings.Repeat("d", 32),
		AccessTTL:    15_000_000_000,
		RefreshTTL:   720_000_000_000,
		CookieSecure: false,
		SMSProvider:  "mock",
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for local development", err)
	}
}

func TestValidateRejectsImpossibleTokenLifetimes(t *testing.T) {
	c := validConfig()
	c.AccessTTL = c.RefreshTTL
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "ACCESS_TOKEN_TTL") {
		t.Fatalf("Validate() = %v, want token lifetime error", err)
	}
}
