# Dira Core API

Platform foundation for the Dira services: **identity, wallets, payments, notifications,
vehicles and ratings**, plus the shared Go packages every service builds on.

`dira-food-api` (food delivery) and `dira-vtc-api` (ride-hailing) are **verticals** on top
of it. See the development plan in
[dira-vtc-api/docs/PLAN.md](https://github.com/kgtech-org/dira-vtc-api/blob/main/docs/PLAN.md).

## Two things in one repo, deliberately

| Path | What | Importable from other services |
|---|---|---|
| `pkg/` | the **shared library** — errors, JWT, HTTP helpers, Mongo, indexes, FCM, storage, media, i18n, jobs, docs, audit, maps client, chat | **yes** |
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
pkg/chat        the client ↔ driver conversation — BEHAVIOUR, not storage
```

> ⚠️ **`pkg/chat` is a LIBRARY, not a service, and that is deliberate.**
>
> Both verticals need the same **behaviour** — who may speak, until when, how a
> message is marked read — but not the same **data**: an order conversation and a
> ride conversation are never read together. Making it a service would have cost
> a **third socket** in the apps (they already carry tracking + orders), and a
> conversation-state synchronisation between services whose failure would read as
> "no driver assigned" with nobody able to say why.
>
> Each vertical wires its own collection, its own parties and its own real-time
> channel. What is shared is the **rule**.
>
> This is the test for anything else: does the core hold it because both need the
> same *data* (a balance, an account), or merely the same *behaviour*? The second
> belongs in `pkg/`.

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
| `POST /internal/wallets/{create,consume,credit,pay,refund,credit-earnings}` | move money |
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

## The frontend contracts live here

`docs/frontend/` holds the **five role specs** the mobile teams read — and they
live in the core, not in a vertical, because they describe **three services**.

| Document | Vertical | Role |
|---|---|---|
| `FOOD-CLIENT.md` | delivery | client |
| `FOOD-MERCHANT.md` | delivery | merchant |
| `FOOD-DELIVERY.md` | delivery | courier |
| `VTC-CLIENT.md` | rides | client |
| `VTC-DRIVER.md` | rides | driver |

> ⚠️ **The file name carries the VERTICAL, deliberately.** "Courier" and
> "driver" both translate to *driver*: a `DRIVER.md` sitting next to a ride
> spec let two teams wire the wrong base URL — `/api/v1/food` instead of
> `/api/v1/vtc` — with nothing in the documents to contradict them. The error
> would only have shown up as a 404, to people with no access to this code.

Every spec spans core **and** a vertical: signing in and paying are core, the
rest is not. Keeping them in `dira-food-api` meant the ride contracts lived in
a repository that does not serve a single ride route.

> ⚠️ **`docs/frontend` is also a Go PACKAGE**, and that is the point. No single
> repository can verify these documents alone — each knows only its own routes.
> Being importable lets every vertical check **its own half** from its own
> repository, with no copy and no network call.
>
> Without it, moving these documents here would have dropped the verification
> from **133** cited routes to **39** — a guarantee quietly emptied, which is
> worse than one that never existed: people go on believing it protects them.
>
> `docs/frontend/specs_test.go` covers the core's share (39 citations), plus
> two guarantees that belong nowhere else: **all five specs are present**, and
> they **share one version** — "a frontend quoting v2.1.0 names a precise
> contract" only holds if nobody can forget to bump one.

## Status

- **Step A — done.** The shared library is extracted; `dira-food-api` builds on it.
- **Step B — done.** The service serves **identity**, **wallets**, **payments**,
  **notifications** and **rating storage**.
- **Step C — done.** `dira-food-api` no longer carries those modules: it reaches them through
  `internal/corebridge`, and its own copies are deleted.

> **Money is tied to a REFERENCE, not to an order.** A ledger entry carries
> `ref_kind` (`order` \| `ride`) and `ref_id`. The field used to be `order_id`,
> from the time a single vertical existed — a name that designates one of two
> would have forced rides to write themselves down as "orders", that is, to lie
> about what the money paid for. The **pair** is the identity: two verticals can
> carry the same id without colliding, and a financial report splits by
> `ref_kind` instead of guessing.

> **`OnRefPaid` now calls the vertical back** — `internal/callback`, the one place where the
> core talks *to* a vertical rather than being asked. It is deliberately thin and one-way:
> the core states a fact, once, and asks nothing back. A dependency where each side queries
> the other would deadlock at startup and stop the core from being deployable alone.
>
> ⚠️ The error **propagates to the webhook**. Acknowledging a provider without having told
> delivery would leave an order paid and never confirmed — and the provider, having received
> an acknowledgement, would not retry. A failure here makes it retry, which is what we want.
> With `FOOD_BASE_URL` unset, the service logs an ERROR at startup and every order payment
> webhook fails loudly.

> **Money movements are idempotent** — the concern that stood here is closed. `PayOrder`,
> `RefundOrder`, `CreditEarnings` and an order-bound `Consume` reserve their operation inside
> the transaction that applies it, and a replay returns success without moving anything twice.
>
> The key is **derived from the operation, not supplied by the caller**: an order is paid
> once, so its id *is* the key. Asking a vertical for a key would have put the guarantee in
> the hands of whoever has the most reasons to forget it — and a forgotten key is invisible
> until a client is charged twice.
>
> ⚠️ A movement with **no natural key** — a commercial gesture, a free-form credit — is
> deliberately **not** guarded: two identical credits may be two intended gestures.

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

## Running it

```sh
cp .env.example .env   # adjust values
go run ./cmd/api       # HTTP API on :8082 (default)
```

```sh
SEED_ADMIN_PASSWORD=<choose-one> go run ./cmd/seed   # the admin, and nothing else
```

`docker/Dockerfile` builds the production image — **two binaries**, `api` and
`seed`. No ffmpeg (it processes no video), and **no separate worker**: the one
deferred task the core owns runs *inside* `api`.

> ⚠️ **One queued task, and one only** — `core:ref_paid`, which tells a vertical
> that a payment went through.
>
> The other **twenty-three** cross-service calls are *commands awaiting an
> answer* — "debit this wallet, did it work?". A queue buys them nothing: you
> would rebuild request/reply on top of it, which is HTTP done worse. This one
> is the only **event**: the core states a fact and expects nothing back.
>
> It was inlined, and that coupled the provider's webhook to the vertical's
> availability. During a delivery redeploy a customer's payment *failed*, and it
> fell to the provider to retry — we made the buyer carry the latency of our
> releases.
>
> ⚠️ **A failed enqueue still fails the webhook**, and must: the payment is
> already marked succeeded, so answering "received" without queueing the
> callback would leave an order paid and never confirmed. The provider retries —
> and the **duplicate path re-enqueues**, without which that retry would find a
> "duplicate" and notify nobody. The vertical's callback is idempotent, so one
> callback too many costs nothing and one missing costs an order.
>
> The consumer runs in the API process rather than its own container: one more
> container on a single host, for a task that makes an HTTP call, costs more to
> operate than it returns. Extracting it later is mechanical — the handler is
> already a standalone type.

> ⚠️ **`seed` provisions the ADMIN, and nothing else.** The core does not know
> what a restaurant or a trip is; each vertical seeds its own demo set and asks
> the core to open the accounts it needs through
> `/internal/accounts/ensure`. This binary exists for one real case: **a core
> deployed alone has nobody to sign in with**. Without it you would have to seed
> an entire vertical to obtain an administrator, or write into the database by
> hand.
>
> It writes **directly** into the core's database, and that is the only place
> where doing so is legitimate — it is its own. A vertical goes through the
> service surface: two writers on one table means the second always ignores
> something the first guarantees.
>
> ⚠️ `--reset` **drops every account of the platform** — users, sessions,
> addresses. Orders and rides in the verticals would survive and point at
> accounts that no longer exist. Refused outright in production.

| Route | Content |
|---|---|
| `GET /docs` | Swagger UI |
| `GET /openapi.yaml` | OpenAPI 3.1 contract, embedded in the binary |

Behind the gateway the core keeps the **root** — `api-staging.dira.llc/api/v1/...` — while
the verticals take a prefix. Its `/internal/...` routes are **404 at the edge**:
their callers are on the internal Docker network, and a shared secret is a thing
that can leak.

The service **refuses to start without MongoDB** — a ping at boot, and a fatal
error otherwise. Starting half-alive would answer requests with an empty
directory.
