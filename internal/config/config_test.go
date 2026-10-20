package config

import (
	"strings"
	"testing"
	"time"
)

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/seatbook")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 40))
	t.Setenv("ADMIN_EMAIL", "admin@example.com")
	t.Setenv("ADMIN_PASSWORD", "LongPassword123!")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("HOLD_SECONDS", "")
	t.Setenv("COOKIE_SECURE", "")
}
func TestDefaults(t *testing.T) {
	validEnv(t)
	c, err := Load()
	if err != nil || c.Hold != 5*time.Minute || c.Origin != "http://localhost:8080" || c.Secure {
		t.Fatalf("config: %+v %v", c, err)
	}
}
func TestRejectInvalid(t *testing.T) {
	for _, tc := range [][2]string{{"JWT_SECRET", "short"}, {"DATABASE_URL", ""}, {"ADMIN_EMAIL", "bad"}, {"ADMIN_PASSWORD", "short"}, {"HOLD_SECONDS", "0"}, {"HOLD_SECONDS", "3601"}, {"HOLD_SECONDS", "abc"}, {"APP_ORIGIN", "https://example.com/path"}, {"APP_ORIGIN", "https://example.com/"}, {"APP_ORIGIN", "ftp://example.com"}, {"COOKIE_SECURE", "yes"}} {
		t.Run(tc[0]+tc[1], func(t *testing.T) {
			validEnv(t)
			t.Setenv(tc[0], tc[1])
			if _, err := Load(); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
