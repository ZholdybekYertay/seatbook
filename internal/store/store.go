package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrExpired  = errors.New("booking expired")
	ErrKeyReuse = errors.New("idempotency key already used for another seat")
)

type Store struct {
	DB   *pgxpool.Pool
	Hold time.Duration
}
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
}
type Event struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	StartsAt    time.Time `json:"starts_at"`
	PriceCents  int       `json:"price_cents"`
	Total       int       `json:"total_seats"`
	Available   int       `json:"available_seats"`
}
type Seat struct {
	ID     int64  `json:"id"`
	Row    int    `json:"row"`
	Number int    `json:"number"`
	Status string `json:"status"`
}
type Booking struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	SeatID     int64     `json:"seat_id"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	EventID    int64     `json:"event_id"`
	EventTitle string    `json:"event_title"`
	StartsAt   time.Time `json:"starts_at"`
	Row        int       `json:"row"`
	Number     int       `json:"number"`
	PriceCents int       `json:"price_cents"`
}

func Open(ctx context.Context, url string, hold time.Duration) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 24
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, Hold: hold}, nil
}
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(72834910)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func unique(err error) bool { var e *pgconn.PgError; return errors.As(err, &e) && e.Code == "23505" }
func (s *Store) CreateUser(ctx context.Context, email, hash string) (User, error) {
	var u User
	err := s.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,$2) RETURNING id,email,role`, email, hash).Scan(&u.ID, &u.Email, &u.Role)
	if unique(err) {
		return u, ErrConflict
	}
	return u, err
}
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.DB.QueryRow(ctx, `SELECT id,email,role,password_hash FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.Role, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return u, err
}
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.DB.QueryRow(ctx, `SELECT id,email,role FROM users WHERE id=$1`, id).Scan(&u.ID, &u.Email, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return u, err
}
func (s *Store) Bootstrap(ctx context.Context, email, hash string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(72834911)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,$2,'admin') ON CONFLICT(email) DO NOTHING`, email, hash); err != nil {
		return err
	}
	// Seed once, including across simultaneous application starts.
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM events)`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		for i, title := range []string{"Go Community Meetup", "Backend Architecture Night", "Live Jazz Session"} {
			var id int64
			err = tx.QueryRow(ctx, `INSERT INTO events(title,description,starts_at,price_cents) VALUES($1,$2,now()+make_interval(days=>$3),$4) RETURNING id`, title, "A small event. Choose your seat and make a reservation.", 7+i*2, 250000+i*100000).Scan(&id)
			if err != nil {
				return err
			}
			if err = insertSeats(ctx, tx, id, 5, 8); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
func insertSeats(ctx context.Context, tx pgx.Tx, id int64, rows, cols int) error {
	_, err := tx.Exec(ctx, `INSERT INTO seats(event_id,row_no,seat_no) SELECT $1,r,c FROM generate_series(1,$2::int) r CROSS JOIN generate_series(1,$3::int) c`, id, rows, cols)
	return err
}
func (s *Store) CreateEvent(ctx context.Context, e Event, rows, cols int) (int64, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO events(title,description,starts_at,price_cents) VALUES($1,$2,$3,$4) RETURNING id`, e.Title, e.Description, e.StartsAt, e.PriceCents).Scan(&id)
	if err != nil {
		return 0, err
	}
	if err = insertSeats(ctx, tx, id, rows, cols); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

const eventQuery = `SELECT e.id,e.title,e.description,e.starts_at,e.price_cents,count(s.id)::int,
 count(s.id) FILTER(WHERE b.id IS NULL)::int FROM events e LEFT JOIN seats s ON s.event_id=e.id
 LEFT JOIN bookings b ON b.seat_id=s.id AND (b.status='confirmed' OR (b.status='pending' AND b.expires_at>statement_timestamp()))`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.Title, &e.Description, &e.StartsAt, &e.PriceCents, &e.Total, &e.Available)
	return e, err
}
func (s *Store) Events(ctx context.Context) ([]Event, error) {
	rows, err := s.DB.Query(ctx, eventQuery+` WHERE e.starts_at>statement_timestamp() GROUP BY e.id ORDER BY e.starts_at,e.id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) Event(ctx context.Context, id int64) (Event, []Seat, error) {
	e, err := scanEvent(s.DB.QueryRow(ctx, eventQuery+` WHERE e.id=$1 GROUP BY e.id`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return e, nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT s.id,s.row_no,s.seat_no,COALESCE(b.status,'available') FROM seats s LEFT JOIN bookings b ON b.seat_id=s.id AND (b.status='confirmed' OR (b.status='pending' AND b.expires_at>statement_timestamp())) WHERE s.event_id=$1 ORDER BY s.row_no,s.seat_no`, id)
	if err != nil {
		return e, nil, err
	}
	defer rows.Close()
	out := []Seat{}
	for rows.Next() {
		var seat Seat
		if err = rows.Scan(&seat.ID, &seat.Row, &seat.Number, &seat.Status); err != nil {
			return e, nil, err
		}
		out = append(out, seat)
	}
	return e, out, rows.Err()
}

const bookingQuery = `SELECT b.id,b.user_id,b.seat_id,CASE WHEN b.status='pending' AND b.expires_at<=statement_timestamp() THEN 'expired' ELSE b.status END,b.expires_at,b.created_at,e.id,e.title,e.starts_at,s.row_no,s.seat_no,e.price_cents FROM bookings b JOIN seats s ON s.id=b.seat_id JOIN events e ON e.id=s.event_id`

