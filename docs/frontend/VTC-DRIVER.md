# App CHAUFFEUR — COURSES (VTC) — contrat d'API

> **Version 4.11.0** · 21 septembre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Courses : `https://api-staging.dira.llc/api/v1/vtc` · Suivi : `wss://tracking-staging.dira.llc` · SIG : `https://maps.dira.llc/api`

---

## 1. Quatre back-ends, deux sockets

| | Rôle |
|---|---|
| **Socle** | connexion, profil, **matériel**, notifications, avis reçus — base `…/api/v1/` |
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
| Pays | `X-Dira-Country: TG` — sur chaque requête ; la réponse porte le pays **retenu** (voir la section *Le pays*) |

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

## ⚠️ Le PAYS — `X-Dira-Country` (v4.2.0)

Dira s'installe pays par pays, et **toute donnée est bornée par pays** :
un compte, une commande, une course, une enseigne, un chauffeur portent un
`country` (ISO 3166-1 alpha-2 : `TG`, `BJ`, `CI`…) et une application ne
voit que ceux de **son** pays. Ce que l'application doit faire tient en
trois gestes.

**1. Envoyer son pays dans l'en-tête `X-Dira-Country`, sur chaque
requête** — socle, métier, suivi. Le serveur répond avec le pays **retenu**
dans le même en-tête : lisez-le, et si votre valeur diffère, alignez-vous.
Pour un compte ordinaire, l'en-tête **informe** — le pays du compte,
inscrit dans le jeton, s'impose ; un client ne change pas de pays en
changeant un en-tête. Sans jeton (inscription, connexion, `GET /countries`),
l'en-tête est **admis** s'il nomme un pays ouvert, sinon le pays par défaut
du déploiement s'applique. Là où l'on ne peut pas poser d'en-tête (une
ouverture de WebSocket depuis un navigateur), `?country=TG` fait le même
travail.

**2. Trouver son pays** — avant l'inscription, puis à chaque démarrage :

```
POST /api/v1/me/country/resolve        (connecté)
{ "lng": 1.2255, "lat": 6.1319 }       — la position de l'appareil, si vous l'avez
{}                                     — sinon corps vide : l'adresse IP décide

→ 200 {
  "country":   "TG",        ← le pays RETENU : envoyez-le dans X-Dira-Country
  "source":    "geo",       ← geo | ip | account | default
  "detected":  "TG",        ← où la position / l'IP situe la personne, même hors zone
  "supported": true,        ← `detected` est un pays ouvert
  "updated":   false        ← le pays du compte a changé
}
```

Deux signaux, dans l'ordre : la **position** de l'appareil (`geo`, la
vérité à cent mètres près) ; sinon, ou si la position est hors zone,
l'**adresse IP** de la requête (`ip`, moins sûre — un opérateur mobile sort
parfois par un autre pays). Un signal qui désigne un pays **non ouvert** ne
vaut pas : `supported: false`, `detected` dit où la personne est, et
`country` reste celui du compte (`account`) ou, s'il n'en avait pas, le pays
par défaut (`default`). C'est le moment d'afficher « Dira n'est pas encore
disponible au Ghana » — sans bloquer : le compte reste utilisable dans son
pays.

**Avant l'inscription**, le compte n'existe pas : appelez `GET /countries`
(public) pour la liste des pays ouverts, avec leur **indicatif**, et
posez `X-Dira-Country` sur `POST /auth/register` avec le pays où la position
de l'appareil tombe (calculé côté appareil, ou après une première
connexion). Sans en-tête, le serveur déduit le pays de l'**indicatif** du
téléphone (`+228` → `TG`, `+229` → `BJ`), puis du pays par défaut. Le compte
porte le résultat dans `user.country`.

**3. Rafraîchir la session quand le pays change.** `updated: true` veut
dire que le compte a changé de pays — mais le jeton en cours porte encore
l'ancien (`cty`), et c'est **le jeton** qui borne les listes. Faites un
`POST /auth/refresh` tout de suite : le nouveau jeton porte le nouveau pays.
Un voyageur qui ouvre l'application à Cotonou devient béninois pour Dira —
c'est ce qu'il veut, commander à Cotonou. Son historique togolais ne bouge
pas, il est marqué de son pays d'alors.

