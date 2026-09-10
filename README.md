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

## The service surface

Everything a vertical is allowed to ask of core lives under `/api/v1/internal/...`, guarded
by a **shared secret** — not a user's token. These routes carry a *service's* powers: debit a
wallet, open an account, notify someone in their name. No person ever calls them.

| Route | What it grants |
|---|---|
| `POST /internal/accounts/{contact,names,by-phone}` | read a name, a phone, an id — nothing more |
| `POST /internal/accounts/{ensure,ensure-merchant}` | open an account; `ensure` takes any role, so it can create an admin |
| `POST /internal/wallets/{create,consume,credit,pay-order,refund-order,credit-earnings}` | move money |
| `POST /internal/notifications/send` | send one templated message |
| `POST /internal/payments/initiate` | start a payment on a client's behalf (WhatsApp) |
| `POST /internal/backoffice/{wallets,token-transactions,payments}` | read the money, **raw** — ids, not names |
| `POST /internal/backoffice/refund-order-payment` | mark an order's payment refunded — the vertical judges the dispute |
| `POST /internal/ratings` | deposit scores a vertical has already validated |

The back-office listings return **identifiers, not names**: core does not know what a point of
sale or a food order is. The vertical owns those objects and names them in its own admin view.
That is why `/admin/wallets` still lives in `dira-food-api` — the rows come from here, the
store names are added there.

> The list is **verified**, not merely written down: `internal/serviceapi/surface_test.go`
> scans the sources and fails on any `/internal/...` route that is not declared. A surface you
> can read in one place is a surface you can audit — but only if nothing can be added quietly.

## Status

- **Step A — done.** The shared library is extracted; `dira-food-api` builds on it.
- **Step B — done.** The service serves **identity**, **wallets**, **payments**,
  **notifications** and **rating storage**.
- **Step C — done.** `dira-food-api` no longer carries those modules: it reaches them through
  `internal/corebridge`, and its own copies are deleted.

> ⚠️ **`OnOrderPaid` is nil, and it is the last real service-to-service gap.** Confirming the
> payment of an *order* means telling the **vertical**, which alone knows what an order is.
> The module logs it loudly rather than losing it. Nothing breaks while food takes its own
> payments — but routing a mobile-money order payment here before that callback exists would
> leave the order paid and never confirmed.

> ⚠️ **`PayOrder` has no idempotency key.** It is now an HTTP call: a lost *response* — the
> debit applied, the answer never delivered — leaves the client charged and the order not
> created. A retry debits twice. The key belongs on the request, and the wallet must refuse
> to apply the same one twice.

> ⚠️ **`internal/rating` is SPLIT on purpose.** Storage, uniqueness and averages are here,
> because the courses will rate their drivers with the same collection. What stays in the
> vertical is the **judgement**: knowing an order was delivered, who carried it, what it
> contained. Core stores what it is handed; it cannot tell a legitimate score from a made-up
> one.

> ⚠️ **`internal/token` is not settled.** Its ledger belongs here, but `BoostDish` and the
> store-option catalogue need the food **catalogue** and **brand ownership**. Those
> collaborators are wired `nil` and their routes are not mounted — see the comment in
> `cmd/api/main.go`. Splitting the module (ledger here, boosting in food) is the next
> decision, and leaving it silent would suggest the module had found its place.
