# Verification report

Verified on 2026-10-20 using Linux, Go 1.26.10 and PostgreSQL 16.15 with a UTF-8 database. The project's Docker and CI recipes target PostgreSQL 17 and Go 1.26.

## Completed checks

- `go test -race -count=1 -v ./...` with `TEST_DATABASE_URL` set: all unit and PostgreSQL integration tests passed; no Go data races reported.
- `go vet ./...`: passed.
- `go build ./cmd/api`: passed (the verification workspace required `-buildvcs=false` because of its surrounding checkout metadata).
- `node --check web/app.js`: passed.
- Docker Compose v2.40.3: both normal and test configurations passed `config --quiet` validation.
- Browser smoke test against the actual Go service: event catalog, registration, seat selection, reservation, confirmation, cancellation, organizer login and event creation passed.
- Mobile viewport 390 × 844: catalog and seat page fit the page width; wider seating layouts scroll within the seat panel.
- No JavaScript exceptions or CSP violations were observed in the tested browser flows. Desktop/mobile screenshots were inspected for layout.

## Database scenarios

- Twenty concurrent requests for one seat: one 201 response, nineteen 409 conflicts.
- Twenty concurrent requests with the same user/key/seat: one created booking; all successful replays returned its ID.
- Same key with a different seat: 409.
- Access to another user's reservation: 404.
- Expired reservation confirmation: rejected; seat can be reserved again.
- Concurrent sweeper, expired confirmation and replacement reservation: consistent final state, one live booking.
- Direct insertion of a second live booking: rejected by the database unique index.
- Repeated startup migrations and bootstrap: passed.

The detailed Go test output is retained in `TEST_RESULTS.txt`.

## Boundaries of this verification

The Docker engine and a Windows machine were not available here. Container image build/start and the PowerShell helpers were therefore not executed end-to-end; the application was built/run directly, Compose configurations were validated, and the helpers were reviewed. Browser checks used headless Chromium 131. Testing confirms the listed scenarios, not the absence of every possible bug or suitability for real payment processing.
