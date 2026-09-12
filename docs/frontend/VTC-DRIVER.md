# App CHAUFFEUR — COURSES (VTC) — contrat d'API

> **Version 3.2.0** · 12 septembre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Courses : `https://api-staging.dira.llc/api/v1/vtc` · Suivi : `wss://tracking-staging.dira.llc` · SIG : `https://maps.dira.llc/api`

---

## 1. Quatre back-ends, deux sockets

| | Rôle |
|---|---|
| **Socle** | connexion, profil, notifications, avis reçus — base `…/api/v1/` |
| **Courses** | profil chauffeur, véhicules, courses, conversation, grand livre — base `…/api/v1/vtc/` |
| **Suivi** (`dira-tracking`) | émission des positions, **réception des appels de course** |
| **SIG** (`dira-maps`) | itinéraire routier — **du JSON, aucune vue de carte** |

> ⚠️ **Deux sockets, pas un.** Celui du suivi émet des positions en continu ;
> celui des courses reçoit les messages du passager. Autres hôtes, autres
> cycles de vie. **Ne les factorisez pas.**

> **Le jeton est le même partout.** Une seule connexion, au socle.

Conventions communes : voir [`FOOD-CLIENT.md` §1](FOOD-CLIENT.md).

---

## 2. Le profil chauffeur

```
GET   /drivers/me                   → crée le profil au premier appel
GET   /drivers/me/stats?date=YYYY-MM-DD&tz=Africa/Lome   → { rides, driver_xof, online_s }   (v3.1.0)
PATCH /drivers/me/online            { "online": true | false }
GET   /drivers/me/vehicles
POST  /drivers/me/vehicles          { class_key, brand, model, license_plate, color, seats, photo_url }
                                    photo_url : téléverser d'abord — POST /api/v1/uploads?kind=vehicle (SOCLE, v3.0.0)
PATCH /drivers/me/active-vehicle    { "vehicle_id": "…" }
```

```json
{ "id": "…", "user_id": "…", "active_vehicle_id": "…", "zone": "Lomé Centre",
  "online": true, "status": "active",
  "last_seen_at": "2026-09-12T10:41:23Z", "tracking_stale": false,
  "offline_at": null, "offline_reason": "",
  "rating_avg": 4.7, "rating_count": 132, "rides_count": 418 }
```

