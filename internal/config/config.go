package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr, DatabaseURL, Secret, AdminEmail, AdminPassword, Origin string
	Hold                                                         time.Duration
	Secure                                                       bool
}

func Load() (Config, error) {
	c := Config{Addr: env("HTTP_ADDR", ":8080"), DatabaseURL: os.Getenv("DATABASE_URL"), Secret: os.Getenv("JWT_SECRET"), AdminEmail: strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))), AdminPassword: os.Getenv("ADMIN_PASSWORD"), Origin: env("APP_ORIGIN", "http://localhost:8080")}
	seconds, err := strconv.Atoi(env("HOLD_SECONDS", "300"))
	if err != nil || seconds < 1 || seconds > 3600 {
		return c, fmt.Errorf("HOLD_SECONDS must be 1..3600")
	}
	c.Hold = time.Duration(seconds) * time.Second
	c.Secure, err = strconv.ParseBool(env("COOKIE_SECURE", "false"))
	if err != nil {
		return c, fmt.Errorf("invalid COOKIE_SECURE")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("APP_ORIGIN must be an origin such as http://localhost:8080 without trailing slash")
	}
	if c.DatabaseURL == "" || len(c.Secret) < 32 {
		return c, fmt.Errorf("DATABASE_URL and JWT_SECRET (at least 32 bytes) are required")
	}
	a, err := mail.ParseAddress(c.AdminEmail)
	if err != nil || a.Address != c.AdminEmail || len(c.AdminPassword) < 10 || len(c.AdminPassword) > 72 {
		return c, fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD (10..72 bytes) are required")
	}
	return c, nil
}
func env(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
