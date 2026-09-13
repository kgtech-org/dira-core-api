# App CLIENT — COURSES (VTC) — contrat d'API

> **Version 4.0.0** · 13 septembre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Courses : `https://api-staging.dira.llc/api/v1/vtc` · Suivi : `wss://tracking-staging.dira.llc`

---

> ## ⚠️ DEUX BASES D'URL
>
> | Ce que vous appelez | Service | Base |
> |---|---|---|
> | connexion, profil, adresses, **portefeuille**, paiements, notifications | **socle** | `…/api/v1/…` |
> | tout le reste de ce document | **courses** | `…/api/v1/vtc/…` |
>
> Le **jeton est le même** des deux côtés : même secret, même session. On ne se
> connecte pas deux fois.

---

## 0. Conventions

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

## 1. Le modèle en une phrase

Un passager compose un trajet, **voit un prix avant de commander**, et la
plateforme cherche un chauffeur pendant qu'il attend.

Trois choses à comprendre avant de coder :

**Le prix ne vient jamais de vous.** Un devis est mémorisé côté serveur et
expire en deux minutes. Vous envoyez son identifiant, pas un montant — sans
quoi on commanderait une course à un franc.

**Les arrêts sont dans l'ordre choisi.** Contrairement aux collectes d'une
livraison, rien n'est réordonné : « d'abord chez ma sœur, puis l'aéroport » se
fait dans cet ordre.

**Une course à bord ne s'annule plus.** Interrompre demanderait de décider où
déposer quelqu'un, et ce n'est pas une décision qu'un bouton prend.

---

## 2. Les classes

```
GET /classes
```

```json
{ "items": [ { "key": "eco", "name": "Éco", "note": "Citadine · 1-4 passagers", "seats": 4 } ] }
```

⚠️ **Aucun prix ici.** Une grille tarifaire affichée hors d'un trajet donne un
chiffre que la course ne confirmera pas. Le prix vient du devis, pour CE
trajet.

Si la liste est vide, le service **refuse tout devis** (`409
no_classes_configured`) : ce n'est pas une panne réseau, c'est une plateforme
non configurée. Dites-le autrement qu'avec « réessayez ».

---

## 3. Le devis — le prix avant de commander

```
POST /rides/quote
{ "stops": [ { "kind": "pickup", "label": "Almadies", "geo": [1.2255, 6.1319] },
             { "kind": "dest",   "label": "Aéroport", "geo": [1.2545, 6.1656] } ] }
```

`kind` vaut `pickup`, `stop` ou `dest`. De **2 à 5** arrêts.

Réponse : **une entrée par classe**, toutes tarifées sur le même trajet.

```json
{ "items": [ {
  "id": "6aa2…", "class_key": "eco",
  "stops": [...], "distance_m": 6141, "duration_s": 467,
  "surge_name": "Aéroport", "surge_multiplier": 1300,
  "fare_xof": 2250,
  "expires_at": "2026-09-10T12:43:20Z"
} ] }
```

> ⚠️ **`expires_at` n'est pas décoratif.** Passé cet instant, la confirmation
> est refusée (`409 quote_expired`). Une application qui l'ignore laisse son
> utilisateur devant un bouton qui échouera. Redemandez un devis plutôt que de
> laisser le bouton actif.

> ⚠️ **La majoration se DIT.** `surge_multiplier` est en **millièmes** :
> `1300` = ×1,3. Renseigné, il doit apparaître à l'écran — une majoration
> découverte au paiement se lit comme une arnaque ; annoncée, elle informe.

**`409 route_unavailable`** : le réseau routier est injoignable. Aucun prix
n'est deviné — une distance à vol d'oiseau ferait payer un trajet qui n'existe
pas. C'est un refus temporaire, pas une erreur de saisie.

---

### ⚠️ Une course se fait DANS une ville (v3.6.0)