```
GET /api/v1/countries                  (public)
→ 200 { "default": "TG", "items": [
  { "code": "TG", "name": "Togo", "currency": "XOF", "currency_name": "Franc CFA (UEMOA)",
    "currency_symbol": "F CFA", "currency_decimals": 0, "phone_prefix": "+228",
    "locale": "fr", "timezone": "Africa/Lome", "center": [1.2255, 6.1319],
    "enabled": true, "default": true }
]}
```

**La monnaie vient du pays.** Tout montant de la plateforme est un entier
dans la plus petite unité de la monnaie du pays où il a été créé — un prix
sous `GN` est en francs guinéens, sous `TG` en francs CFA. Rien n'est
converti : formatez avec `currency_symbol` et `currency_decimals` du pays
courant (« 2 500 F CFA », « 35 000 FG »). Les champs nommés `…_xof` sont
un héritage de nommage : ils portent la monnaie du pays.

Trois pays sont ouverts d'office : **Togo** (`TG`), **Sénégal** (`SN`),
**Guinée** (`GN`). Les autres s'ouvrent depuis la console.

`GET /me` porte `country`. **Ne le mettez pas en cache au-delà d'une
session** : la résolution du démarrage suivant peut le changer.

> ⚠️ **Ne pas envoyer d'en-tête n'est pas une erreur, mais c'est un
> silence** : le serveur retombe sur le pays du compte, puis sur celui du
> déploiement, et l'application ne saura jamais qu'elle a été rangée
> ailleurs que là où elle croit être. L'en-tête est ce qui rend l'écart
> visible — dans la réponse.

## 2. Le profil chauffeur

```
GET   /drivers/me                   → crée le profil au premier appel
GET   /drivers/me/stats?date=YYYY-MM-DD&tz=Africa/Lome   → { rides, driver_xof, online_s }   (v3.1.0)
PATCH /drivers/me/online            { "online": true | false }
GET   /drivers/me/vehicles
POST  /drivers/me/vehicles          { class_key, brand, model, license_plate, color, seats, photo_url?, images? }
                                    images / photo_url : téléverser d'abord — POST /api/v1/uploads?kind=vehicle (SOCLE, v3.0.0)
PATCH /drivers/me/active-vehicle    { "vehicle_id": "…" }
```

> **Le véhicule — couleur, description, photos (v4.10.0).** Chaque véhicule
> rendu porte :
> - **`description`**, GÉNÉRÉE par le serveur — *marque modèle couleur* :
>   « Toyota Avensis rouge ». Affichez-la telle quelle partout où un véhicule
>   se nomme (liste, sélecteur, fiche) ; ne recomposez rien, ne la demandez
>   jamais en saisie. `label` reste ce qu'on reconnaît dans la rue, avec la
>   plaque (« Toyota Avensis · TG-4417 »).
> - **`color`** : la couleur saisie au formulaire (« Rouge » — le serveur la
>   met en minuscules dans la description). Proposez-la dans le formulaire de
>   déclaration, au même titre que la marque et le modèle.
> - **`images[]`** : toutes les photos du véhicule (extérieur, intérieur,
>   plaque — **8 au plus**), jamais `null`, dans l'ordre de dépôt ;
>   **`photo_url`** est la COUVERTURE (celle choisie, sinon la première).
>   Formulaire : chaque photo passe d'abord par `POST /uploads?kind=vehicle`
>   → `{ url }`, puis la liste des URLs part dans `images` ; `photo_url`
>   facultatif. Un envoi qui échoue ne doit pas faire perdre la saisie.

```json
{ "id": "…", "user_id": "…", "active_vehicle_id": "…", "zone": "Lomé Centre",
  "online": true, "status": "active",
  "last_seen_at": "2026-09-12T10:41:23Z", "tracking_stale": false,
  "offline_at": null, "offline_reason": "",
  "rating_avg": 4.7, "rating_count": 132, "rides_count": 418 }
```

