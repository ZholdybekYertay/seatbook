package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ZholdybekYertay/seatbook/internal/config"
	"github.com/ZholdybekYertay/seatbook/internal/service"
	"github.com/ZholdybekYertay/seatbook/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type API struct {
	Store   *store.Store
	Service *service.Service
	Config  config.Config
	limiter *limiter
}
type ctxKey struct{}

func New(s *store.Store, c config.Config, static http.Handler) http.Handler {
	a := &API{Store: s, Service: &service.Service{Store: s}, Config: c, limiter: &limiter{entries: make(map[string]window)}}
	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.Ping(r.Context()); err != nil {
			write(w, 503, map[string]string{"error": "database unavailable"})
			return
		}
		write(w, 200, map[string]string{"status": "ready"})
	})
	m.HandleFunc("POST /api/auth/register", a.register)
	m.HandleFunc("POST /api/auth/login", a.login)
	m.HandleFunc("POST /api/auth/logout", a.logout)
	m.Handle("GET /api/me", a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { write(w, 200, user(r)) })))
	m.HandleFunc("GET /api/events", a.events)
	m.HandleFunc("GET /api/events/{id}", a.event)
	m.Handle("POST /api/admin/events", a.auth(http.HandlerFunc(a.createEvent)))
	m.Handle("POST /api/bookings", a.auth(http.HandlerFunc(a.reserve)))
	m.Handle("GET /api/bookings", a.auth(http.HandlerFunc(a.bookings)))
	m.Handle("POST /api/bookings/{id}/confirm", a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { a.transition(w, r, "confirm") })))
	m.Handle("POST /api/bookings/{id}/cancel", a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { a.transition(w, r, "cancel") })))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		write(w, 404, map[string]string{"error": "endpoint not found"})
	})
	m.Handle("/", static)
	return a.middleware(m)
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := strings.Split(r.Header.Get("Content-Type"), ";")[0]; strings.TrimSpace(ct) != "application/json" {
		fail(w, 415, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid JSON or body too large")
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, "body must contain one JSON object")
		return false
	}
	return true
}
func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if origin := r.Header.Get("Origin"); origin != "" && origin != a.Config.Origin {
				fail(w, 403, "untrusted request origin")
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "cross-site request rejected")
				return
			}
		}
		defer func() {
			if e := recover(); e != nil {
				slog.Error("request panic", "error", e)
				fail(w, 500, "internal server error")
			}
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("seatbook_session")
		if err != nil {
			fail(w, 401, "login required")
			return
		}
		claims := &jwt.RegisteredClaims{}
		_, err = jwt.ParseWithClaims(cookie.Value, claims, func(t *jwt.Token) (any, error) { return []byte(a.Config.Secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("seatbook"), jwt.WithExpirationRequired())
		if err != nil {
			fail(w, 401, "session expired; please log in")
			return
		}
		id, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil || id < 1 {
			fail(w, 401, "invalid session")
			return
		}
		u, err := a.Store.UserByID(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, 401, "invalid session")
			return
		}
		if err != nil {
			a.error(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}
func user(r *http.Request) store.User { return r.Context().Value(ctxKey{}).(store.User) }
func (a *API) session(w http.ResponseWriter, u store.User) error {
	claims := jwt.RegisteredClaims{Issuer: "seatbook", Subject: strconv.FormatInt(u.ID, 10), IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(8 * time.Hour))}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.Config.Secret))
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "seatbook_session", Value: token, Path: "/", HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	return nil
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func validCredentials(c *credentials) bool {
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	a, e := mail.ParseAddress(c.Email)
	return e == nil && a.Address == c.Email && len(c.Email) <= 254 && len(c.Password) >= 10 && len(c.Password) <= 72
}
func (a *API) authLimit(w http.ResponseWriter, r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !a.limiter.allow(ip, time.Now()) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "too many attempts; try again in a minute")
		return false
	}
	return true
}
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if !a.authLimit(w, r) {
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	if !validCredentials(&c) {
		fail(w, 400, "valid email and password of 10..72 bytes required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		a.error(w, err)
		return
	}
	u, err := a.Store.CreateUser(r.Context(), c.Email, string(hash))
	if err != nil {
		a.error(w, err)
		return
	}
	if err = a.session(w, u); err != nil {
		a.error(w, err)
		return
	}
	write(w, 201, u)
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if !a.authLimit(w, r) {
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	if !validCredentials(&c) {
		fail(w, 401, "invalid email or password")
		return
	}
	u, err := a.Store.UserByEmail(r.Context(), c.Email)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 401, "invalid email or password")
		return
	}
	if err != nil {
		a.error(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(c.Password)) != nil {
		fail(w, 401, "invalid email or password")
		return
	}
	if err = a.session(w, u); err != nil {
		a.error(w, err)
		return
	}
	write(w, 200, u)
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "seatbook_session", Value: "", Path: "/", HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	write(w, 200, map[string]bool{"ok": true})
}
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	es, err := a.Store.Events(r.Context())
	if err != nil {
		a.error(w, err)
		return
	}
	write(w, 200, es)
}
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid id")
		return 0, false
	}
	return id, true
}
func (a *API) event(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	e, seats, err := a.Store.Event(r.Context(), id)
	if err != nil {
		a.error(w, err)
		return
	}
	write(w, 200, map[string]any{"event": e, "seats": seats, "server_time": time.Now().UTC()})
}
func (a *API) createEvent(w http.ResponseWriter, r *http.Request) {
	if user(r).Role != "admin" {
		fail(w, 403, "admin role required")
		return
	}
	var input struct {
		Title       string    `json:"title"`
		Description string    `json:"description"`
		StartsAt    time.Time `json:"starts_at"`
		PriceCents  int       `json:"price_cents"`
		Rows        int       `json:"rows"`
		Columns     int       `json:"columns"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if !utf8.ValidString(input.Title) || utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 120 || !utf8.ValidString(input.Description) || utf8.RuneCountInString(input.Description) > 2000 || !input.StartsAt.After(time.Now().Add(time.Minute)) || input.PriceCents < 0 || input.PriceCents > 100000000 || input.Rows < 1 || input.Rows > 12 || input.Columns < 1 || input.Columns > 16 {
		fail(w, 400, "invalid event: title 1..120, description <=2000, future date, rows 1..12, columns 1..16, price 0..100000000 cents")
		return
	}
	id, err := a.Store.CreateEvent(r.Context(), store.Event{Title: input.Title, Description: input.Description, StartsAt: input.StartsAt, PriceCents: input.PriceCents}, input.Rows, input.Columns)
	if err != nil {
		a.error(w, err)
		return
	}
	write(w, 201, map[string]int64{"id": id})
}
func (a *API) reserve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SeatID int64 `json:"seat_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	b, replay, err := a.Service.Reserve(r.Context(), user(r).ID, in.SeatID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		a.error(w, err)
		return
	}
	status := 201
	if replay {
		status = 200
		w.Header().Set("Idempotency-Replayed", "true")
	}
	write(w, status, b)
}
func (a *API) bookings(w http.ResponseWriter, r *http.Request) {
	var before int64
	if text := r.URL.Query().Get("before"); text != "" {
		var err error
		before, err = strconv.ParseInt(text, 10, 64)
		if err != nil || before < 1 {
			fail(w, 400, "invalid pagination cursor")
			return
		}
	}
	bs, err := a.Store.Bookings(r.Context(), user(r).ID, before)
	if err != nil {
		a.error(w, err)
		return
	}
	write(w, 200, bs)
}
func (a *API) transition(w http.ResponseWriter, r *http.Request, action string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := a.Service.Transition(r.Context(), user(r).ID, id, action)
	if err != nil {
		a.error(w, err)
		return
	}
	write(w, 200, b)
}
func (a *API) error(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalid):
		fail(w, 400, "seat_id must be positive; Idempotency-Key must be 8..80 letters, digits, '-' or '_'")
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "not found")
	case errors.Is(err, store.ErrKeyReuse):
		fail(w, 409, err.Error())
	case errors.Is(err, store.ErrExpired):
		fail(w, 409, "booking expired")
	case errors.Is(err, store.ErrConflict):
		fail(w, 409, "seat unavailable, email already registered, or transition not allowed")
	default:
		slog.Error("request failed", "error", err)
		fail(w, 500, "internal server error")
	}
}

type window struct {
	start time.Time
	count int
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]window
}

func (l *limiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= 4096 {
		for k, v := range l.entries {
			if now.Sub(v.start) >= time.Minute {
				delete(l.entries, k)
			}
		}
		if _, ok := l.entries[ip]; !ok && len(l.entries) >= 4096 {
			return false
		}
	}
	v := l.entries[ip]
	if now.Sub(v.start) >= time.Minute {
		v = window{start: now}
	}
	v.count++
	l.entries[ip] = v
	return v.count <= 20
}