> **La présence — v3.1.0.** `last_seen_at` est la dernière position que le
> suivi a vue de votre véhicule (lue à l'instant) ; **`tracking_stale`** est
> vrai après **90 s** sans position, ou jamais vu — c'est « suivi arrêté » :
> vous êtes encore en ligne, mais **vous n'êtes plus appelé**. Affichez-le
> comme le widget. ⚠️ Après **5 min** sans position, **le serveur vous met hors
> ligne** : `online: false`, `offline_reason: "stale"`, `offline_at`. Dites-le
> (« Vous avez été mis hors ligne : position non reçue depuis 5 min »), au lieu
> d'afficher un état incohérent. `offline_reason` vaut `driver` quand c'est
> vous qui vous êtes retiré, `admin` sinon.

> **La journée — `GET /drivers/me/stats`.** `rides` et `driver_xof` comptent
> les courses **terminées** du jour (`date`, défaut aujourd'hui) dans **votre
> fuseau** (`tz`, défaut `Africa/Lome`) ; `online_s` est le temps en ligne du
> jour, période en cours comprise. Remplace le calcul depuis
> `GET /rides?limit=50` et le compteur local du téléphone.

> ⚠️ **`online` et `status` sont DEUX AXES, et ils doivent le rester.**
> `status` est ce que l'administration a décidé (`pending`, `active`,
> `suspended`) ; `online` est ce que le chauffeur choisit maintenant. Les
> confondre laisserait un chauffeur suspendu lever sa propre suspension en se
> remettant en ligne.

> ⚠️ **`rating_avg` ne s'affiche JAMAIS sans `rating_count`.** 5,0 sur un avis
> et 4,6 sur deux cents ne disent pas la même chose.

**Se mettre en ligne est refusé** si :

| Refus | Pourquoi |
|---|---|
| `409` compte non `active` | l'administration ne l'a pas encore validé |
| `409` aucun véhicule actif | le passager doit savoir ce qui vient le chercher |
| `402 debt_limit_reached` | la dette a franchi le plafond — voir §6 |

⚠️ **La classe du véhicule décide de ce que vous pouvez prendre.** Une citadine
ne sert pas une course « van » : l'accepter mettrait six personnes dans quatre
places.

---

## 3. L'APPEL — la course vient au chauffeur

**Il n'y a pas de liste de courses disponibles.** Le serveur choisit qui
appeler, par vagues, et le chauffeur accepte ou refuse.

L'appel arrive **par le socket du SUIVI**, celui-là même sur lequel vous
poussez vos positions :

```
WS wss://tracking-staging.dira.llc/track/agent
```

```json
{ "type": "call", "call_id": "…", "ref": "<ride_id>", "attempt": 1,
  "expires_at": 1789044151142,
  "pickup": { "lng": 1.2255, "lat": 6.1319 },
  "dropoff": { "lng": 1.2545, "lat": 6.1656 },
  "distance_m": 15.8,
  "meta": { "fare_xof": 2250, "class": "eco", "stops": 2, "distance_m": 6141 } }
```

Puis, à la fermeture : `{ "type": "call_closed", "call_id": "…", "reason": "…" }`.

> **Le même appel arrive AUSSI par FCM — v3.1.0.** Un socket meurt (jeton
> périmé, réseau, service tué, veille profonde) et l'appel n'arrive pas. Le
> suivi envoie donc, pour chaque `call` et `call_closed`, un message FCM
> **data-only, priorité haute**, aux appareils déclarés par `POST /me/devices`
> (socle) : `{ "type": "call" | "call_closed", "call_id", "ref", "expires_at",
> "attempt", "reason" }`. Pas de `notification` : c'est l'application qui sonne
> et ouvre son écran plein. À réception : reconnecter le socket si besoin, puis
> traiter comme une trame socket. **Idempotence par `call_id`** — reçue deux fois
> (socket + push), une trame ne sonne qu'une fois. Le message a un TTL FCM égal
> au temps restant de l'appel : un appel fermé n'est jamais livré en retard.
> ⚠️ Déclarez le jeton FCM à la connexion **et à chaque rotation**.

**Répondre** — sur le service de SUIVI, avec votre jeton :

```
POST https://tracking-staging.dira.llc/track/calls/{call_id}/accept   { "vehicle_id": "…" }
POST https://tracking-staging.dira.llc/track/calls/{call_id}/decline  { "vehicle_id": "…" }
```

> ⚠️ **Position émise DÈS L'ENTRÉE dans le parcours.** Sans elle, aucun appel
> n'arrive : c'est la position qui vous met dans le vivier.
>
> ⚠️ **Le compte à rebours se rend depuis `expires_at`**, jamais depuis une
> horloge locale — un téléphone déréglé afficherait un délai qui ment.
>
> ⚠️ **Refuser explicitement** plutôt que laisser expirer : la vague suivante
> part plus tôt, et un chauffeur qui n'a pas répondu n'est pas rappelé.
>
> ⚠️ **`meta.fare_xof` s'affiche AVANT d'accepter.** C'est ce qui permet de
> décider ; l'accepter à l'aveugle puis découvrir le prix serait une mauvaise
> surprise à chaque course.

> ⚠️ **Refuser un APPEL et abandonner une COURSE sont deux gestes différents,
> sur deux services différents.** `…/track/calls/{call_id}/decline` décline une
> offre que vous n'avez pas prise — la vague continue sans vous, et il ne se
> passe rien d'autre. `POST /rides/{id}/decline` vous retire d'une course que
> vous teniez **déjà** : le passager attendait, et cela compte comme une
> annulation. Les confondre ferait passer un simple refus pour un abandon.

> **Prise DIRECTE** — `POST /rides/{id}/accept` `{ "vehicle_id": "…" }` existe
> aussi, sur la base des courses. C'est le même geste que la réponse à l'appel,
> sans passer par le suivi. Utilisez la réponse à l'appel dans le parcours
> normal ; cette route sert les cas où l'identifiant de course est connu
> autrement (reprise après coupure, écran d'assistance).

**Refus possibles à l'acceptation** — l'appel reste ouvert pour les autres :

| Code | Sens |
|---|---|
| `403 not_called` | vous n'étiez pas dans cette vague |
| `409 call_expired` | l'offre appartient déjà à la vague suivante |
| `402 debt_limit_reached` | votre dette dépasse le plafond |
| `409 ride_taken` | un autre a été plus rapide |

---

## 4. Conduire

```
PATCH /rides/{id}/status                   { "status": "approach" | "onboard" | "completed" }
POST  /rides/{id}/stops/{index}/reached
POST  /rides/{id}/decline                  { "reason": "…" }
GET   /rides/{id}
GET   /rides?cursor=…                      # l'historique de VOS courses
```

```
accepted → approach → onboard → completed
        ↘ cancelled
```

> ⚠️ **Une course `onboard` ne s'annule plus.** Le passager est dans la
> voiture ; l'interrompre demanderait de décider où on le dépose. Le bouton
> doit disparaître à ce statut, pas échouer.

`stop_index` avance à chaque arrêt atteint. Les arrêts sont **dans l'ordre
choisi par le passager** : rien n'est réordonné, contrairement aux collectes
d'une livraison.

**Ce que vous voyez et que le passager ne voit pas** : `commission_xof` et
`driver_xof`. C'est votre part, et elle n'est servie qu'à vous.

### Émettre sa position PENDANT la course — `mission_id` (v3.2.0)

Les positions poussées sur le socket du suivi portent **`mission_id`** dès
que la course est acceptée, et jusqu'à `completed` :

```json
{ "vehicle_id": "…", "mission_id": "<ride_id>", "lng": 1.2255, "lat": 6.1319,
  "heading": 40, "speed": 12, "ts": 1789044151142 }
```

C'est ce champ qui fait le **parcours** : à `completed`, le serveur fige les
positions portées par la course, les recale sur la route, et la course garde
**`traveled_polyline`** (polyline Google), **`actual_distance_m`** et
**`distance_source`** (`tracked`). Sans `mission_id`, la course se termine
avec `distance_source: "planned"` — la distance du devis, aucun tracé — et
personne ne peut la rejouer. **Le prix ne change pas** : il vient du devis,
le parcours est une trace, pas une facture.

> Une course terminée sans tracé n'est pas une erreur du serveur : c'est un
> `mission_id` absent ou une position jamais poussée. Vérifier le socket avant
> d'ouvrir un ticket.

---

## 5. Parler au passager

```
GET  /rides/{id}/messages          → { items, unread }
POST /rides/{id}/messages          { "body": "je suis devant le portail" }
POST /rides/{id}/messages/read
```

Mêmes règles que côté passager : `409 conversation_closed` **deux heures** après
l'arrivée, historique toujours lisible.

---

## 6. L'argent — et la DETTE

```
GET /drivers/me/statement?limit=50
```

```json
{ "balance_xof": -4500, "owing": true, "over_limit": false,
  "max_debt_xof": 10000,
  "entries": [ { "id": "…", "ride_id": "…", "kind": "commission",
                 "amount_xof": -450, "reason": "…", "created_at": "…" } ] }
```

> ⚠️ **C'est le point le plus important de cette application.**
>
> Une course en **espèces** vous laisse l'argent en poche et vous fait devoir la
> **commission** à la plateforme : `amount_xof` négatif. Une course payée en
> ligne vous **crédite** votre part : positif.
>
> `balance_xof` **négatif** = vous devez. Au-delà de `max_debt_xof`,
> `over_limit` passe à `true` et **vous ne recevez plus aucun appel**.

**Affichez la dette en permanence**, pas dans un écran caché. Un chauffeur qui
cesse de recevoir des courses sans comprendre pourquoi croit à une panne.

`owing` et `over_limit` sont **rendus calculés** : le signe d'un nombre se lit
mal en un coup d'œil, et « dois-je de l'argent ? » ne doit pas dépendre d'une
comparaison que chaque application refait à sa façon.

---

## 7. Ce que le SOCLE sert (sans `/vtc`)

| | |
|---|---|
| `POST /auth/login` · `/auth/refresh` · `/auth/logout` | la session |
| `GET · PATCH /me` | le profil |
| `PATCH /me/preferences` | `locale` ∈ `fr` · `en` (autre : 422), `theme` |
| `POST /uploads?kind=vehicle` · `?kind=avatar` | les photos — **v3.0.0** : les courses n'avaient **aucune** porte d'envoi, c'est désormais celle du socle, pour tout le monde |
| `GET /wallet` · `/wallet/transactions` | le portefeuille Dira |
| `GET /me/notifications` · `POST /me/devices` | les notifications |
| `GET /agents/{id}/ratings` | vos avis |

- **Téléphone en E.164 avec le `+`** ; sans indicatif, `422` avec `fields: ["phone"]`. `account_suspended` (403) à la connexion : le dire tel quel.
- **Durées de vie des jetons — v3.1.0.** Access token **15 min** (staging : **10 min**), refresh token **30 jours**, consommé à la rotation (le rejouer → 401 → revenir à la connexion). ⚠️ Le socket du suivi est ouvert avec l'access token et **vit plus longtemps que lui** : à l'échéance, le suivi le ferme avec le code **4401 `token_expired`** — rafraîchir (`POST /auth/refresh`) **puis** reconnecter, jamais reconnecter avec le même jeton. Poignée de main : **401** = rafraîchir et revenir ; **403** = ce rôle ne peut pas pousser de positions, ne pas réessayer. L'ancien access token reste valide jusqu'à son échéance après une rotation : un court chevauchement socket / REST est normal.
- **Un `422` nomme ses champs** (`fields`, `reason`) — voir la liste de contrôle du `README`.

---

## 8. ⚠️ Ce que la maquette demande et que l'API ne sert PAS

### ❌ Les documents de conformité (`ch_docs`)

**Aucune route.** Permis, pièce d'identité, carte grise, assurance : l'écran
n'a rien derrière côté courses. La livraison a l'équivalent
(`/food/agent/documents`) ; le partage des deux est en cours.

### ❌ Les statistiques « depuis », « taux d'acceptation », « heures en ligne »

`rides_count` existe. Le reste, non.

### ❌ Le partage de trajet et le bouton d'urgence

Non commencé.

### 🟡 Le montant à encaisser en espèces

`fare_xof` est le prix. Aucun champ ne dit « encaissez ceci » — pour une course
en espèces, c'est le même montant. Un champ explicite serait plus sûr : à
demander si l'écran en a besoin.
