# HTTP API

All paths are same-origin. JSON responses use `Content-Type: application/json`. Errors have the shape `{"error":"message"}`. Send `Content-Type: application/json` for requests with JSON bodies. Login/register set an HttpOnly cookie; send that cookie on later authenticated calls. The frontend does this automatically.

## Routes

| Method | Path | Access | Result |
|---|---|---|---|
| GET | `/health` | Public | Liveness |
| GET | `/ready` | Public | PostgreSQL connectivity |
| POST | `/api/auth/register` | Public | Create user and sign in; 201 |
| POST | `/api/auth/login` | Public | Sign in; 200 |
| POST | `/api/auth/logout` | Public | Clear cookie; 200 |
| GET | `/api/me` | Signed in | Current user |
| GET | `/api/events` | Public | Next 100 upcoming events |
| GET | `/api/events/{id}` | Public | Event, seats, server time |
| POST | `/api/admin/events` | Admin | Create event and seats; 201 |
| POST | `/api/bookings` | Signed in | Reserve one seat; 201 or replay 200 |
| GET | `/api/bookings?before={id}` | Signed in | Up to 100 own bookings, newest first |
| POST | `/api/bookings/{id}/confirm` | Owner | Simulate checkout; 200 |
| POST | `/api/bookings/{id}/cancel` | Owner | Release a pending hold; 200 |

A booking belonging to another account returns **404**, so booking existence is not disclosed. Unauthenticated requests return 401; missing organizer role returns 403. Wrong state, unavailable seat or a reused key with different input returns 409. Unknown JSON properties, invalid fields or multiple JSON objects return 400. Wrong content type returns 415. Authentication attempts are limited to 20 per minute per direct client IP; the app does not trust `X-Forwarded-For`.

## Registration/login

```json
{"email":"visitor@example.com","password":"LongEnoughPassword!"}
```

Email is trimmed/lowercased; display names are not accepted. Password length is 10–72 **bytes**. Roles cannot be selected during registration. Cookie lifetime is 8 hours; password hashes and signing secrets are never returned in JSON.

## Create event

```json
{
  "title":"Go Community Night",
  "description":"An evening for backend developers.",
  "starts_at":"2027-06-01T14:00:00Z",
  "price_cents":250000,
  "rows":5,
  "columns":8
}
```

Time must be at least one minute in the future. Bounds: title 1–120 Unicode characters; description up to 2000; rows 1–12; columns 1–16; price 0–100000000 in minor units (250000 = 2500.00 KZT). Returned `id` identifies the event. Currency amounts are stored as integers.

## Reserve

Required header: `Idempotency-Key: visitor-request-0001`. Allowed key: 8–80 ASCII letters, digits, hyphens and underscores; unique within an account.

```json
{"seat_id":42}
```

The first successful request returns 201 and a booking. Same user/key/seat returns that booking with 200 and `Idempotency-Replayed: true`; a different seat with the same key returns 409. On network failure, retry with the **same** key. For a deliberately new reservation, generate a new key.

A booking includes `id`, `user_id`, `seat_id`, `status`, `expires_at`, `created_at`, event details, row, number and price. `expires_at` matters only for pending holds. A confirmation retry returns the same confirmed booking. A cancellation retry returns the same cancelled booking. Expired bookings and confirmed-ticket cancellations return 409.

## PowerShell example

Run while the app is available on port 8080. Use an unused email if registering repeatedly.

```powershell
$base = 'http://localhost:8080'
$body = @{ email='demo@example.com'; password='DemoPassword123!' } | ConvertTo-Json
Invoke-RestMethod "$base/api/auth/register" -Method Post -ContentType 'application/json' -Body $body -SessionVariable session
$events = Invoke-RestMethod "$base/api/events"
$detail = Invoke-RestMethod "$base/api/events/$($events[0].id)"
$seat = $detail.seats | Where-Object { $_.status -eq 'available' } | Select-Object -First 1
$key = [guid]::NewGuid().ToString()
$body = @{ seat_id=$seat.id } | ConvertTo-Json
$booking = Invoke-RestMethod "$base/api/bookings" -Method Post -ContentType 'application/json' -Body $body -Headers @{ 'Idempotency-Key'=$key } -WebSession $session
Invoke-RestMethod "$base/api/bookings/$($booking.id)/confirm" -Method Post -WebSession $session
```

The API rejects cross-origin mutations. Scripts without an `Origin` header are permitted; browser requests with an origin must match `APP_ORIGIN`. No CORS configuration is needed because the frontend is embedded in the same service.
