# App CHAUFFEUR — COURSES (VTC) — contrat d'API

> **Version 4.1.0** · 15 septembre 2026
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

---

## 1 bis. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` — le même jeton pour le socle et pour les courses |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | `?limit=20&cursor=<id>` → `{ "items": [...], "next_cursor": "…" }` — `next_cursor` absent = dernière page |
| Montants | **entiers**, en XOF (`…_xof`). Jamais de flottant. |
| Dates | ISO 8601 UTC (`2026-09-13T10:41:23Z`). Une **date seule** s'écrit `YYYY-MM-DD`. |
| Coordonnées | `[lng, lat]`, dans cet ordre, partout |
| Langue | `Accept-Language: fr` ou `en` — les messages d'erreur et les notifications suivent |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ;
le code est le contrat.

**Un `422 validation_failed` nomme ses champs.** `fields` liste les **clés
JSON** en cause : soulignez **ces** cases, pas une bannière sous tout le
formulaire. `reason` précise, quand ce n'est pas la valeur d'un champ :
`unknown_field` (une clé que la route ne connaît pas — **refusée, pas
ignorée**, son nom est dans `fields` ; c'est un bug de l'application) ou
`invalid_json`.

**`401`** = jeton expiré ou invalide : `POST /auth/refresh`, puis rejouer la
requête ; si le refresh échoue, revenir à la connexion. **`403`** = ce rôle,
ou cette personne, n'a pas accès — ne pas réessayer.

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
appeler, et le chauffeur accepte ou refuse.

> **Comment on est appelé — v4.1.0.** C'est un réglage d'exploitation, pas
> une règle de l'application : soit **tous** les chauffeurs libres dans le
> rayon sont appelés en même temps (le premier qui accepte l'emporte), soit
> **un seul** à la fois, du plus proche au suivant dès un refus ou l'échéance.
> Dans les deux cas : **un chauffeur en cours d'appel n'est jamais appelé pour
> une autre course** — un seul écran d'appel à la fois, jamais remplacé par un
> second ; **personne n'est rappelé** sur la même course ; le compte à rebours
> (`expires_at`) est celui du serveur et peut être **plus court** que
> d'habitude quand la recherche touche à sa fin. Refuser explicitement vaut
> mieux que laisser passer : en mode « un par un », c'est ce qui fait sonner
> le suivant tout de suite.

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
  "meta": { "fare_xof": 2250, "class": "eco", "stops": 2, "distance_m": 6141,
            "rider_rating_avg": 4.6, "rider_rating_count": 12 } }
```

> **La note du passager — v3.8.0.** `meta.rider_rating_avg` /
> `rider_rating_count` sont ce que les chauffeurs précédents ont dit de ce
> passager (§4 bis). **Absents** = jamais noté : n'affichez rien, pas un
> « 0 ». Et jamais la moyenne sans le nombre.

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


### 🔥 La ZONE ROUGE — forte demande non servie (v3.3.0)

Quand **plusieurs clients** n'ont pas trouvé de chauffeur au même endroit en peu de
temps (par défaut : cinq en un quart d'heure dans un rayon d'un kilomètre),
la plateforme prévient les chauffeurs **libres et en ligne** à portée (5 km) par un
message FCM **data-only**, sur les mêmes appareils que l'appel :

```json
{ "type": "hot_zone", "signal_id": "…", "vertical": "vtc",
  "lng": "1.238500", "lat": "6.148100", "radius_m": "420", "count": "7",
  "distance_m": "2630", "polyline": "…", "expires_at": "1789238520000" }
