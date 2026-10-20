# SeatBook

A full-stack seat reservation project built with **Go, PostgreSQL and vanilla JavaScript**. Users select a seat, get a five-minute hold and confirm it through a simulated checkout. The backend protects against double booking and repeated requests.

**Windows quick start:** open [START_HERE_RU.md](START_HERE_RU.md).

## Run

Install Docker Desktop with the Linux engine and Docker Compose v2.20+.

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start.ps1
```

Open **http://localhost:8080**. The first build downloads Go modules and container images. The helper generates fresh database/JWT secrets in `.env` without overwriting existing settings.

Local demo organizer: `admin@seatbook.local` / `SeatBookDemo2026!`.
Register a separate account to use the app as a regular visitor.

Alternatively:

```powershell
Copy-Item .env.example .env
docker compose up --build -d --wait
```

The manual alternative uses the example secrets. Both methods are for local development. Open the app using `localhost`, matching `APP_ORIGIN`.

## Features

- Email/password registration; bcrypt hashing; JWT in an HttpOnly, SameSite=Strict cookie.
- User/admin authorization; booking ownership checks.
- Upcoming event catalog, seating grid, organizer event creation.
- Five-minute pending reservations, simulated confirmation and release.
- PostgreSQL transactions and seat row locks; partial unique index prevents duplicate live bookings.
- User-scoped `Idempotency-Key`; repeating the same key/seat returns the existing booking.
- Expiration enforced on the request path plus an independent cleanup worker.
- Accessible labels, responsive layout, countdown, live availability polling.
- Embedded static files: one Go binary serves the frontend and API.
- Bounded JSON bodies, strict decoding, origin checks, authentication rate limit, timeouts and graceful shutdown.
- Integration tests, concurrency tests, race detector and GitHub Actions.

## Rules

Each booking covers **one seat**. Pending holds may be confirmed or cancelled. A confirmed booking is final in this demo; cancellation/refunds of confirmed tickets are deliberately outside its scope. A new reservation after cancellation/expiry needs a **new idempotency key**. Replaying an old key returns that original booking, including its current terminal status. Reusing a key for a different seat returns 409.

Expiration uses PostgreSQL time, checked **after acquiring the seat lock**. Even if cleanup is stopped, expired holds cannot be confirmed and do not prevent a new reservation. Confirm/release/expiry use the same seat-first lock order. The unique index is a separate database guard.

Checkout has no payment provider and never charges money. No email service, password reset, refresh tokens, assigned multi-seat carts or event editing are included. Logout clears the browser cookie; an already copied JWT remains valid until its 8-hour expiry. The in-memory login limiter is per app instance, suitable for this local single-instance demo.

## Structure

```text
cmd/api/                 Startup and background expiry worker
internal/config/         Environment parsing
internal/httpapi/        HTTP routes, cookies, authorization and tests
internal/service/        Reservation input/business entry points
internal/store/          PostgreSQL transactions and schema
web/                     Embedded HTML, CSS, JavaScript and favicon
scripts/                 Windows setup and start helpers
docs/                    API, architecture, verification and learning guide
```

## Development without Docker for the Go app

Go **1.26+** and PostgreSQL **16+**. The Docker/CI configurations use Go 1.26 and PostgreSQL 17. No Node/npm or frontend build is required.

First run setup and start the database:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1
docker compose up -d db
```

Load `.env` values into PowerShell and configure the local connection:

```powershell
Get-Content .env | ForEach-Object {
    if ($_ -match '^([A-Z_]+)=(.*)$') {
        [Environment]::SetEnvironmentVariable($matches[1], $matches[2], 'Process')
    }
}
$env:DATABASE_URL = "postgres://seatbook:$($env:DB_PASSWORD)@localhost:5434/seatbook?sslmode=disable"
$env:HTTP_ADDR = ":$($env:APP_PORT)"
$env:APP_ORIGIN = "http://localhost:$($env:APP_PORT)"
$env:COOKIE_SECURE = "false"
go mod download
go run ./cmd/api
```

Stop the Compose app first if it is already using the port: `docker compose stop app`. Go reads process environment variables; it does not automatically load `.env`. PostgreSQL tables and demo events initialize automatically. Event dates are seeded once and stay unchanged on later starts.

## Tests

Unit tests and static analysis:

```powershell
go test ./...
go vet ./...
```

All tests, including a real isolated PostgreSQL service and Go race detector:

```powershell
docker compose -f compose.test.yaml up --abort-on-container-exit --exit-code-from test
docker compose -f compose.test.yaml down
```

To run integration tests directly, set `TEST_DATABASE_URL` to a **test database**. Each run creates/drops its own uniquely named schema and needs schema-creation permission; existing schemas are never truncated. Without that variable the integration suite is explicitly skipped. On Windows, direct `-race` needs a compatible C compiler; the Docker test command supplies one.

## Configuration

| Variable | Meaning |
|---|---|
| `DATABASE_URL` | Required PostgreSQL URL (Compose supplies it) |
| `JWT_SECRET` | Required signing key, at least 32 bytes |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD` | Bootstrap admin; password 10–72 bytes |
| `HTTP_ADDR` | Go listen address; default `:8080` |
| `APP_ORIGIN` | Exact browser origin; default `http://localhost:8080`, no trailing slash |
| `COOKIE_SECURE` | `false` for local HTTP; `true` for HTTPS |
| `HOLD_SECONDS` | Hold duration, default 300; allowed 1–3600 |
| `APP_PORT` | Compose host port, default 8080 |
| `DB_PASSWORD` | Compose PostgreSQL password; use URL-safe letters/digits/underscore/hyphen |

Bootstrap creates the admin if the email does not exist. It **does not** change an existing user's role or password. Changing `ADMIN_PASSWORD` later does not reset an existing account. The browser helper text assumes the default 300-second hold; the actual countdown always uses the server expiry.

## Operations

```powershell
docker compose ps
docker compose logs -f app
docker compose down
```

`down` keeps the database volume. **`docker compose down -v` permanently deletes local accounts, events and reservations.** Changing `DB_PASSWORD` in `.env` does not change the password of an already initialized PostgreSQL volume.

Ports bind to loopback for local use. Before any internet deployment, configure TLS, set `COOKIE_SECURE=true`, use a strong unique organizer password and secrets, configure `APP_ORIGIN`, and add operational backups. The supplied Compose file is a local development setup.

See [docs/API.md](docs/API.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/LEARNING_RU.md](docs/LEARNING_RU.md).