> **La présence — v3.1.0.** `last_seen_at` est la dernière position que le
> suivi a vue de votre véhicule (lue à l'instant) ; **`tracking_stale`** est
> vrai après **2 min** sans position (v4.1.1 — 90 s avant : un téléphone
> en poche faisait clignoter « suivi arrêté »), ou jamais vu — c'est
> « suivi arrêté » :
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
| `402 debt_over_limit` | la dette a franchi le plafond — voir §6 |
| `402 equipment_overdue` | une échéance de **matériel** est en retard au-delà du seuil du contrat — voir §6 bis (v4.11.0) |

⚠️ **La classe du véhicule décide de ce que vous pouvez prendre (v4.6.0).**
Un véhicule sert les courses de **son mode et des modes avant lui** dans
`GET /classes` — l'ordre de la liste est une hiérarchie : eco < confort <
van. Une confort est appelée pour une course eco (le passager monte dans
mieux) ; une eco **n'est jamais appelée** pour une course confort, et une
citadine ne sert pas une course « van » : l'accepter mettrait six personnes
dans quatre places. Le serveur trie **avant** de sonner ; forcer
l'acceptation répond `409 vehicle_class_mismatch`.

Les classes proposées à la déclaration viennent de `GET /classes` (public) :
`key`, `name`, `icon_url` (image), `map_icon_url` / `map_icon`. **Affichez
`icon_url` + `name` tels que servis** — les modes se règlent depuis la
console, et un mode peut s'ajouter (v4.5.0). Le `class` d'un appel (§3)
est la `key`.

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
| `409 vehicle_class_mismatch` | le véhicule ne sert pas le mode de la course (v4.6.0) — n'arrive qu'en forçant `POST /rides/{id}/accept` sur une course pour laquelle vous n'avez pas été appelé ; l'écran d'appel n'en verra jamais |
| `409 rider_cannot_pay` | le passager payait sur son solde Dira et ne peut plus (v4.4.0) : **la course a été annulée**, vous êtes libre — fermez l'écran, aucune course ne vous attend |

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

**Le trajet aussi peut changer sous vous (v4.4.0).** Le passager — ou
l'exploitation — ajoute un arrêt, en retire un, change la destination
pendant que vous roulez. Vous recevez `ride_stops_changed`
(`data.type: "ride_status"`, `event: "stops_changed"`, `ride_id`,
`fare_xof`, `delta_xof`) : **relisez `GET /rides/{id}`** et redessinez
l'itinéraire. Ce qui change : `stops` (les arrêts déjà atteints restent en
tête, inchangés), `distance_m`, `duration_s`, `fare_xof`, `driver_xof`
(votre part suit), et `fare_adjustments[]` qui garde l'historique. Le
montant à encaisser en **espèces** est le nouveau `fare_xof` — la
différence n'a pas été prise au passager, c'est vous qui l'encaissez
(`movement: cash`). Pour une course payée sur le solde, la différence a déjà
bougé (`charged` / `refunded`) : rien à demander.

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
  "heading": 40, "heading_source": "gps", "speed": 12, "ts": 1789044151142 }
```

**Le CAP est attendu, pas facultatif (v4.4.0).** `heading` est la direction
du véhicule, en degrés depuis le **nord vrai** (0–360, 0 = nord, 90 = est) ;
c'est ce qui oriente la voiture sur la carte de l'exploitation. Deux
sources, et `heading_source` dit laquelle :

| Source | Quand | Comment |
|---|---|---|
| `gps` | en mouvement (vitesse > ~1,5 m/s) | le `bearing` de la position (Android `Location.bearing` si `hasBearing()`, iOS `CLLocation.course` ≥ 0) |
| `compass` | à l'arrêt, ou quand le GPS n'a pas de cap | les capteurs : **vecteur de rotation** (`TYPE_ROTATION_VECTOR` / `CMDeviceMotion.heading`), converti en azimut, corrigé de la **déclinaison magnétique** (`GeomagneticField`) pour rendre le nord vrai, lissé (moyenne circulaire sur ~1 s) |

```json
{ "vehicle_id": "…", "mission_id": "…", "lng": 1.2255, "lat": 6.1319,
  "heading": 40, "heading_source": "gps", "speed": 12, "ts": 1789044151142 }