```

- Ce n'est **pas un appel** : rien à accepter, personne n'attend une réponse.
  C'est une information — « il y a du travail là-bas » — à afficher comme
  une bannière ou une zone sur la carte, jamais comme une sonnerie.
- `polyline` est le **contour** de la zone, encodé comme un parcours de course
  (Google, précision 5) : ce qui sait dessiner un trajet sait dessiner ce
  contour. `lng`/`lat`/`radius_m` suffisent pour un simple cercle.
- `expires_at` (ms) : passé, ne plus l'afficher. Le message a le même TTL
  chez FCM. `count` est le nombre de clients non servis dans la fenêtre.
- **Idempotence par `signal_id`** : la même zone peut être rappelée (au plus
  toutes les 10 min, 3 fois) — mettre à jour, pas empiler.
- Les zones ouvertes se relisent à tout moment, par exemple à l'ouverture de
  l'application ou après un message manqué :

```
GET https://api-staging.dira.llc/api/v1/analytics/zones?vertical=vtc
→ { "items": [ { "id", "vertical", "center": [lng, lat], "radius_m", "polygon": [[lng, lat]…],
                 "polyline", "count", "opened_at", "updated_at" } ] }
```

La zone est servie **sans les clients qui la font** : l'application n'a rien à
faire de qui attend où.


### 📈 Les ZONES ACTIVES — où est le travail en ce moment (v3.4.0)

À l'ouverture de l'application (et à chaque retour au premier plan), lire :

```
GET https://api-staging.dira.llc/api/v1/analytics/zones/active?vertical=vtc
→ { "from", "to", "requested", "cell_km2": 3,
    "cells": [ { "rank": 1, "label": "N'tifafakomé", "center": [lng, lat],
                 "polygon": [[lng, lat]…], "polyline": "…",
                 "requested": 14, "met": 9, "unmet": 5, "share_pct": 38.9 }, … ] }
```

La ville est découpée en cellules de **3 km²** ; chaque heure, les demandes
de l'heure écoulée sont comptées par cellule et les **cinq premières** sont
servies ici. `center` est le **barycentre des demandes** — là où les clients
appellent, pas le milieu du carré : c'est le point à afficher et vers lequel
guider. `polygon` / `polyline` dessinent la cellule ; `label` est le quartier
quand le SIG le connaît (sinon absent : afficher le rang et la carte, pas
l'identifiant `cell`). `share_pct` est la part de la demande de l'heure.
`cells` vide = rien à montrer (démarrage, nuit calme) — pas une erreur.

C'est une **lecture**, pas un appel : ne rien sonner, ne rien proposer à
accepter. À rafraîchir au plus toutes les 5 minutes — le classement ne
change qu'à l'heure pleine.

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
PATCH /rides/{id}/status                   { "status": "picking_up" | "in_transit" | "completed" }
POST  /rides/{id}/stops/{index}/reached
POST  /rides/{id}/decline                  { "reason": "…" }
GET   /rides/{id}
GET   /rides?cursor=…                      # l'historique de VOS courses
```

```
accepted → picking_up → in_transit → completed
        ↘ cancelled
```

> **v4.0.0 — le vocabulaire commun.** `approach` est devenu **`picking_up`**
> (« je roule vers le passager »), `onboard` est devenu **`in_transit`**
> (« il est à bord ») : ce sont les mots d'une course de livraison aussi, et
> le `PATCH` ne prend plus les anciens (`422`).

```
searching → accepted → picking_up → in_transit → completed
                                            ↘ cancelled   (tout état avant completed)
```

| Statut | Ce que ça veut dire | Course de livraison | Course VTC |
|---|---|---|---|
| `searching` | on cherche quelqu'un | la course attend un livreur — proposée dès que le repas est **prêt** | on appelle des chauffeurs |
| `accepted` | quelqu'un a pris l'opération | un livreur l'a acceptée, il part vers le restaurant | un chauffeur l'a prise |
| `picking_up` | il est au point de départ | la **première collecte** est faite, il en reste | il **roule vers le passager** |
| `in_transit` | le colis / le passager est à bord | toutes les collectes faites, en route vers le client | le passager est monté |
| `completed` | livré / déposé | remise au client | passager déposé |
| `cancelled` | fini sans être fait | commande annulée (client, marchand, exploitation) | par le passager, le chauffeur ou la plateforme |