Le départ et l'arrivée doivent être dans la **même ville desservie** —
dedans, ou à moins de **50 km** de sa limite (la banlieue, la plage,
l'aéroport voisin). Le refus arrive **au devis**, avant tout prix :

```
422 { "error": { "code": "out_of_service_area",
                 "message": "Ce lieu est à 380 km de Lomé : nous ne desservons pas au-delà de 70 km de la ville" } }
422 { "error": { "code": "different_cities",
                 "message": "Le départ est à Lomé et l'arrivée à Kara : une course se fait dans une seule ville" } }
```

Afficher le message tel quel (il est localisé et porte la distance). Les
villes sont publiques — `GET /cities` → `{ items: [ { key, name,
center: [lng, lat], radius_km, tolerance_km, active } ] }` — pour dire « nous
ne desservons pas encore ici » dès qu'un passager pose un point, avant même
le devis : un point est acceptable s'il est à moins de `radius_km +
tolerance_km` du `center` d'une ville active, et les deux points doivent
tomber dans la **même** ville.

---

## 4. Commander

```
POST /rides
{ "quote_id": "6aa2…", "payment_method": "cash" | "wallet" | "online" }
```

⚠️ **Aucun montant.** Le prix vient du devis mémorisé.

| Moyen | Ce qui se passe à la commande | À l'annulation |
|---|---|---|
| `cash` | rien : le chauffeur encaisse à l'arrivée | rien à rendre |
| `wallet` | le **solde Dira est débité** immédiatement | **remboursé** sur le portefeuille |
| `online` | `payment_url` est rendue — **rien n'est encaissé** | remboursé **si** le paiement a été confirmé |

> ⚠️ **`402 insufficient_funds`** sur `wallet` : le solde ne couvre pas la
> course. Ce n'est pas une panne — routez vers la recharge du portefeuille
> (`POST /wallet/purchase`, **au socle**), pas vers un message d'erreur.
>
> Encaisser AVANT d'appeler est délibéré : un chauffeur qui accepte une course
> impayable aura roulé pour rien, et le découvrir à l'arrivée est le pire
> moment pour tout le monde.

> ⚠️ **`payment_url` ne prouve RIEN.** Elle ouvre la page de l'opérateur. La
> course reste impayée tant que le serveur n'a pas reçu la confirmation :
> conclure au retour de cette page offrirait la course. Rafraîchissez
> `GET /rides/{id}` et lisez… rien de plus que le statut — le passager n'a pas
> besoin de savoir si la plateforme a encaissé.

La réponse est la course, en `searching`.

---

## 4 bis. Programmer une course — ponctuelle ou récurrente (v3.5.0)

Une programmation est une **intention datée**, pas une course. À l'heure
dite, la plateforme fait ce que le passager aurait fait — un devis **au prix
du moment**, une commande, l'appel d'un chauffeur — et la course qui en sort
est une course ordinaire, avec `schedule_id`.

```
POST /scheduled-rides
{ "stops": [ { "kind": "pickup", "label": "Maison", "geo": [lng, lat] },
             { "kind": "dest",   "label": "Bureau", "geo": [lng, lat] } ],
  "class_key": "eco", "payment_method": "cash",
  "kind": "once",      "at": "2026-09-15T07:30:00Z",              # ponctuelle
  — ou —
  "kind": "recurring", "recurrence": { "days": [1,2,3,4,5], "time": "07:30",
                                       "tz": "Africa/Lome", "until": "2026-12-31T00:00:00Z" },
  "alert_lead_min": 5, "note": "…" }
→ 201 { "id", "kind", "status": "active", "at": <prochaine occurrence>, "alert_at",
        "recurrence", "describe": "lun, mar, mer, jeu, ven à 07:30", "alert_lead_min", "runs": [] }

GET  /scheduled-rides            # les actives et en pause ; ?all=1 pour tout
GET  /scheduled-rides/{id}       # avec `runs` : chaque lancement
POST /scheduled-rides/{id}/pause · /resume · /cancel
```

- `at` est l'heure du **lancement de l'appel** — pas une heure d'arrivée du
  chauffeur. Une ponctuelle doit être au moins `alert_lead_min` minutes dans
  le futur (sinon `422` : commander tout de suite). Les jours de la
  récurrence sont ISO (1 = lundi … 7 = dimanche), l'heure est **locale** au
  fuseau donné (`Africa/Lome` par défaut).
- ⚠️ **Le passager est PRÉVENU avant que l'appel ne parte** :
  `ride_scheduled_soon` (« nous appelons un chauffeur dans 5 min ») à
  `alert_at` ; puis `ride_scheduled_started` quand la course existe (data :
  `ride_id` — ouvrir l'écran de la course, elle est `searching`) ; ou
  `ride_scheduled_failed` avec la raison (solde insuffisant, itinéraire,
  plateforme indisponible à l'heure dite). Data commun :
  `{ "type": "scheduled_ride", "schedule_id", "at" }`.
- `runs[]` : `launched` (avec `ride_id`), `failed` (avec `reason`),
  `skipped` (occurrence passée en pause). Une occurrence manquée de plus de
  15 min n'est **pas** lancée en retard.
- `pause` suspend ; `resume` reprend à la prochaine occurrence — une
  ponctuelle dont l'heure est passée ne reprend pas (`409 schedule_passed`).
  `cancel` met fin ; les courses déjà lancées continuent.
- Statuts : `active` · `paused` · `done` (ponctuelle lancée, ou récurrence
  arrivée à `until`) · `cancelled`.

---

## 5. La course, statut par statut

```
searching → accepted → picking_up → in_transit → completed
         ↘ cancelled (par le passager, le chauffeur, ou l'échec de la recherche)
```

> **v4.0.0 — le vocabulaire commun.** Ce sont les **mêmes mots** que pour une
> course de livraison et que la fin d'une commande de repas : une application
> qui suit les deux n'a qu'un seul écran d'état à écrire.

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

La **commande de repas** garde ce qui n'existe que pour un repas —
`pending_payment → paid → preparing → ready` — puis reprend **mot pour mot** le
cycle de sa course : `accepted → picking_up → in_transit → completed`.
`ready` côté commande = `searching` côté course.

Ce qui a été **renommé** en v4.0.0 (les anciens mots n'existent plus dans
aucune réponse, et sont refusés en entrée) :

| Avant | Après | Où |
|---|---|---|
| `available` | `searching` | course de livraison |
| `assigned` | `accepted` | course de livraison, commande |
| `delivering` | `in_transit` | course de livraison, commande |
| `delivered` | `completed` | course de livraison, commande |
| `approach` | `picking_up` | course VTC |
| `onboard` | `in_transit` | course VTC |

| Statut | Ce que voit le passager |
|---|---|
| `searching` | « nous cherchons un chauffeur » — `driver_id` est vide |
| `accepted` | un chauffeur a pris la course — `driver` dit qui (v3.8.0) |
| `picking_up` | il roule vers le point de départ |
| `in_transit` | le passager est à bord |
| `completed` | terminée |
| `cancelled` | `cancelled_by` dit qui, `cancelled_reason` pourquoi |

```
GET /rides/{id}
GET /rides?cursor=…     # l'historique de VOS courses, page par page
```

**Le parcours d'une course terminée (v3.2.0).** À `completed`, la course
porte **`traveled_polyline`** (polyline Google, le chemin réellement roulé,
recalé sur la route), **`actual_distance_m`** et **`distance_source`** :
`tracked` quand le tracé existe, `planned` quand le chauffeur n'a envoyé
aucune position — la distance est alors celle du devis et il n'y a rien à
dessiner. Les trois champs sont absents avant la fin de la course et sur une
course annulée. **Le prix ne dépend pas de ce tracé** : `fare_xof` vient du
devis, le parcours est ce qu'on montre dans le détail et le reçu.

**La carte du chauffeur (v3.8.0).** Dès `accepted`, `GET /rides/{id}` porte
`driver` : `{ id, name, rating_avg, rating_count, rides_count,
vehicle: { class_key, brand, model, license_plate, color } }` — qui vient,
dans quelle voiture, avec quelle note. `id` est le **profil** du chauffeur,
celui dont `GET /agents/{id}/ratings` liste les avis. Absente sur la liste
`GET /rides` : c'est le détail qui la porte. Pas de téléphone : la mise en
relation passe par la conversation (§7).

`stop_index` est l'étape **atteinte** — zéro tant que le départ n'est pas fait.
Chaque `stops[i].reached_at` porte l'instant.

> ⚠️ **`commission_xof` et `driver_xof` ne vous sont pas servis.** Ce que la
> plateforme prend est une affaire entre elle et le chauffeur ; l'afficher au
> passager le ferait s'interroger sur ce qui revient à qui au moment où il
> paie. Ne les attendez pas.

> ⚠️ **Une recherche épuisée n'annule PAS la course.** Elle reste `searching`,
> et l'exploitation peut attribuer à la main. N'affichez pas « annulée » sur un
> délai : affichez que la recherche continue.

**Annuler** :

```
POST /rides/{id}/cancel   { "reason": "…" }   // motif facultatif
```

`409 invalid_transition` sur une course `in_transit` : le bouton doit
disparaître à ce statut, pas échouer.

---

## 6. Suivre le chauffeur

Le suivi temps réel passe par **`dira-tracking`**, sur son propre hôte — pas
par cette API.

```
WS wss://tracking-staging.dira.llc/track/subscribe/{ride_id}

{ "type": "hello",    "mission_id": "…" }
{ "type": "position", "vehicle_id": "…", "vehicle_type": "voiture", "plate": "…",
  "lng": 1.2255, "lat": 6.1319, "heading": 122.5, "speed": 8.3, "ts": 1757… }
{ "type": "status",   "status": "picking_up", "ts": 1757… }
```

L'identifiant de course sert d'identifiant de mission (`mission_id` =
`ride_id`). Le jeton d'accès passe dans l'URL (`?token=`) — un WebSocket de
navigateur ne porte pas d'en-tête.

- **Reconnexion avec back-off** (1 s, 2 s, 4 s … 30 s) : le socket tombe
  quand le téléphone change de réseau ; ne laissez pas une voiture figée sur
  la carte.
- **Interpolation** entre deux positions : une position toutes les quelques
  secondes, un marqueur qui glisse — pas qui saute.
- **Repli REST** : sans socket, `GET /rides/{id}` toutes les 10 s donne
  l'état ; la position, elle, ne se lit que sur le socket.
- **Échéance du jeton** : le suivi ferme le socket avec le code **4401
  `token_expired`** — rafraîchir (`POST /auth/refresh`) **puis** reconnecter,
  jamais reconnecter avec le même jeton.
- Poignée de main : **401** = rafraîchir et revenir ; **403** = ce rôle ne
  peut pas suivre cette course, ne pas réessayer.

⚠️ **La base d'URL du suivi est distincte de celle de l'API** : deux variables
d'environnement.

### Suivre l'état — socket, push, et `GET` (v4.0.0)

**Le socket du suivi porte aussi les changements d'état de la course** :
une trame `{ "type": "status", "status": "…" }` à chaque passage —
`accepted`, `picking_up`, `in_transit`, `completed`, `cancelled`. Elle ne
porte que le mot : à réception, **relisez `GET /rides/{id}`** et redessinez
(qui vient, où en est-il, le prix, le parcours). Vous pouvez changer le badge
avant la réponse.

**Application en arrière-plan** : les mêmes passages arrivent en **push**
(socle, `POST /me/devices`), avec en données `type: "ride_status"`,
`ride_id`, `status` :

| Gabarit | Quand | Données |
|---|---|---|
| `ride_accepted` | un chauffeur a pris la course — nom et voiture dans le texte | `status: accepted` |
| `ride_driver_on_the_way` | il roule vers vous | `status: picking_up` |
| `ride_cancelled` | annulée par le chauffeur ou la plateforme (`reason`) | `status: cancelled` |
| `ride_rate_prompt` | terminée — noter, remercier (§7 bis) | `type: ride_rate_prompt` |
| `ride_scheduled_soon` · `_started` · `_failed` | course programmée (§4 bis) | |

À réception : ouvrir la course, `GET /rides/{id}`. Une trame reçue deux fois
(socket **et** push) est normale — le second `GET` répond la même chose.

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

**Ce qu'aucun canal ne garantit** : l'ordre, l'unicité, la livraison. Deux
trames pour le même passage (socket **et** push) sont normales — le second
`GET` répond la même chose. Une application qui ferait du socket sa source
de vérité verrait, un jour, une course « en route » qu'un `GET` dit terminée.

---

## 7. Parler au chauffeur — sans échanger de numéros

```
GET  /rides/{id}/messages          → { items, unread }
POST /rides/{id}/messages          { "body": "je suis au portail bleu" }
POST /rides/{id}/messages/read
```

Une conversation par course, entre ses deux parties, sans téléphone échangé.
`items` sont les messages du plus ancien au plus récent (`from`: `client` |
`driver`, `body`, `created_at`) ; `unread` compte ceux que vous n'avez pas
encore marqués lus. `body` : 1 à 1000 caractères.

| Refus | Quand |
|---|---|
| `409 no_driver_yet` | personne n'a encore pris la course — **lisible, pas écrivable** |
| `409 conversation_closed` | plus de **deux heures** après l'arrivée |
| `403 forbidden` | cette conversation n'est pas la vôtre |

> La saisie se désactive sur les deux premiers ; l'historique **reste lisible**.
> Deux heures après l'arrivée : « j'ai oublié mon téléphone sur la banquette »
> se dit dans la minute qui suit, pas un mois plus tard.

---

## 7 bis. Noter et remercier — après la course (v3.8.0)

À `completed`, le passager reçoit **`ride_rate_prompt`** (push, données
`{ type: "ride_rate_prompt", ride_id }`) : ouvrir l'écran de fin de course
dessus, avec les deux gestes.

```
POST /rides/{id}/rating   { "score": 5, "comment": "…" }      // comment facultatif, 1000 car. max
POST /rides/{id}/tip      { "amount_xof": 500 }               // 100 ≤ montant ≤ 50 000
```

Les deux rendent la **course** (`201`) : `rating` porte la note que **vous**
venez de laisser, `tip_xof` / `tipped_at` le pourboire passé. Sur
`GET /rides/{id}` et l'historique, `rating` absent = **pas encore notée** —
c'est ce qui affiche « notez votre course » ; `tip_xof` absent = pas de
pourboire.

**La note** va au **profil du chauffeur** (`driver.rating_avg`,
`driver.rating_count`, ses avis sur `GET /agents/{driver.id}/ratings`). Une
fois par course, dans les **7 jours** qui suivent l'arrivée. Le chauffeur note
aussi le passager — vous ne lisez jamais sa note, il ne lit jamais la vôtre :
`rating` est toujours **la vôtre**.

**Le pourboire** part du **solde Dira** (`GET /wallet`) et arrive au chauffeur
**sans commission**, en une fois par course. Il est prévenu à l'instant.

| Refus | Quand |
|---|---|
| `409 ride_not_completed` | la course n'est pas terminée — masquer les deux gestes avant |
| `409 already_rated` · `409 already_tipped` | déjà fait : afficher ce qui a été laissé, pas le formulaire |
| `409 rating_window_closed` · `409 tip_window_closed` | plus de 7 jours |
| `409 no_driver` | course sans chauffeur (annulée en recherche) |
| `402 insufficient_funds` | solde Dira insuffisant — **la course reste sans pourboire** : proposer la recharge (`POST /wallet/purchase`), puis réessayer |
| `422` | note hors de 1..5, montant hors de 100..50 000 |

> ⚠️ **Pas de pourboire en espèces ni par mobile money ici.** Un pourboire en
> espèces se donne dans la voiture et ne regarde pas la plateforme ; un
> pourboire par opérateur attendrait le rappel pour un geste qui doit être
> immédiat. L'écran propose le solde Dira, et la recharge s'il manque.

---

## 8. Ce que le SOCLE sert (sans `/vtc`)

| | |
|---|---|
| `POST /auth/register` · `/auth/login` · `/auth/refresh` · `/auth/logout` | la session |
| `GET · PATCH /me` · `PATCH /me/preferences` | le profil |
| `POST /uploads?kind=avatar` | la photo de profil — **v3.0.0**, une seule porte pour toute la plateforme |
| `GET · POST /me/addresses` · `PUT · DELETE /me/addresses/{id}` | le carnet d'adresses |
| `GET /wallet` · `/wallet/transactions` · `POST /wallet/purchase` | le solde Dira |
| `GET /payments/providers` · `POST /payments/initiate` · `GET /payments/{id}` | mobile money |
| `GET /me/notifications` · `POST /me/devices` | les notifications |
| `GET /agents/{id}/ratings` | les avis d'un chauffeur — `id` = `driver.id` de la course (le profil) |

- **Inscription** : `{ phone (E.164, avec le +), name, password, role: "client", email?, first_name?, last_name? }`. Sans `+`, `422` avec `fields: ["phone"]` ; `phone_taken` (409) → proposer la connexion ; `account_suspended` (403) → le dire tel quel.
- **Un `422` nomme ses champs** (`fields`, `reason`) — §0.
- `GET /wallet` répond toujours `200` à un client : le Dira Cash s'ouvre à la première lecture.

---

## 9. ⚠️ Ce que la maquette demande et que l'API ne sert PAS

Écrit ici pour être découvert **maintenant**, pas à l'intégration.

### ✅ Noter une course, laisser un pourboire — SERVIS (v3.8.0)

`POST /rides/{id}/rating` et `POST /rides/{id}/tip` — §7 bis. L'écran de fin
de course a tout ce qu'il lui faut, y compris la carte du chauffeur (§5).

### ✅ Les adresses enregistrées « Maison » / « Travail » — SERVIES

Le carnet d'adresses du socle porte tout ce que la maquette demande :

```
GET    /me/addresses
POST   /me/addresses      { "label": "Maison", "address": "…", "geo": [lng, lat],
                            "details": "portail bleu", "is_default": true }
PUT    /me/addresses/{id}
DELETE /me/addresses/{id}
```

`label` est le nom que l'utilisateur donne — « Maison », « Bureau ». Les
raccourcis de l'écran d'accueil se câblent dessus, et `geo` alimente
directement le devis.

> ⚠️ **`is_default` est UNIQUE par compte** : poser le drapeau le retire de
> l'ancienne, dans la même opération. N'entretenez pas votre propre notion de
> « par défaut » en local, elle divergerait.
>
> Le carnet est **partagé avec la livraison** : c'est le même compte et le même
> socle. Une adresse ajoutée depuis l'application de courses apparaît côté
> livraison, et c'est voulu.

### ❌ Le partage de trajet et le contact d'urgence

L'écran « sécurité » n'a aucune route. Non commencé.

### 🟡 Le prix « à partir de » sur l'écran d'accueil

`GET /classes` ne rend aucun prix, et c'est délibéré. Si la maquette en affiche
un, il faut demander un devis — donc connaître le trajet.