func scanBooking(row pgx.Row) (Booking, error) {
	var b Booking
	err := row.Scan(&b.ID, &b.UserID, &b.SeatID, &b.Status, &b.ExpiresAt, &b.CreatedAt, &b.EventID, &b.EventTitle, &b.StartsAt, &b.Row, &b.Number, &b.PriceCents)
	return b, err
}
func (s *Store) Booking(ctx context.Context, user, id int64) (Booking, error) {
	b, err := scanBooking(s.DB.QueryRow(ctx, bookingQuery+` WHERE b.id=$1 AND b.user_id=$2`, id, user))
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}
func (s *Store) Bookings(ctx context.Context, user, before int64) ([]Booking, error) {
	rows, err := s.DB.Query(ctx, bookingQuery+` WHERE b.user_id=$1 AND ($2::bigint=0 OR b.id<$2) ORDER BY b.id DESC LIMIT 100`, user, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Booking{}
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// All seat mutations lock the seat first; the partial unique index is a second guard.
func (s *Store) Reserve(ctx context.Context, user, seat int64, key string) (Booking, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Booking{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,42))`, fmt.Sprintf("%d:%s", user, key)); err != nil {
		return Booking{}, false, err
	}
	var id, oldSeat int64
	err = tx.QueryRow(ctx, `SELECT id,seat_id FROM bookings WHERE user_id=$1 AND request_key=$2`, user, key).Scan(&id, &oldSeat)
	if err == nil {
		if oldSeat != seat {
			return Booking{}, false, ErrKeyReuse
		}
		if err = tx.Commit(ctx); err != nil {
			return Booking{}, false, err
		}
		b, e := s.Booking(ctx, user, id)
		return b, true, e
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Booking{}, false, err
	}
	var eventID int64
	err = tx.QueryRow(ctx, `SELECT event_id FROM seats WHERE id=$1 FOR UPDATE`, seat).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Booking{}, false, ErrNotFound
	}
	if err != nil {
		return Booking{}, false, err
	}
	var future bool
	if err = tx.QueryRow(ctx, `SELECT starts_at>clock_timestamp() FROM events WHERE id=$1`, eventID).Scan(&future); err != nil {
		return Booking{}, false, err
	}
	if !future {
		return Booking{}, false, ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE bookings SET status='expired',updated_at=clock_timestamp() WHERE seat_id=$1 AND status='pending' AND expires_at<=clock_timestamp()`, seat); err != nil {
		return Booking{}, false, err
	}
	var occupied bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bookings WHERE seat_id=$1 AND status IN ('pending','confirmed'))`, seat).Scan(&occupied); err != nil {
		return Booking{}, false, err
	}
	if occupied {
		return Booking{}, false, ErrConflict
	}
	err = tx.QueryRow(ctx, `INSERT INTO bookings(user_id,seat_id,request_key,status,expires_at) VALUES($1,$2,$3,'pending',clock_timestamp()+make_interval(secs=>$4)) RETURNING id`, user, seat, key, s.Hold.Seconds()).Scan(&id)
	if unique(err) {
		return Booking{}, false, ErrConflict
	}
	if err != nil {
		return Booking{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Booking{}, false, err
	}
	b, err := s.Booking(ctx, user, id)
	return b, false, err
}
func (s *Store) Transition(ctx context.Context, user, id int64, action string) (Booking, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Booking{}, err
	}
	defer tx.Rollback(ctx)
	var seat int64
	if err = tx.QueryRow(ctx, `SELECT seat_id FROM bookings WHERE id=$1 AND user_id=$2`, id, user).Scan(&seat); errors.Is(err, pgx.ErrNoRows) {
		return Booking{}, ErrNotFound
	} else if err != nil {
		return Booking{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM seats WHERE id=$1 FOR UPDATE`, seat); err != nil {
		return Booking{}, err
	}
	var status string
	var expired bool
	var future bool
	err = tx.QueryRow(ctx, `SELECT b.status,b.expires_at<=clock_timestamp(),e.starts_at>clock_timestamp() FROM bookings b JOIN seats s ON s.id=b.seat_id JOIN events e ON e.id=s.event_id WHERE b.id=$1`, id).Scan(&status, &expired, &future)
	if err != nil {
		return Booking{}, err
	}
	if status == "pending" && expired {
		if _, err = tx.Exec(ctx, `UPDATE bookings SET status='expired',updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
			return Booking{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return Booking{}, err
		}
		return Booking{}, ErrExpired
	}
	switch action {
	case "confirm":
		if status != "pending" && status != "confirmed" {
			return Booking{}, ErrConflict
		}
		if !future {
			return Booking{}, ErrConflict
		}
		status = "confirmed"
	case "cancel":
		if status != "pending" && status != "cancelled" {
			return Booking{}, ErrConflict
		}
		status = "cancelled"
	default:
		return Booking{}, ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE bookings SET status=$1,updated_at=clock_timestamp() WHERE id=$2`, status, id); err != nil {
		return Booking{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Booking{}, err
	}
	return s.Booking(ctx, user, id)
}

// Each batch uses the same seat-first lock order; SKIP LOCKED avoids waiting for users.
func (s *Store) Expire(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT s.id FROM seats s WHERE EXISTS(SELECT 1 FROM bookings b WHERE b.seat_id=s.id AND b.status='pending' AND b.expires_at<=clock_timestamp()) ORDER BY s.id LIMIT 100 FOR UPDATE OF s SKIP LOCKED`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		_, err = tx.Exec(ctx, `UPDATE bookings SET status='expired',updated_at=clock_timestamp() WHERE seat_id=ANY($1::bigint[]) AND status='pending' AND expires_at<=clock_timestamp()`, ids)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