> ⚠️ **Une course `in_transit` ne s'annule plus.** Le passager est dans la
> voiture ; l'interrompre demanderait de décider où on le dépose. Le bouton
> doit disparaître à ce statut, pas échouer.

`stop_index` avance à chaque arrêt atteint. Les arrêts sont **dans l'ordre
choisi par le passager** : rien n'est réordonné, contrairement aux collectes
d'une livraison.

**Ce que vous voyez et que le passager ne voit pas** : `commission_xof` et
`driver_xof`. C'est votre part, et elle n'est servie qu'à vous.

### La course peut changer SOUS vous (v4.0.0)

Le passager annule pendant que vous roulez vers lui ; l'exploitation
réattribue. Vous l'apprenez par **push** — `ride_cancelled_by_rider`
(`data.type: "ride_status"`, `ride_id`, `status: "cancelled"`, `reason`) —
et, si votre écran est ouvert, rien d'autre ne vous le dira : **relisez
`GET /rides/{id}` à chaque push**, et à chaque retour au premier plan.
`cancelled` = fermer l'écran de course, vous êtes de nouveau appelable.
Une action sur une course annulée répond `409 invalid_transition` :
c'est le signal de relire, pas de réessayer.

**Le flux, dans l'ordre — un signal, un `GET` :**

1. **À l'ouverture d'un écran** : `GET /rides/{id}`. C'est l'état de référence —
   jamais ce que dit le socket.
