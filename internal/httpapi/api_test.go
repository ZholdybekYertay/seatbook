package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ZholdybekYertay/seatbook/internal/config"
	"github.com/ZholdybekYertay/seatbook/internal/store"
	"github.com/ZholdybekYertay/seatbook/web"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig() config.Config {
	return config.Config{Secret: strings.Repeat("k", 40), Origin: "http://localhost:8080"}
}
func TestRejectsUntrustedOrigins(t *testing.T) {
	h := New(nil, testConfig(), web.Handler())
	for _, origin := range []string{"https://evil.example", "http://localhost:8080.evil.example", "null"} {
		r := httptest.NewRequest("POST", "/api/auth/logout", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("origin %q: %d", origin, w.Code)
		}
	}
}
func TestRejectsBadTokens(t *testing.T) {
	for _, tc := range []struct {
		method  jwt.SigningMethod
		issuer  string
		expires time.Time
	}{{jwt.SigningMethodHS384, "seatbook", time.Now().Add(time.Hour)}, {jwt.SigningMethodHS256, "wrong", time.Now().Add(time.Hour)}, {jwt.SigningMethodHS256, "seatbook", time.Now().Add(-time.Hour)}} {
		c := jwt.RegisteredClaims{Subject: "1", Issuer: tc.issuer, ExpiresAt: jwt.NewNumericDate(tc.expires)}
		token, _ := jwt.NewWithClaims(tc.method, c).SignedString([]byte(testConfig().Secret))
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.AddCookie(&http.Cookie{Name: "seatbook_session", Value: token})
		w := httptest.NewRecorder()
		New(nil, testConfig(), web.Handler()).ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("bad token: %d", w.Code)
		}
	}
}
func TestStaticAssets(t *testing.T) {
	h := New(nil, testConfig(), web.Handler())
	for _, path := range []string{"/", "/app.js", "/style.css", "/favicon.svg"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("asset %s: %d", path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
			t.Fatal("missing CSP")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/embed.go", nil))
	if w.Code != 404 {
		t.Fatal("source exposed")
	}
}
func TestLimiter(t *testing.T) {
	l := &limiter{entries: map[string]window{}}
	now := time.Now()
	for i := 0; i < 20; i++ {
		if !l.allow("ip", now) {
			t.Fatal("early block")
		}
	}
	if l.allow("ip", now) {
		t.Fatal("missing limit")
	}
	if !l.allow("ip", now.Add(time.Minute)) {
		t.Fatal("window did not reset")
	}
}
func TestCredentials(t *testing.T) {
	for _, tc := range []struct {
		email, password string
		want            bool
	}{{" USER@Example.com ", "0123456789", true}, {"Name <user@example.com>", "0123456789", false}, {"user@example.com", "short", false}, {"bad", "0123456789", false}, {"user@example.com", strings.Repeat("a", 73), false}} {
		c := credentials{tc.email, tc.password}
		if validCredentials(&c) != tc.want {
			t.Errorf("validation: %q", tc.email)
		}
	}
}

type reply struct {
	code int
	body []byte
	err  error
}

func request(c *http.Client, method, path, body, key string) reply {
	r, e := http.NewRequest(method, path, strings.NewReader(body))
	if e != nil {
		return reply{err: e}
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	res, e := c.Do(r)
	if e != nil {
		return reply{err: e}
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	return reply{res.StatusCode, b, e}
}
func expect(t *testing.T, r reply, status int) {
	t.Helper()
	if r.err != nil || r.code != status {
		t.Fatalf("expected %d, got %d: %s (%v)", status, r.code, r.body, r.err)
	}
}
func booking(t *testing.T, r reply) store.Booking {
	t.Helper()
	var b store.Booking
	if err := json.Unmarshal(r.body, &b); err != nil {
		t.Fatal(err)
	}
	return b
}
func client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}
}

// Integration tests use an isolated schema; they never truncate existing tables.
func TestIntegration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := fmt.Sprintf("seatbook_test_%d", time.Now().UnixNano())
	if _, err = root.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := store.Open(ctx, u.String(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("AdminPass1234!"), bcrypt.MinCost)
	for i := 0; i < 2; i++ {
		if err = s.Bootstrap(ctx, "admin@example.com", string(hash)); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(New(s, testConfig(), web.Handler()))
	defer ts.Close()
	base := ts.URL
	c, other, admin := client(), client(), client()
	expect(t, request(c, "POST", base+"/api/auth/register", `{"email":"user@example.com","password":"UserPass1234!"}`, ""), 201)
	expect(t, request(other, "POST", base+"/api/auth/register", `{"email":"other@example.com","password":"OtherPass1234!"}`, ""), 201)
	expect(t, request(admin, "POST", base+"/api/auth/login", `{"email":"admin@example.com","password":"AdminPass1234!"}`, ""), 200)
	r := request(c, "GET", base+"/api/me", "", "")
	expect(t, r, 200)
	var owner store.User
	if err = json.Unmarshal(r.body, &owner); err != nil {
		t.Fatal(err)
	}
	eventID, err := s.CreateEvent(ctx, store.Event{Title: "Concurrency test", StartsAt: time.Now().Add(time.Hour), PriceCents: 150000}, 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	_, seats, err := s.Event(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(c *http.Client, seat int64, key string) reply {
		return request(c, "POST", base+"/api/bookings", fmt.Sprintf(`{"seat_id":%d}`, seat), key)
	}
	t.Run("validation-and-authorization", func(t *testing.T) {
		expect(t, request(client(), "POST", base+"/api/bookings", `{"seat_id":1}`, "valid-key"), 401)
		expect(t, request(c, "POST", base+"/api/admin/events", `{}`, ""), 403)
		expect(t, reserve(c, seats[0].ID, "bad"), 400)
		expect(t, request(c, "POST", base+"/api/bookings", `{"seat_id":1,"extra":true}`, "valid-key"), 400)
		expect(t, request(c, "POST", base+"/api/bookings", `{"seat_id":1} {}`, "valid-key"), 400)
		expect(t, request(c, "POST", base+"/api/bookings", `{"seat_id":`+strings.Repeat("1", 18000)+`}`, "valid-key"), 400)
		expect(t, reserve(c, 9223372036854775807, "missing-seat"), 404)
		expect(t, request(c, "GET", base+"/api/bookings?before=abc", "", ""), 400)
	})
	t.Run("20-requests-one-seat", func(t *testing.T) {
		replies := make(chan reply, 20)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				replies <- reserve(c, seats[0].ID, fmt.Sprintf("race-key-%03d", i))
			}(i)
		}
		close(start)
		wg.Wait()
		close(replies)
		wins, conflicts := 0, 0
		var winner store.Booking
		for r := range replies {
			if r.err != nil {
				t.Fatal(r.err)
			}
			switch r.code {
			case 201:
				wins++
				winner = booking(t, r)
			case 409:
				conflicts++
			default:
				t.Fatalf("unexpected %d: %s", r.code, r.body)
			}
		}
		if wins != 1 || conflicts != 19 {
			t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
		}
		expect(t, request(other, "POST", fmt.Sprintf("%s/api/bookings/%d/cancel", base, winner.ID), "", ""), 404)
		expect(t, request(c, "POST", fmt.Sprintf("%s/api/bookings/%d/cancel", base, winner.ID), "", ""), 200)
		expect(t, reserve(c, seats[0].ID, "after-cancel"), 201)
	})
	t.Run("idempotency-under-concurrency", func(t *testing.T) {
		replies := make(chan reply, 20)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); replies <- reserve(c, seats[1].ID, "same-key-123") }()
		}
		wg.Wait()
		close(replies)
		var id int64
		created := 0
		for r := range replies {
			if r.err != nil || r.code != 200 && r.code != 201 {
				t.Fatalf("replay: %d %s %v", r.code, r.body, r.err)
			}
			if r.code == 201 {
				created++
			}
			b := booking(t, r)
			if id != 0 && id != b.ID {
				t.Fatal("duplicate booking")
			}
			id = b.ID
		}
		if created != 1 {
			t.Fatalf("created=%d", created)
		}
		expect(t, reserve(c, seats[2].ID, "same-key-123"), 409)
		path := fmt.Sprintf("%s/api/bookings/%d", base, id)
		expect(t, request(c, "POST", path+"/confirm", "", ""), 200)
		expect(t, request(c, "POST", path+"/confirm", "", ""), 200)
		expect(t, request(c, "POST", path+"/cancel", "", ""), 409)
	})
	t.Run("expiry-and-rebooking", func(t *testing.T) {
		r := reserve(c, seats[3].ID, "expire-key-1")
		expect(t, r, 201)
		b := booking(t, r)
		if _, err = s.DB.Exec(ctx, `UPDATE bookings SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, b.ID); err != nil {
			t.Fatal(err)
		}
		expect(t, request(c, "POST", fmt.Sprintf("%s/api/bookings/%d/confirm", base, b.ID), "", ""), 409)
		expect(t, reserve(other, seats[3].ID, "expire-key-2"), 201)
		r = reserve(c, seats[3].ID, "expire-key-1")
		expect(t, r, 200)
		if booking(t, r).Status != "expired" {
			t.Fatal("replay changed original booking")
		}
	})
	t.Run("sweeper-vs-confirm-vs-rebook", func(t *testing.T) {
		for i := 4; i < 14; i++ {
			r := reserve(c, seats[i].ID, fmt.Sprintf("expired-race-%d", i))
			expect(t, r, 201)
			b := booking(t, r)
			if _, err = s.DB.Exec(ctx, `UPDATE bookings SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, b.ID); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 1)
			rs := make(chan reply, 2)
			start := make(chan struct{})
			wg.Add(3)
			go func() { defer wg.Done(); <-start; errs <- s.Expire(ctx) }()
			go func() {
				defer wg.Done()
				<-start
				rs <- request(c, "POST", fmt.Sprintf("%s/api/bookings/%d/confirm", base, b.ID), "", "")
			}()
			go func() { defer wg.Done(); <-start; rs <- reserve(other, seats[i].ID, fmt.Sprintf("replacement-%d", i)) }()
			close(start)
			wg.Wait()
			if err = <-errs; err != nil {
				t.Fatal(err)
			}
			r1, r2 := <-rs, <-rs
			if !((r1.code == 409 && r2.code == 201) || (r1.code == 201 && r2.code == 409)) {
				t.Fatalf("expiry race: %d %s / %d %s", r1.code, r1.body, r2.code, r2.body)
			}
			var active int
			if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM bookings WHERE seat_id=$1 AND status IN ('pending','confirmed')`, seats[i].ID).Scan(&active); err != nil || active != 1 {
				t.Fatalf("active=%d err=%v", active, err)
			}
		}
	})
	t.Run("admin-and-login", func(t *testing.T) {
		input := fmt.Sprintf(`{"title":"New event","description":"test","starts_at":%q,"price_cents":1000,"rows":2,"columns":3}`, time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
		r := request(admin, "POST", base+"/api/admin/events", input, "")
		expect(t, r, 201)
		var result struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(r.body, &result)
		_, ss, err := s.Event(ctx, result.ID)
		if err != nil || len(ss) != 6 {
			t.Fatalf("seats: %d %v", len(ss), err)
		}
		expect(t, request(c, "POST", base+"/api/auth/logout", "", ""), 200)
		expect(t, request(c, "GET", base+"/api/me", "", ""), 401)
		expect(t, request(c, "POST", base+"/api/auth/login", `{"email":"user@example.com","password":"WrongPass1234!"}`, ""), 401)
		expect(t, request(c, "POST", base+"/api/auth/login", `{"email":"USER@example.com","password":"UserPass1234!"}`, ""), 200)
		expect(t, request(c, "GET", base+"/api/bookings", "", ""), 200)
	})
	t.Run("database-constraint", func(t *testing.T) {
		_, err = s.DB.Exec(ctx, `INSERT INTO bookings(user_id,seat_id,request_key,status,expires_at) VALUES($1,$2,'index-guard','pending',now()+interval '1 minute')`, owner.ID, seats[1].ID)
		if err == nil {
			t.Fatal("database permitted double booking")
		}
	})
}
