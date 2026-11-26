# Fayrly v1 Backend

Go microservices backend for a concert, event and charity platform.

- JWT authentication with rotating refresh tokens, plus optional Firebase ID-token login.
- Concert / charity / community event CRUD with a guarded publishing lifecycle.
- Report CRUD with a submit → approve/reject workflow and separation of duties.
- User administration for superadmins and admins.
- Role and permission CRUD with an editable access matrix.

## Architecture

Five services, one Go module. Each `cmd/*` binary is deployed as its own container
and owns its database. Only the gateway is exposed to clients.

```
                        ┌──────────────────────┐
   clients ───────────► │  gateway   :8080     │  CORS · rate limit · readiness
                        └──────────┬───────────┘
              ┌──────────────┬─────┴────────┬──────────────┐
              ▼              ▼              ▼              ▼
        ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
        │ auth     │   │ user     │   │ event    │   │ report   │
        │ :8081    │◄─►│ :8082    │   │ :8083    │   │ :8084    │
        └────┬─────┘   └────┬─────┘   └────┬─────┘   └────┬─────┘
             ▼              ▼              ▼              ▼
        fayrly_auth    fayrly_user    fayrly_event   fayrly_report
                        (PostgreSQL 16 + GORM, one database per service)
```

| Service | Port | Database | Responsibility |
| --- | --- | --- | --- |
| `gateway` | 8080 | – | Public entry point, CORS, per-IP rate limiting, aggregated `/readyz` |
| `auth` | 8081 | `fayrly_auth` | Credentials, JWT issuing, refresh-token families, Firebase login, bootstrap superadmin |
| `user` | 8082 | `fayrly_user` | User profiles, roles, permissions, access matrix, auth credential provisioning |
| `event` | 8083 | `fayrly_event` | Concert / charity / community events, ticket inventory and checkout |
| `report` | 8084 | `fayrly_report` | Reports, metrics, submit/review workflow |

Service-to-service calls use `X-Internal-Key` and are never routed by the gateway.
Each service embeds its own SQL migrations and applies them at startup when
`AUTO_MIGRATE=true`.

Data access goes through GORM (`gorm.io/gorm` on the pgx Postgres driver). The
schema stays in plain SQL migrations so partial indexes, check constraints and
RBAC seeds remain explicit; GORM repos map rows to domain structs and never run
`AutoMigrate`.

## Quick start (Docker)

```bash
cd backend-service
make setup        # creates .env from .env.example
make up           # builds five images and starts postgres + services
make smoke        # end-to-end checks against http://localhost:8080
```

The first superadmin is created by the auth service from these `.env` values:

```
BOOTSTRAP_ADMIN_EMAIL=admin@fayrly.local
BOOTSTRAP_ADMIN_PASSWORD=FayrlyAdmin123!
```

Change them before exposing anything. If the credential already exists, bootstrap
does nothing, so it is safe to restart the stack.

Useful commands:

```bash
make logs         # follow container logs
make down         # stop the stack
make reset        # stop and delete the database volume (destructive)
```

PostgreSQL is published on `localhost:5432` for local development.

## Local development without containers for the services

```bash
make setup
make db-up
make run-auth     # terminal 1
make run-user     # terminal 2
make run-event    # terminal 3
make run-report   # terminal 4
make run-gateway  # terminal 5
make smoke
```

`make run-*` sources `.env`, so local services use the `localhost` database URLs.

## Configuration

All configuration is environment based.

| Variable | Default | Used by |
| --- | --- | --- |
| `JWT_SECRET` | – (required, ≥32 chars) | auth, user, event, report |
| `JWT_ISSUER` / `JWT_AUDIENCE` | `fayrly` / `fayrly-api` | all token consumers |
| `ACCESS_TOKEN_TTL` | `15m` | auth, user, event, report |
| `REFRESH_TOKEN_TTL` | `720h` | auth |
| `INTERNAL_API_KEY` | – (required for auth/user) | auth ↔ user internal calls |
| `AUTO_MIGRATE` | `true` | all services with a database |
| `AUTH_DATABASE_URL` | – | auth |
| `USER_DATABASE_URL` | – | user |
| `EVENT_DATABASE_URL` | – | event |
| `REPORT_DATABASE_URL` | – | report |
| `AUTH_ADDR` / `USER_ADDR` / `EVENT_ADDR` / `REPORT_ADDR` | `:8081`–`:8084` | respective service |
| `GATEWAY_ADDR` | `:8080` | gateway |
| `AUTH_SERVICE_URL` | `http://localhost:8081` | gateway, user |
| `USER_SERVICE_URL` | `http://localhost:8082` | gateway, auth |
| `EVENT_SERVICE_URL` | `http://localhost:8083` | gateway |
| `REPORT_SERVICE_URL` | `http://localhost:8084` | gateway |
| `CORS_ALLOWED_ORIGINS` | empty (same-origin only) | gateway |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `20` / `40` (`50`/`100` in compose) | gateway |
| `FIREBASE_PROJECT_ID` | empty (Firebase login disabled) | auth |
| `MIDTRANS_SERVER_KEY` / `MIDTRANS_SIGNATURE_SECRET` | empty (checkout disabled) | event |
| `MIDTRANS_ENVIRONMENT` | `sandbox` (`production` for live payments) | event |
| `MAIL_MODE` | `auto` (`log` prints emails to the service log) | auth |
| `RESEND_API_KEY` / `MAIL_FROM` | empty | auth (password reset email) |
| `BOOTSTRAP_ADMIN_EMAIL` / `_PASSWORD` / `_NAME` | empty | auth |