```

Sans capteur exploitable (téléphone sans magnétomètre, calibration
impossible), omettez les deux champs : le serveur garde le dernier cap
connu. N'envoyez jamais `0` pour « inconnu » — c'est le nord.

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

> 🔔 **Le message est POUSSÉ à l'autre côté (v4.8.0).** Il n'y a pas de
> socket de conversation sur les courses : quand l'autre application n'est
> pas sur l'écran de la course, c'est la notification `chat_message` qui
> l'atteint — titre « Nouveau message », corps = le texte, **jamais le nom
> de l'expéditeur** — avec `data: { type: "chat_message", ride_id }`. Ouvrez
> la conversation de cette course dessus et relisez
> `GET /rides/{id}/messages`. Sur l'écran de la course, sondez la
> conversation toutes les 5 s tant qu'elle est ouverte ; une notification
> reçue pendant ce temps ne s'affiche pas deux fois — c'est le même message.

---

## 5 bis. Le SUPPORT — et l'objet oublié dans votre véhicule (v4.9.0)

```
POST /tickets                       { category, message, ride_id? }   → 201 ticket
GET  /tickets                       → mes tickets ET les objets perdus qui me concernent
GET  /tickets/{id}                  → le ticket et son fil
POST /tickets/{id}/messages         { "body": "…" }
POST /tickets/{id}/lost-item        { "found": true | false, "note": "sous le siège passager" }   ← LA réponse du chauffeur
```

**Ouvrir une demande** : `category` ∈ `ride` (un problème pendant une course
— `ride_id` obligatoire, et seulement une course que **vous** avez conduite,
`403` sinon) · `payment` · `tokens` (jetons, commission, dette) · `account` ·
`behaviour` (un passager) · `other`. Le ticket rendu porte une `reference`
(`TCK-000123`) à afficher, un `status` (`open` · `in_progress` · `waiting` ·
`resolved` · `closed`) et un fil `messages[]` où `author_role` dit qui parle
(`driver` vous, `admin` le support, `client` le passager sur un objet perdu).
Le support répond : vous recevez **`ticket_reply`**, puis
**`ticket_resolved`** à la clôture — catégorie `support`, non coupable.

### 🎒 Un passager a oublié quelque chose dans votre véhicule

C'est le seul cas où un ticket **vient à vous** sans que vous l'ayez ouvert.

1. Le passager déclare l'objet depuis son application, sur **votre** course.
   Vous recevez à l'instant **`lost_item_reported`** — « Objet oublié dans
   votre véhicule · Un passager a oublié : Sac à dos noir (Lomé Centre →
   Aéroport · 19/09 14:02). Vérifiez votre véhicule et répondez depuis
   l'application » — données `{ type: "lost_item", ticket_id, ride_id }`.
   **Ouvrez le ticket dessus** : `GET /tickets/{id}` rend `lost_item.item`,
   `lost_item.details`, et le fil.
2. Le ticket apparaît aussi dans `GET /tickets` : vous en êtes partie
   (`counterpart_id` = vous), même si vous ne l'avez pas ouvert.
3. **Répondez** — c'est le geste que l'écran doit rendre évident, deux
   boutons : `POST /tickets/{id}/lost-item { "found": true, "note": "…" }` ou
   `{ "found": false }`. La note est facultative (où l'objet était, où vous
   êtes). Réservé au chauffeur de la course : un autre chauffeur reçoit `403`,
   un ticket qui n'est pas un objet perdu `409 not_a_lost_item`.
4. Le passager est prévenu de votre réponse. **Trouvé** : le ticket passe
   `in_progress` et **le support vous contacte pour organiser la
   restitution** — vous n'avez pas à joindre le passager, et le fil du ticket
   n'est pas là pour échanger des numéros. **Pas trouvé** : le ticket reste
   ouvert côté support ; vous pouvez répondre à nouveau plus tard si l'objet
   réapparaît.
5. `lost_item.found` reste `null` tant que vous n'avez pas répondu : c'est
   l'état « à traiter » à mettre en évidence dans votre liste.

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
>
> Une ligne **`kind: "equipment"`** (v4.11.0) est une **retenue pour le
> matériel** (gilet, casque, téléphone vendu ou loué par Dira — §6 bis) :
> négative quand on retient sur une course, positive quand on vous rend une
> caution. `reason` nomme l'article (« matériel : Casque Dira »).

**Affichez la dette en permanence**, pas dans un écran caché. Un chauffeur qui
cesse de recevoir des courses sans comprendre pourquoi croit à une panne.

`owing` et `over_limit` sont **rendus calculés** : le signe d'un nombre se lit
mal en un coup d'œil, et « dois-je de l'argent ? » ne doit pas dépendre d'une
comparaison que chaque application refait à sa façon.

---

## 6 bis. Le MATÉRIEL — gilet, casque, téléphone (v4.11.0) — **SOCLE** (sans `/vtc`)

> Dira vend, loue ou prête du matériel à ses chauffeurs. Le contrat, l'échéancier
> et les paiements sont tenus par le **socle** (`…/api/v1/`, pas `/vtc`) ; ce
> que vous devez est **retenu sur vos gains** et apparaît dans votre relevé
> (`kind: "equipment"`, §6).

```
GET  /equipment/catalogue?vertical=vtc      → { items: [ … ] }        ce que Dira propose dans votre pays
POST /equipment/requests                    { item_id, mode, quantity?, note? }   → 201 contrat `requested`
GET  /me/equipment                          → { items: [ contrats ], standing }
POST /me/equipment/{id}/accept              → le contrat `accepted`
POST /me/equipment/{id}/pay                 { amount_xof }   ← ⚠️ 409 `equipment_no_wallet` pour un chauffeur (voir plus bas)
```

**`vertical=vtc` est obligatoire** sur le catalogue et sur une demande : un
article peut être réservé aux livreurs ou aux chauffeurs (`audiences`), et le
socle ne devine pas depuis quelle application vous parlez. Rôle `driver`
exigé partout ; `X-Dira-Country` fixe le pays du catalogue et des réglages.

### Le catalogue

```json
{ "id": "…", "kind": "vest", "name": "Gilet réfléchissant Dira", "description": "…",
  "photos": [ "https://…" ], "audiences": [ "vtc", "food" ],
  "sale_price_xof": 6000, "rental_daily_xof": 0, "rental_weekly_xof": 500, "rental_monthly_xof": 1500,
  "deposit_xof": 2000, "stock": 12, "track_stock": true, "active": true }
