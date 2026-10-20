# Architecture and consistency

One Go application serves the embedded frontend and JSON API. PostgreSQL stores all persistent state. A goroutine periodically releases expired holds. No Redis, broker or separate frontend toolchain is required.

HTTP handlers authenticate/authorize, decode and validate requests. The service layer validates reservation commands. The store implements the transactional SQL invariants. `cmd/api` handles configuration, migration, bootstrap and lifecycle.

## Persistence

`users → bookings → seats → events` connects a visitor to a seat at a particular event. Seat uniqueness is scoped to `(event_id,row_no,seat_no)`. The partial unique index on `bookings(seat_id)` covers only `pending` and `confirmed` rows; historical cancelled/expired bookings remain for display and idempotency.

A version-1 schema is embedded in the binary and applied in a transaction under a startup advisory lock. Running it twice is safe. This project has one schema version; later schema changes should be introduced as explicit versioned migrations rather than by editing an already deployed version.

## Reservation transaction

1. Lock a user/key advisory identifier. Hash collisions only serialize unrelated requests; they cannot merge records.
2. Find an existing booking for that user/key. Return it if its seat matches; otherwise return a key-reuse conflict.
3. Lock the target **seat** row with `FOR UPDATE`.
4. Check event start using PostgreSQL clock time after obtaining the lock.
5. Mark old, expired pending bookings for this seat as expired.
6. Refuse if another pending/confirmed booking still owns the seat.
7. Insert a pending booking with a database-generated deadline; commit.

The database unique index provides a second line of defense if another writer fails to follow the application locking rules. Multiple instances coordinate through PostgreSQL, not a Go mutex. The session rate limiter is the only intentionally instance-local control.

## Confirmation, cancellation, expiry

Confirmation and cancellation first find an owned booking, lock its seat, then re-read its state. An expired pending booking is changed to expired and cannot be confirmed. The transaction commits this expiration before returning a conflict. Confirmed/cancelled states are idempotent only for their matching action.

The sweeper locks up to 100 eligible seats in seat-id order using `SKIP LOCKED`, then updates their expired pending bookings. It uses the same seat-first order as user operations. A busy seat is handled by a later batch. A crash does not lose deadlines because they are in PostgreSQL. Reads report overdue pending bookings as expired even before physical cleanup.

## Client behavior

The grid refreshes every 5 seconds. It is a hint: the reservation transaction is authoritative. The UI keeps the same idempotency key after a failed network/server request, and replaces it for a new user selection. Dates are displayed in the browser's timezone. Server timestamps drive the countdown with a best-effort clock offset; the database alone enforces expiry.

The application embeds HTML/CSS/JS, so rebuilding Go is required to include frontend edits. Browser code uses DOM `textContent` for event/user data and does not inject event descriptions as HTML.

## Deliberate scope

One seat per booking; one fixed currency; no real payment effects; no refunds; immutable events after creation; no mail delivery or password resets. This keeps the main learning goal focused on transactional consistency, authorization, idempotency and a usable small frontend.