Generate production secrets with `openssl rand -base64 48`.

## API overview

All routes are served by the gateway under `http://localhost:8080`.

### Auth (`/api/v1/auth`)

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| POST | `/register` | – | Create credential + member profile, returns a session |
| POST | `/login` | – | Email/password login |
| POST | `/firebase` | – | Exchange a Firebase ID token |
| POST | `/refresh` | – | Rotate a refresh token |
| POST | `/logout` | – | Revoke the refresh-token family |
| GET | `/me` | Bearer | Current profile, roles and permissions |
| POST | `/password` | Bearer | Change password and revoke all sessions |
| POST | `/password/forgot` | – | Email a 6-digit reset code (always `202` for unknown emails) |
| POST | `/password/verify` | – | Check a reset code without consuming it |
| POST | `/password/reset` | – | Consume a reset code and set a new password |

Session response:

```json
{
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": 900,
  "refresh_token": "…",
  "refresh_expires_in": 2592000,
  "user": {"id": "...", "email": "...", "name": "...", "status": "active", "roles": ["member"], "permissions": []}
}
```

### Users, roles and access matrix

| Method | Path | Permission |
| --- | --- | --- |
| GET / POST | `/api/v1/users` | `user:read` / `user:write` |
| GET / PATCH / DELETE | `/api/v1/users/{id}` | `user:read` / `user:write` / `user:delete` |
| PUT | `/api/v1/users/{id}/roles` | `user:write` |
| POST | `/api/v1/users/{id}/reset-password` | `user:write` |
| GET / POST | `/api/v1/roles` | `role:read` / `role:write` |
| GET / PATCH / DELETE | `/api/v1/roles/{id}` | `role:read` / `role:write` |
| PUT | `/api/v1/roles/{id}/permissions` | `role:write` |
| GET | `/api/v1/permissions` | `role:read` |
| GET / PUT | `/api/v1/access-matrix` | `role:read` / `role:write` |

Creating a user without a password returns a generated `temporary_password` plus a
human-readable notice (returned once, never stored in plain text). Assigning the
`superadmin` role requires a superadmin actor. System roles cannot be deleted, the
superadmin permission set is immutable, and the last active superadmin cannot be
removed, demoted, suspended or deleted.

### Events (`/api/v1/events`)

| Method | Path | Auth |
| --- | --- | --- |
| GET | `/` | public (published/ongoing/completed only) |
| GET | `/{id}` | public; drafts return 404 unless the caller has `event:read` |
| GET | `/mine` | Bearer |
| POST | `/` | `event:write` |
| PATCH | `/{id}` | `event:write` |
| POST | `/{id}/status` | `event:publish` |
| DELETE | `/{id}` | `event:delete` |

Lifecycle: `draft → published → ongoing → completed`, with `cancelled` reachable
from draft/published/ongoing. Completed and cancelled events are read-only.
Money fields (`price_min`, `price_max`, `donation_target`, `donation_raised`) are
integers in the smallest currency unit, avoiding floating-point drift.

List filters: `q`, `type`, `city`, `status` (comma-separated), `from`, `to`,
`page`, `page_size`. User lists accept `q`, `status`, `role`; report lists accept
`q`, `type`, `status`, `event_id`, `mine=true`.

### Event tickets and checkout

Ticket types hold the authoritative unit price and inventory; event `price_min` and
`price_max` remain display fields. Create ticket types while an event is a draft:

```http
POST /api/v1/events/{event_id}/ticket-types
Authorization: Bearer <admin-token>
Content-Type: application/json

{"name":"General Admission","price":150000,"capacity":300}
```

Ticket prices and payment totals are integer IDR. Checkout accepts a ticket type
and quantity, never a client-supplied amount. Send a unique `Idempotency-Key` for
each checkout attempt:

```http
POST /api/v1/events/{event_id}/orders
Authorization: Bearer <access-token>
Idempotency-Key: <unique-key>
Content-Type: application/json

{"ticket_type_id":"<ticket-type-uuid>","quantity":2}
```

The response includes a `redirect_url` for the Midtrans-hosted payment page.
Checkout offers GoPay, bank transfer virtual accounts, and QRIS. Midtrans sends
status updates to `POST /api/v1/events/payment-notifications`; configure that
public HTTPS URL as the Payment Notification URL in the Midtrans dashboard.
The webhook signature is checked with `MIDTRANS_SIGNATURE_SECRET` before order
status or reserved inventory is changed. Keep both Midtrans secrets only in the
event service environment. Configure both together; checkout is unavailable if
they are absent.

For local testing, set `MIDTRANS_SERVER_KEY` and `MIDTRANS_SIGNATURE_SECRET` to
Sandbox credentials and leave `MIDTRANS_ENVIRONMENT=sandbox`. Use
`MIDTRANS_ENVIRONMENT=production` only with production credentials and a live
notification URL.

### Reports (`/api/v1/reports`)

| Method | Path | Permission |
| --- | --- | --- |
| GET | `/` | `report:read` |
| GET | `/{id}` | `report:read` |
| POST | `/` | `report:write` |
| PATCH | `/{id}` | `report:write` |
| DELETE | `/{id}` | `report:write` (author, draft/rejected only, or `report:delete`) |
| POST | `/{id}/submit` | `report:write` (author only) |
| POST | `/{id}/review` | `report:approve` |

Workflow: `draft → submitted → approved | rejected`. Rejected reports return to
the author for edits and can be resubmitted. Authors cannot review their own
reports, and rejection requires notes. Reports in `submitted` or `approved` state
cannot be deleted.

### Errors and pagination

```json
{"error": {"code": "validation_failed", "message": "the request contains invalid fields", "details": {"email": "must be a valid email"}, "request_id": "…"}}
```

Lists return:

```json
{"data": [], "pagination": {"page": 1, "page_size": 20, "total": 0, "total_pages": 0}}
```

`X-Request-ID` is accepted from callers (max 128 chars) or generated, echoed on
the response and included in every log line.

## Roles

| Role | Capabilities |
| --- | --- |
| `superadmin` | Every permission; manages admins, roles and the access matrix |
| `admin` | Users, events and reports; cannot change roles |
| `manager` | Full event lifecycle plus report authoring |
| `staff` | Read events, read/write reports |
| `member` | No back-office access |

Permissions are embedded in the access token. Changes to a role or the access
matrix take effect for a user on their next refresh or login (maximum 15 minutes
with default TTLs). This keeps every service stateless on the request path.

## Security notes

- Passwords use bcrypt (cost 12) with a ≥8 character policy requiring a letter and
  a digit. Unknown-email logins still run a dummy hash to reduce timing leakage.
- Refresh tokens are opaque, stored only as SHA-256 hashes, rotated on every use,
  and grouped into a `family_id`. Reusing a rotated token revokes the whole family.
- Password changes, admin resets and account deletion revoke all refresh tokens.
  Existing access tokens remain valid until expiry; keep the access TTL short.
- Firebase ID tokens are verified against Google's public x509 certificates
  (cached according to `Cache-Control`) and require a verified email. No service
  account file is needed.
- The gateway trusts the socket address for rate limiting, because
  `X-Forwarded-For` can be spoofed by clients. Put a trusted load balancer in
  front and adapt the limiter if you need real client IPs behind a proxy.
- Rate limiting is in-memory and per instance. Use a shared store (Redis) when
  running several gateway replicas.
- TLS is expected to terminate at the edge (load balancer or reverse proxy).
- `/internal/*` routes are not routed by the gateway and require `X-Internal-Key`.

## Project layout

```
cmd/                      one main package per service
internal/platform/        shared config, logging, HTTP, authn, database, errors
internal/gateway/         edge routing, CORS, rate limit, readiness aggregation
internal/auth/            credentials, sessions, Firebase verifier, migrations
internal/user/            users, roles, permissions, access matrix, migrations
internal/event/           events CRUD and lifecycle, migrations
internal/report/          reports and review workflow, migrations
deploy/initdb/            creates the per-service databases on first boot
scripts/smoke.sh          end-to-end API test
```

## Testing

```bash
make test
make vet
make build
make smoke    # requires the stack to be running
```

`make smoke` covers login, RBAC denials, role/access-matrix changes, user
provisioning, the event lifecycle, the report approve/reject flow, CORS, refresh
rotation and reuse detection, logout, and account deletion.