2. **Socket ouvert** : sur une trame d'état, comparez à ce que vous affichez ;
   si ça diffère, `GET /rides/{id}` et redessinez. La trame porte le statut : vous
   pouvez changer le badge **avant** la réponse. Une trame qui « recule »
   (un `from` qui n'est pas votre état) signale une trame manquée — relisez.
3. **Push reçu** (application en arrière-plan) : `data.type` dit quoi ouvrir,
   l'identifiant sur quoi, `data.status` ce qui a changé. Même geste : ouvrir
   l'écran, `GET /rides/{id}`.
4. **Reconnexion** du socket (back-off 1 s → 2 s → 4 s … 30 s) :
   `GET /rides/{id}` **immédiatement**, avant d'appliquer la moindre trame — tout
   ce qui s'est passé pendant la coupure n'est que dans la base.
5. **Sans socket** (refusé, réseau captif, batterie) : **sondez** `GET /rides/{id}`
   toutes les **10 s** tant que l'opération n'est ni `completed` ni
   `cancelled`, en comparant `updated_at` ; passez à 30 s au bout de cinq
   minutes sans changement. Ne sondez **jamais** une opération terminée.
6. **Retour au premier plan** : `GET /rides?limit=5` et repérer une course
   `accepted`, `picking_up` ou `in_transit` qui est la vôtre — c'est ce qui
   remet l'écran de course en place après un redémarrage de l'application.

**Ce qu'aucun canal ne garantit** : l'ordre, l'unicité, la livraison. Deux
trames pour le même passage (socket **et** push) sont normales — le second
`GET` répond la même chose. Une application qui ferait du socket sa source
de vérité verrait, un jour, une course « en route » qu'un `GET` dit terminée.

### 4 bis. Après la course — noter le passager, recevoir un pourboire (v3.8.0)

```
POST /rides/{id}/rating   { "score": 1..5, "comment": "…" }   // comment facultatif
```

**Vous notez le passager**, et c'est utile : un passager qui ne vient pas au
point de rendez-vous, qui fait attendre, qui salit la voiture — c'est le
chauffeur suivant qui le subit, sauf si vous l'avez dit. Votre note nourrit
`rider_rating_avg` que les chauffeurs voient **à l'appel** (§3), et
l'exploitation la lit. Une fois par course, dans les **7 jours** ; la
réponse (`201`) rend la course avec `rating` = **votre** note. Le passager
vous note aussi : vous ne lisez jamais sa note sur une course donnée, seule
votre **moyenne** bouge (`rating_avg` / `rating_count` du profil, §2).

**Le pourboire** ne se demande pas : le passager le laisse depuis son solde
Dira, et vous recevez **`ride_tip_received`** (push, `{ type:
"ride_tip_received", ride_id, amount_xof }`). La course porte alors
`tip_xof` / `tipped_at`, et votre relevé une écriture **`tip`** (§6) —
**sans commission**.

| Refus | Quand |
|---|---|
| `409 ride_not_completed` | la course n'est pas terminée |
| `409 already_rated` | déjà notée |
| `409 rating_window_closed` | plus de 7 jours |
| `403 forbidden` | pas votre course |

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


### 🔁 L'ENCHAÎNEMENT — un appel avant l'arrivée (v3.4.0)

Quand l'exploitation l'a activé (`GET /settings/dispatch` →
`{ "chain_calls": true, "chain_radius_m": 2000 }`), un chauffeur **passager à
bord** et à moins de `chain_radius_m` de sa destination **reçoit déjà les
appels** de la course suivante — par le socket et par push, comme un appel
ordinaire. S'il accepte :

- la course suivante est `accepted` et porte **`chained_from`** = la course en
  cours. Elle **attend** : ne pas la démarrer, ne pas changer d'écran — la
  course en cours va jusqu'à `completed`, la suivante devient alors la course
  active (elle apparaît dans `GET /rides` avec `chained_from`).
- **une seule** suivante à la fois : pendant qu'elle attend, aucun autre
  appel n'arrive (`driver_busy` sur toute autre acceptation).
- `mission_id` des positions reste celui de la course **en cours** jusqu'à
  son `completed`, puis passe à la suivante.

Afficher « prochaine course : … » sur l'écran de la course en cours quand
`chained_from` existe, et « enchaînement actif » dans le profil quand le
réglage l'est — pour que le chauffeur comprenne pourquoi son téléphone sonne
avant d'avoir déposé. Réglage inactif : rien ne change, une course à la fois.

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
> ligne vous **crédite** votre part : positif. Un **pourboire** (`kind: "tip"`,
> v3.8.0) est positif et entier — la plateforme n'en prend rien.
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
| `GET /agents/{id}/ratings` | vos avis — `id` = votre **profil** (`GET /drivers/me` → `id`) |

- **Téléphone en E.164 avec le `+`** ; sans indicatif, `422` avec `fields: ["phone"]`. `account_suspended` (403) à la connexion : le dire tel quel.
- **Durées de vie des jetons — v3.1.0.** Access token **15 min** (staging : **10 min**), refresh token **30 jours**, consommé à la rotation (le rejouer → 401 → revenir à la connexion). ⚠️ Le socket du suivi est ouvert avec l'access token et **vit plus longtemps que lui** : à l'échéance, le suivi le ferme avec le code **4401 `token_expired`** — rafraîchir (`POST /auth/refresh`) **puis** reconnecter, jamais reconnecter avec le même jeton. Poignée de main : **401** = rafraîchir et revenir ; **403** = ce rôle ne peut pas pousser de positions, ne pas réessayer. L'ancien access token reste valide jusqu'à son échéance après une rotation : un court chevauchement socket / REST est normal.
- **Un `422` nomme ses champs** (`fields`, `reason`) — §1 bis.

---

## 8. ⚠️ Ce que la maquette demande et que l'API ne sert PAS

### ❌ Les documents de conformité (`ch_docs`)

**Aucune route.** Permis, pièce d'identité, carte grise, assurance : l'écran
n'a rien derrière côté courses. La livraison a l'équivalent
(`/food/agent/documents`) ; le partage des deux est en cours.

### ❌ Les statistiques « depuis », « taux d'acceptation », « heures en ligne »

`rides_count` existe — et compte réellement depuis la v3.8.0 (il restait à
zéro). Le reste, non.

### ❌ Le partage de trajet et le bouton d'urgence

Non commencé.

### 🟡 Le montant à encaisser en espèces

`fare_xof` est le prix. Aucun champ ne dit « encaissez ceci » — pour une course
en espèces, c'est le même montant. Un champ explicite serait plus sûr : à
demander si l'écran en a besoin.
