# Dira Core API

Platform foundation for the Dira services: **identity, wallets, payments, notifications,
vehicles and ratings**, plus the shared Go packages every service builds on.

`dira-food-api` (food delivery) and `dira-vtc-api` (ride-hailing) are **verticals** on top
of it. See the development plan in
[dira-vtc-api/docs/PLAN.md](https://github.com/kgtech-org/dira-vtc-api/blob/main/docs/PLAN.md).

## Two things in one repo, deliberately

| Path | What | Importable from other services |
|---|---|---|
| `pkg/` | the **shared library** — errors, JWT, HTTP helpers, Mongo, indexes, FCM, storage, media, i18n, jobs, docs, audit, maps client | **yes** |
| `internal/` | the **core service** itself — identity, wallets, payments, notifications | **no** — Go forbids it |

That split is not a convention: Go's `internal/` rule *enforces* it. A vertical can depend
on the library without ever reaching into the service, and no third repository is needed to
keep the two apart.

## Shared packages

```
pkg/apperr      typed business errors → HTTP status
pkg/auth        JWT minting and verification, roles
pkg/httpx       decoding, validation, JSON responses, cursor pagination
pkg/middleware  auth, roles, rate limiting, request id
pkg/db          MongoDB client, transactions, index applier
pkg/config      environment readers + the settings every service has
pkg/fcm         Firebase push (HTTP v1, self-signed service-account JWT)
pkg/storage     object storage (MinIO / S3)
pkg/media       video probing and transcoding
pkg/i18n        French/English message catalogue
pkg/jobs        Asynq client
pkg/docs        Swagger UI + OpenAPI serving (the contract is passed in)
pkg/audit       audit-log recorder
pkg/diramaps    dira-maps client (route duration, landmarks)
```

⚠️ **What `pkg/` must never hold**: business settings. A delivery fee grid, a WhatsApp
token or a ride commission belongs to its service. Gathering them here would turn a shared
struct into the dumping ground of three products, and each service would carry variables it
never reads — so variables nobody knows whether to set.

## Status

**Step A of the plan is done**: the shared library is extracted and `dira-food-api` builds
on it. The core *service* (step B) has not started.
