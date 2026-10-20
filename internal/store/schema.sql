CREATE TABLE IF NOT EXISTS schema_versions (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS users (
 id bigserial PRIMARY KEY, email text NOT NULL UNIQUE, password_hash text NOT NULL,
 role text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS events (
 id bigserial PRIMARY KEY, title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
 description text NOT NULL DEFAULT '', starts_at timestamptz NOT NULL,
 price_cents integer NOT NULL CHECK (price_cents BETWEEN 0 AND 100000000),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS seats (
 id bigserial PRIMARY KEY, event_id bigint NOT NULL REFERENCES events(id),
 row_no integer NOT NULL CHECK (row_no BETWEEN 1 AND 12),
 seat_no integer NOT NULL CHECK (seat_no BETWEEN 1 AND 16),
 UNIQUE(event_id,row_no,seat_no)
);
CREATE TABLE IF NOT EXISTS bookings (
 id bigserial PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id),
 seat_id bigint NOT NULL REFERENCES seats(id), request_key text NOT NULL,
 status text NOT NULL CHECK (status IN ('pending','confirmed','cancelled','expired')),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(user_id,request_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS one_live_booking_per_seat ON bookings(seat_id) WHERE status IN ('pending','confirmed');
CREATE INDEX IF NOT EXISTS booking_user_page ON bookings(user_id,id DESC);
CREATE INDEX IF NOT EXISTS pending_expiration ON bookings(expires_at) WHERE status='pending';
INSERT INTO schema_versions(version) VALUES(1) ON CONFLICT DO NOTHING;