```

`kind` ∈ `vest` · `bag` · `phone` · `helmet` · `box` · `other`. Un prix à `0`
= **pas proposé sous ce mode** : un article dont `sale_price_xof` vaut `0`
ne s'achète pas, un article sans loyer ne se loue pas ; n'affichez que les
modes possibles. `deposit_xof` est la caution demandée à la remise (rendue
au retour si le contrat le prévoit). `stock` n'a de sens que si
`track_stock` est vrai ; à `0`, l'article s'affiche mais la remise attendra.

### Demander un article — quand le pays l'autorise

`POST /equipment/requests { "item_id", "mode": "sale" | "rental" | "loan", "quantity"?, "note"? }`.
Réponses à prévoir : `403 equipment_requests_closed` (le pays n'ouvre pas les
demandes depuis l'application — **cachez le bouton, ne le grisez pas**, le
chauffeur passera par l'agence), `409 equipment_mode_not_allowed` (mode
fermé dans ce pays), `409 equipment_not_offered` (article sans prix sous ce
mode), `404` (article inactif ou réservé à l'autre verticale). Le contrat
rendu est `requested` : **rien n'est dû** tant que l'exploitation ne l'a pas
qualifié puis remis.

### Vos contrats — `GET /me/equipment`

```json
{ "items": [ {
    "id": "…", "vertical": "vtc", "item_id": "…", "item_name": "Casque Dira", "item_kind": "helmet",
    "quantity": 1, "serial": "HD-0042", "mode": "sale", "status": "active",
    "price_xof": 15000, "deposit_xof": 5000,
    "paid_xof": 5300, "outstanding_xof": 14700, "due_xof": 0, "overdue_since": null, "blocked": false,
    "plan": { "schedule": "installments", "installments": 4, "period": "weekly", "first_due_days": 7,
              "collect_from_earnings": true, "collect_from_wallet": false, "allow_partial": true,
              "earnings_percent": 15, "earnings_fixed_xof": 0, "min_left_xof": 1000,
              "daily_cap_xof": 0, "weekly_cap_xof": 0, "earnings_only_when_due": false,
              "grace_days": 3, "late_fee_xof": 0, "late_fee_percent": 0, "block_after_days": 10,
              "reminder_days": 2, "deposit_refundable": true },
    "schedule": [
      { "n": 1, "kind": "deposit",     "due_at": "…", "amount_xof": 5000, "late_fee_xof": 0, "paid_xof": 5000, "owed_xof": 0,    "paid_at": "…", "status": "paid" },
      { "n": 2, "kind": "installment", "due_at": "…", "amount_xof": 3750, "late_fee_xof": 0, "paid_xof": 300,  "owed_xof": 3450, "status": "pending" },
      { "n": 3, "kind": "installment", "due_at": "…", "amount_xof": 3750, "late_fee_xof": 0, "paid_xof": 0,    "owed_xof": 3750, "status": "pending" } ],
    "payments": [
      { "id": "…", "at": "…", "amount_xof": 5000, "source": "manual", "note": "espèces à l'agence" },
      { "id": "…", "at": "…", "amount_xof": 300,  "source": "ledger", "ref_kind": "ride", "ref_id": "…" } ],
    "next_period_at": null, "notes": "", "requested_at": null, "accepted_at": "…", "handed_at": "…",
    "returned_at": null, "closed_at": null, "created_at": "…", "updated_at": "…" } ],
  "standing": { "contracts": 1, "outstanding_xof": 14700, "due_xof": 0, "overdue_xof": 0, "blocked": false } }
