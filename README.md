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

## The service

```
cmd/api          HTTP entrypoint
internal/user    accounts, auth, RBAC, addresses, preferences
internal/token   token wallets and ledger
internal/payment mobile money (provider abstraction + mock)
internal/notify  inbox, push devices, multilingual templates (FCM)
internal/config  core-only settings (the shared ones come from pkg/config)
internal/indexes the MongoDB indexes this service owns
api/openapi.yaml the contract, embedded in the binary
```

⚠️ **The JWT secret is the SAME as the verticals'.** That is what lets each of them verify
a token **locally**, without calling core — a per-request verification would make this
service the single point of failure of the whole platform.

## Status

- **Step A — done.** The shared library is extracted; `dira-food-api` builds on it.
- **Step B — in progress.** The service boots and serves **identity**, **wallets**,
  **payments** and **notifications**. Ratings and vehicles are still in `dira-food-api`.

> ⚠️ **`OnOrderPaid` is nil, and it is the first real service-to-service gap.** Confirming
> the payment of an *order* means telling the **vertical**, which alone knows what an order
> is. The module logs it loudly rather than losing it; the hook becomes an HTTP callback in
> step C. Nothing is broken while `dira-food-api` still takes its own payments — but routing
> payments here before that callback exists would leave orders paid and never confirmed.

> ⚠️ **`internal/rating` was moved here and moved back.** It has no import coupling, but it
> needs the order, delivery, brand and dish services to know *what* is being rated. Its
> rollup mechanism may become shared later; the module as it stands is the vertical's.

> ⚠️ **`internal/token` is not settled.** Its ledger belongs here, but `BoostDish` and the
> store-option catalogue need the food **catalogue** and **brand ownership**. Those
> collaborators are wired `nil` and their routes are not mounted — see the comment in
> `cmd/api/main.go`. Splitting the module (ledger here, boosting in food) is the next
> decision, and leaving it silent would suggest the module had found its place.