```

**Les statuts** : `requested` (vous l'avez demandé) → `draft` (Dira vous le
propose : **à accepter**) → `accepted` → `active` (remis, l'échéancier
court) → `returned` (rendu, il peut rester à payer) → `completed` ;
`cancelled` avant remise ; `defaulted` = contentieux (à afficher tel quel,
sans bouton).

**Les modes** : `sale` — l'article est à vous une fois payé, `price_xof` est
le **total** ; `rental` — `price_xof` est le **loyer par période**
(`plan.period` : `daily` · `weekly` · `biweekly` · `monthly`), une ligne
`period` s'ajoute à chaque échéance jusqu'au retour, `next_period_at` dit
quand ; `loan` — rien n'est dû hors caution et dégâts éventuels.

**Les quatre totaux sont calculés par le serveur, ne les recalculez pas** :
`paid_xof` (tout ce qui a été réglé, caution comprise), `outstanding_xof`
(tout ce qui reste, échu ou non, pénalités comprises), `due_xof` (ce qui
est **échu** aujourd'hui), `overdue_since` (depuis quand la plus ancienne
échéance est en retard, `null` sinon), `blocked` (voir plus bas).
`standing` les additionne sur tous vos contrats : **c'est le bandeau à
afficher**, un chauffeur ne doit pas additionner ses contrats à la main.

Chaque ligne de `schedule` porte `kind` (`deposit` la caution, due à la
remise · `installment` une part du prix · `period` un loyer · `damage` des
dégâts constatés au retour), `status` (`pending` · `due` · `overdue` ·
`paid` · `waived` — remise gracieuse), `late_fee_xof` (pénalité de retard,
ajoutée **une fois** quand le délai de grâce est dépassé), `owed_xof` ce qui
reste sur la ligne.

### Accepter

Un contrat `draft` vous est notifié (**`equipment_contract_proposed`**) :
affichez l'article, le mode, le prix, la caution, **l'échéancier** et
surtout **la retenue sur gains** (`plan.earnings_percent` % de chaque course
et/ou `earnings_fixed_xof` par course, jamais au-delà de ce qui est dû, en
laissant au moins `min_left_xof` sur chaque gain), puis un bouton
`POST /me/equipment/{id}/accept`. Autre statut : `409 equipment_bad_status`.
Pas de refus depuis l'application : on n'accepte pas, et l'exploitation
annule. Après acceptation, **rien ne démarre avant la remise physique** ;
`handed_at` et **`equipment_handed_over`** marquent le départ.

### Comment vous payez — la RETENUE sur le relevé

Un chauffeur n'a pas de solde Dira à débiter : ce qu'il doit se règle **sur le
relevé de courses** (§6). À chaque course réglée, le socle calcule la retenue
(`plan`), et le service des courses inscrit une ligne **`kind: "equipment"`,
`amount_xof` négatif, `ride_id` de la course** dans `GET /drivers/me/statement`
— exactement comme une commission. Le contrat enregistre un paiement
`source: "ledger"` avec `ref_kind: "ride"`, et vous recevez
**`equipment_charged`** (« 300 F ont été pris sur votre relevé pour Casque
Dira »). **Les retenues font partie de la dette** : elles pèsent sur
`balance_xof` et donc sur `max_debt_xof`.

Un règlement direct reste possible **hors application** : espèces ou mobile
money à l'agence, enregistrés par l'exploitation (`source: manual` ·
`mobile_money`), ou une remise gracieuse (`waiver`). `POST /me/equipment/{id}/pay`
répond **`409 equipment_no_wallet`** pour un chauffeur : **ne proposez pas
ce bouton** dans l'application chauffeur (il sert aux livreurs, qui ont un
solde).

Le loyer (`rental`) se retient de la même façon ; quand une période s'ouvre
sans course, la ligne attend (`pending` → `due` → `overdue`).

### Le retard — et le BLOCAGE

- `reminder_days` jours avant une échéance : **`equipment_due`**.
- Échéance dépassée + `grace_days` : la ligne passe `overdue`, la pénalité
  (`late_fee_xof` et/ou `late_fee_percent` %) s'ajoute une fois, vous recevez
  **`equipment_overdue`**.
- Retard de plus de `block_after_days` jours (si > 0) : `blocked: true`,
  **`equipment_blocked`**, et **`PATCH /drivers/me/online { online: true }`
  répond `402 equipment_overdue`** — au même titre que `402 debt_over_limit`.
  Le chauffeur reste libre de terminer une course en cours. L'écran de mise
  en ligne doit dire **pourquoi** et renvoyer vers l'écran du matériel :
  `standing.blocked` et, contrat par contrat, `overdue_since` + `due_xof`.
  Le blocage se lève dès que plus rien n'est en retard (règlement à
  l'agence, remise gracieuse, ou retenue sur une course terminée).

### Le retour

L'exploitation enregistre le retour : le contrat passe `returned`,
`return_condition` décrit l'état, une ligne `damage` apparaît si des dégâts
sont facturés, et la **caution** est rendue si `plan.deposit_refundable`
(moins les dégâts). Pour un chauffeur, ce remboursement est un paiement
**négatif, `pending: true`** dans `payments[]` — il sera compensé sur le
relevé à la prochaine course (ligne `equipment` **positive**) ou versé à
l'agence. Vous recevez **`equipment_returned`** (« Caution rendue : 5000 F.
Reste dû : 0 F »). Un contrat rendu où rien ne reste dû passe `completed`.

### Notifications (catégorie `support` — non coupable)

| Clé | Quand | Données |
|---|---|---|
| `equipment_contract_proposed` | un contrat `draft` attend votre accord | `{ type: "equipment", contract_id, vertical }` |
| `equipment_handed_over` | remise faite, échéancier lancé | idem |
| `equipment_due` | `reminder_days` avant une échéance | idem |
| `equipment_charged` | une retenue ou un prélèvement a eu lieu | idem |
| `equipment_overdue` | une échéance est en retard | idem |
| `equipment_blocked` | mise en ligne bloquée | idem |
| `equipment_returned` | retour enregistré, caution rendue | idem |

`data.contract_id` ouvre le contrat ; toutes viennent avec `data.type: "equipment"`.

---

## 7. Ce que le SOCLE sert (sans `/vtc`)

| | |
|---|---|
| `POST /auth/login` · `/auth/refresh` · `/auth/logout` | la session |
| `GET · PATCH /me` | le profil |
| `PATCH /me/preferences` | `locale` ∈ `fr` · `en` (autre : 422), `theme` |
| `POST /uploads?kind=vehicle` · `?kind=avatar` | les photos — **v3.0.0** : les courses n'avaient **aucune** porte d'envoi, c'est désormais celle du socle, pour tout le monde |
| `GET /wallet` · `/wallet/transactions` | le portefeuille Dira |
| `GET /equipment/catalogue` · `GET /me/equipment` · `POST /me/equipment/{id}/accept` | le **matériel** vendu, loué ou prêté par Dira — §6 bis (v4.11.0) |
| `GET /me/notifications` · `POST /me/devices` | les notifications |
| `GET /agents/{id}/ratings` | vos avis — `id` = votre **profil** (`GET /drivers/me` → `id`) |

- **Téléphone en E.164 avec le `+`** ; sans indicatif, `422` avec `fields: ["phone"]`. `account_suspended` (403) à la connexion : le dire tel quel.
- **Durées de vie des jetons — v3.1.0.** Access token **15 min** (staging : **10 min**), refresh token **30 jours**, consommé à la rotation (le rejouer → 401 → revenir à la connexion). ⚠️ Le socket du suivi est ouvert avec l'access token et **vit plus longtemps que lui** : à l'échéance, le suivi le ferme avec le code **4401 `token_expired`** — rafraîchir (`POST /auth/refresh`) **puis** reconnecter, jamais reconnecter avec le même jeton. Poignée de main : **401** = rafraîchir et revenir ; **403** = ce rôle ne peut pas pousser de positions, ne pas réessayer. L'ancien access token reste valide jusqu'à son échéance après une rotation : un court chevauchement socket / REST est normal.
- **Un `422` nomme ses champs** (`fields`, `reason`) — §1 bis.

### Le compte — la photo, le nom d'affichage, le prénom et le nom (v4.8.1)

`GET /me` rend ce que l'app affiche du compte :

```json
{ "id": "…", "phone": "+22891000101", "name": "Kossi Amegan",
  "avatar_url": "https://files.dira.llc/dira-media-staging/avatar/e63ea33e-….jpg",
  "role": "driver", "status": "active", "country": "TG", "created_at": "…",
  "preferences": { … } }
```

- **`avatar_url` est LA photo du compte.** Affichez-la partout où l'app dessine
  un rond d'initiale — accueil, compte, profil — avec l'initiale de `name` en
  repli quand le champ manque, et dessous le temps du chargement. ⚠️ C'est le
  même champ que l'administration renseigne : un chauffeur dont la console
  montre la photo et l'app une initiale n'a pas deux photos, il a une app qui
  ne lit pas le champ.
- **`name` est le nom d'AFFICHAGE ; `first_name` / `last_name` sont l'état
  civil.** Les trois sont indépendants. Un compte créé à l'inscription, au
  téléphone ou par l'administration ne porte que `name` : `first_name` et
  `last_name` sont **absents de la réponse** tant que la personne ne les a
  pas saisis — c'est l'état normal, pas une donnée perdue ni un mélange. Ne
  les déduisez **jamais** de `name` (« Diallo Ndiaye » a deux prénoms), et
  n'affichez pas leur concaténation là où `name` existe. Dans le formulaire
  de profil : trois cases, pré-remplies avec ce que `GET /me` rend, vides
  sinon.
- **Changer la photo, en deux étapes** : `POST /uploads?kind=avatar`
  (multipart, champ `file`, jpeg · png · webp, ≤ 5 MiB) → `201 { url }`, puis
  `PATCH /me { "avatar_url": url }` → la réponse est le compte à jour, à
  garder en session. Deux étapes pour qu'un envoi qui échoue ne fasse pas
  perdre le reste de la saisie.
- **`PATCH /me` n'envoie que ce qui change** :
  `{ name?, first_name?, last_name?, birth_date?, gender?, email?, avatar_url? }`.
  Une clé inconnue → `422 reason: unknown_field`. Quand `name` n'a jamais
  été posé (vide, ou égal au téléphone), le serveur le fait suivre
  `first_name + last_name` — dans tous les autres cas il ne bouge que si
  l'app l'envoie.

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
