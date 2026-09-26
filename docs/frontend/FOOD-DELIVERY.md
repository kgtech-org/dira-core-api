# App LIVREUR — LIVRAISON — contrat d'API

> **Version 4.29.0** · 26 septembre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Livraison : `https://api-staging.dira.llc/api/v1/food` · Suivi : `wss://tracking-staging.dira.llc` · SIG : `https://maps.dira.llc/api`


> ## ⚠️ v2.0.0 — LES ROUTES CHANGENT DE BASE
>
> La plateforme s'est scindée en un **socle** partagé et des **verticales**.
> Une seule chose change pour une application, mais elle change partout :
>
> | Ce que vous appelez | Où c'est servi | Base |
> |---|---|---|
> | connexion, profil, adresses, **envoi de fichiers**, portefeuille, paiements, notifications, avis | **socle** (`dira-core-api`) | `…/api/v1/…` — **inchangé** |
> | tout le reste de ce document | **livraison** (`dira-food-api`) | `…/api/v1/food/…` — **nouveau préfixe** |
>
> Concrètement : `POST /api/v1/auth/login` ne bouge pas, `GET /api/v1/food/orders`
> remplace `GET /api/v1/orders`. **Une seule variable de configuration ne suffit
> plus** : prévoyez-en deux, `API_BASE` et `FOOD_BASE`.
>
> Pourquoi : les **courses** (VTC) arrivent, sur la même identité et le même
> portefeuille. Se connecter et payer sont le même geste quel que soit le
> service ; les dupliquer aurait donné deux annuaires, deux soldes, et une
> suspension qui ne vaut que d'un côté.
>
> Le **jeton reste le même** des deux côtés — même secret, même session. Vous ne
> vous connectez pas deux fois.

---

## 1. Quatre back-ends, deux sockets

Ce parcours parle à **quatre serveurs** depuis la v2.0.0 — le socle et la livraison se sont séparés. Les bases d'URL doivent être **configurables indépendamment** : une seule variable rendrait impossible d'en déplacer un.

| | Rôle |
|---|---|
| **Socle** (`dira-core-api`) | connexion, profil, **portefeuille**, paiements, **matériel**, notifications, avis reçus — base `…/api/v1/` |
| **Livraison** (`dira-food-api`) | véhicules, courses, collectes, conversation, conformité — base `…/api/v1/food/`, REST **et** WebSocket |
| **Suivi** (`dira-tracking`) | émission des positions GPS, **réception des appels de course** |
| **SIG** (`dira-maps`) | itinéraire routier, géocodage inverse — **du JSON, aucune vue de carte** |

> ⚠️ **Deux sockets, pas un.** Celui du suivi émet des positions en continu ; celui de l'API reçoit les messages du client. Autres hôtes, autres cycles de vie. **Ne les factorisez pas** derrière une seule abstraction.

> **dira-maps ne fournit aucune vue de carte.** Le fond de carte vient du composant natif du téléphone. Passer par une `WebView` est à proscrire.

---

## 1 bis. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` — le même jeton pour le socle et pour la livraison |
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

## 2. Compte et véhicules

> Le **compte** (`/me`, `/auth/*`) est au **socle**, base `…/api/v1/` sans `/food`. Les **véhicules**, eux, sont à la livraison : `…/api/v1/food/agent/vehicles`.

```
POST   /auth/register       { phone (E.164, avec le +), name, password, role: "driver", first_name?, last_name? }
POST   /auth/login          { phone | email, password, app: "driver" }   ⚠️ v4.26.0
GET    /me · PATCH /me · PATCH /me/preferences
POST   /agent/vehicles      { type, brand?, model?, license_plate?, color?, photo_url?, images?, capacity? }
GET    /agent/vehicles
PATCH  /agent/vehicles/{id}
PATCH  /agent/active-vehicle   { vehicle_id }
PATCH  /agent/availability     { available }
```

- ⚠️ **Le téléphone porte son indicatif** : `+22890100001`. Sans `+`, `422` avec `fields: ["phone"]` — le socle ne devine pas de pays. Espaces et tirets tolérés. `phone_taken` (409) : le numéro a déjà un compte → proposer la connexion.
- **Un `422` nomme ses champs — v3.0.0.** `fields` liste les clés JSON en cause : soulignez ces cases. `reason: unknown_field` signifie que l'app envoie une clé que la route ne connaît pas — c'est refusé, pas ignoré, et c'est un bug à corriger côté app. `first_name` / `last_name` sont désormais **acceptés** à l'inscription.
- **`account_suspended` (403)** à la connexion : le compte est suspendu, le mot de passe est bon — le dire tel quel.

### 🚪 DIRE QUELLE APPLICATION SE CONNECTE — `app` (v4.26.0)

`POST /auth/login` accepte un champ `app`. **Envoyez `app: "driver"` à chaque
connexion**, à côté du téléphone et du mot de passe :

```
POST /auth/login   { phone | email, password, app: "driver" }
```

⚠️ **Ce qui se passait sans lui** : un client se connectait ici, et rien ne
l'arrêtait. Le mot de passe est bon, le jeton est émis, l'accueil s'ouvre —
puis chaque écran répond `403`, et la personne conclut que l'application est
cassée au lieu de comprendre qu'elle s'est trompée d'application. C'est du
support pour rien, et une mauvaise première impression.

Le socle refuse désormais, **`403 wrong_app`**, et le refus DIT OÙ ALLER :

| champ | ce qu'il porte |
|---|---|
| `error.code` | `wrong_app` |
| `error.reason` | **l'application à ouvrir** : `client` · `driver` · `merchant` · `console` |
| `error.message` | la phrase déjà traduite, à afficher telle quelle si vous n'avez pas la vôtre |

⚠️ **`reason`, et rien d'autre.** L'enveloppe d'erreur du socle ne rend que
`code`, `message`, `fields` et `reason` — il n'y a pas de `meta` sur le fil.
C'est écrit ici parce qu'une première rédaction de cette section promettait
un `meta.open_instead` qui n'existe pas, et l'erreur se serait découverte à
l'intégration.

Affichez « Ce compte est un compte client. Ouvrez l'application Dira
(client). » — **jamais « identifiants invalides »** : le mot de passe était
juste, et envoyer la personne changer un mot de passe correct ne mène nulle
part.

**Ce que la règle NE sépare PAS.** Elle sépare des FAMILLES de comptes, pas
des applications. `driver` vaut pour le chauffeur VTC **et** le livreur —
même rôle au socle ; `client` vaut pour la course **et** la livraison. Deux
applications de la même famille ne se distinguent donc pas l'une de l'autre
à la connexion. La famille `driver`, elle, est bien tenue à l'écart des
autres.

**Un compte de DIRECTION entre partout.** C'est voulu, pas un trou :
l'exploitation ouvre votre application pour reproduire ce qu'un utilisateur
décrit au support.

**`app` omis ne vérifie rien.** Une version d'application pas encore mise à
jour continue donc de fonctionner exactement comme avant — mais le mauvais
compte y entre aussi comme avant. C'est la raison d'envoyer le champ dès
cette version.

- `type` ∈ `moto` · `velo` · `voiture` · `pieton` · `tricycle`. `capacity` = courses simultanées.
- **Couleur, description, photos (v4.10.0).** Chaque véhicule rendu porte
  **`description`**, GÉNÉRÉE — *marque modèle couleur* (« Yamaha Crypton
  rouge » ; sans marque ni modèle, le type parle : « moto rouge ») : à
  afficher telle quelle, jamais à saisir. `color` se propose au formulaire
  avec la marque et le modèle. **`images[]`** : toutes les photos (**8 au
  plus**, jamais `null`), chacune téléversée d'abord par
  `POST /uploads?kind=vehicle` → `{ url }` ; **`photo_url`** est la
  couverture (celle choisie, sinon la première). En `PATCH`, `images`
  **remplace** la galerie.

### Le compte — la photo, le nom d'affichage, le prénom et le nom (v4.8.1)

`GET /me` rend ce que l'app affiche du compte :

```json
{ "id": "…", "phone": "+22890100001", "name": "Koffi Mensah",
  "avatar_url": "https://files.dira.llc/dira-media-staging/avatar/019d7a38-….jpg",
  "role": "driver", "status": "active", "country": "TG", "created_at": "…",
  "preferences": { … } }
```

- **`avatar_url` est LA photo du compte.** Affichez-la partout où l'app dessine
  un rond d'initiale — accueil, compte, profil — avec l'initiale de `name` en
  repli quand le champ manque, et dessous le temps du chargement. ⚠️ C'est le
  même champ que l'administration renseigne : un livreur dont la console
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
- Photo : téléverser d'abord (`POST /api/v1/uploads?kind=vehicle&entity={id}` — au **socle**, sans `/food` : la même porte sert les courses), rattacher l'URL par `PATCH`. L'étape est séparée pour qu'une photo qui échoue ne fasse pas perdre la saisie.
- **Mettre hors service ≠ supprimer** : `PATCH { "is_active": false }`. Un véhicule ayant servi doit rester lisible dans l'historique.
- **Le véhicule actif porte le GPS**, c'est lui que le client voit avancer, et lui qui est enregistré sur la course.

> Sans véhicule actif, **accepter est refusé** (`409 no_active_vehicle`). Dites-le **avant** que le livreur tente d'accepter : à ce moment-là il court déjà vers une course qu'il ne peut pas prendre.

**Disponibilité** : se retirer **n'arrête pas le GPS**. Le livreur rentre chez lui, ou termine la course en main, tout en refusant les suivantes. C'est pourquoi la disponibilité se déclare et ne se déduit pas du flux de positions.

> ⚠️ **HORS SERVICE VEUT DIRE AUCUN APPEL — y compris par le véhicule
> (v4.26.0).** L'attribution juge deux choses : le COMPTE et le VÉHICULE.
> Le compte était bien filtré ; le véhicule, lui, ne regardait que son état
> mécanique (`maintenance`), jamais la disponibilité de son propriétaire.
> Quand une vague ne nommait que des véhicules, un livreur retiré pouvait
> donc encore sonner. Le véhicule interroge désormais son livreur : hors
> service ou suspendu, il est écarté aussi de cet axe. Si un livreur
> indisponible reçoit encore une commande, c'est un bug à signaler, plus une
> tolérance.


> ⚠️ **LA PRÉSENCE — v4.7.0, la même règle que les chauffeurs VTC.** Un
> livreur disponible qui **n'émet plus sa position pendant cinq minutes**
> est **retiré du service par le serveur** (`available: false`,
> `offline_reason: "stale"`) : il doit se redéclarer disponible. La réponse
> de `PATCH /agent/availability` porte la présence :
>
> | Champ | Sens |
> |---|---|
> | `last_seen_at` | dernière position de son véhicule vue par le suivi |
> | `tracking_stale` | disponible mais **rien émis depuis plus de deux minutes** : le suivi ne le propose plus — affichez **« suivi arrêté »**, c'est la raison n°1 d'un téléphone qui ne sonne pas |
> | `is_online` | disponible **et** vu — appelable |
> | `offline_at` · `offline_reason` | depuis quand et pourquoi il n'est plus en service : `driver` (lui), `stale` (silence), `admin` (l'exploitation) |
>
> Ce que l'app doit faire : garder le socket de suivi ouvert et **émettre
> dès la disponibilité**, pas seulement en course ; sur `tracking_stale`,
> le dire au livreur ; sur un retrait `stale` (`available: false` alors
> qu'il ne l'a pas demandé), proposer de se redéclarer d'un geste.
>
> **`402 equipment_overdue`** (v4.11.0) : la prise de service est refusée
> parce qu'une échéance de **matériel** (gilet, sac, téléphone — §8 bis) est
> en retard au-delà du seuil du contrat. Dites-le et ouvrez l'écran du
> matériel, avec le bouton payer.
>
> **`402 debt_over_limit`** (v4.12.0, mode commission) : la **dette** du
> livreur — des commissions sur des commandes en espèces que son solde n'a
> pas couvertes, `GET /wallet` → `debt_xof` — dépasse le plafond du pays.
> Elle se rembourse d'office sur son prochain gain en ligne, ou à l'agence.

---

## 3. L'APPEL — la course vient au livreur

Quand le marchand valide sa préparation, les livreurs libres autour de la
**première collecte** sont appelés. Sans preneur, la course part en urgence
côté back-office et **retourne au pot commun**.

> **Comment on est appelé — v4.7.0.** C'est un **réglage d'exploitation**,
> le même que pour les chauffeurs VTC, pas une règle de l'application : soit
> **tous** les livreurs libres dans le rayon sont appelés en même temps (le
> premier qui accepte l'emporte), soit **un seul** à la fois, du plus proche
> au suivant dès un refus ou l'échéance. Par défaut : tous à 5 km, 30 s
> chacun, trois vagues ou cinq minutes. Dans les deux cas : **un livreur en
> cours d'appel n'est jamais appelé pour une autre course** — un seul écran
> d'appel à la fois, jamais remplacé par un second ; **personne n'est
> rappelé** sur la même course ; le compte à rebours (`expires_at`) est
> celui du serveur et peut être **plus court** que d'habitude quand la
> recherche touche à sa fin. Refuser explicitement vaut mieux que laisser
> passer : en mode « un par un », c'est ce qui fait sonner le suivant tout
> de suite. `GET /food/settings/dispatch` rend le réglage en vigueur
> (`call_mode`, `call_radius_m`, `call_ttl_s`, …) si l'écran veut le dire.

> **Qui sonne — v4.7.0.** Avant de faire sonner qui que ce soit, le serveur
> écarte de la vague ceux qui ne peuvent pas prendre : livreur **retiré du
> service** ou suspendu, sans véhicule au volant, et **véhicule immobilisé
> par l'exploitation** — c'est le véhicule qui **émet** qui est jugé, pas
> celui déclaré actif dans l'app. Un livreur écarté ne voit rien ; il n'a
> rien à faire.

> **v3.7.0 — elle n'y reste pas indéfiniment.** Quinze minutes après la
> validation du marchand, une course que personne n'a prise **expire** :
> elle disparaît de `/deliveries/available`, et l'accepter répond
> `409 delivery_expired`. Elle revient si le marchand relance (nouvel
> appel). Une course vue dans la liste puis refusée à l'acceptation n'est
> donc pas un bug : rafraîchir la liste.

> **v3.7.1 — et elle n'y entre qu'une fois le repas prêt.** Une commande
> en préparation n'est pas dans `/deliveries/available` ; l'accepter
> répondrait `409 order_not_ready`. La liste ne montre que ce qui est à
> prendre maintenant — pas ce qu'on attendrait au comptoir.

L'appel arrive sur le socket de suivi **déjà ouvert**, en sens inverse des positions :

```jsonc
{ "type": "call", "call_id": "…", "ref": "<delivery_id>", "attempt": 1,
  "expires_at": 1757…,
  "pickup": { "lng": …, "lat": … }, "dropoff": { "lng": …, "lat": … },
  "meta": { "token_cost": 3, "cash_required_xof": 4200,
            "planned_distance_m": 5400, "pickups": 2, "dropoff_address": "…" } }

{ "type": "call_closed", "call_id": "…", "ref": "…", "reason": "taken" | "cancelled" | "exhausted" }
```

Répondre — avec le **JWT du livreur**, sur le service de **suivi** :

```
POST {tracking}/track/calls/{call_id}/accept    { "vehicle_id": "…" }
POST {tracking}/track/calls/{call_id}/decline
```

- **`meta` porte tout ce qu'il faut pour décider.** L'écran d'appel ne doit déclencher **aucune requête** avant de s'afficher : trente secondes ne laissent pas le temps d'un aller-retour de plus sur un réseau mobile.
- **Le compte à rebours est celui du serveur.** Rendez le temps restant depuis `expires_at` — une application mise en arrière-plan ne doit pas pouvoir le rallonger.
- **Refuser explicitement vaut mieux que laisser expirer** : quand tous les appelés refusent, la vague suivante part sans attendre les 30 s. Le bouton « refuser » fait avancer la commande.
- **`call_closed` ferme l'écran.** Sans lui, le compte à rebours continuerait sur une course déjà partie et le livreur accepterait dans le vide.
- `accept` rend les refus métier tels quels (§5).

> ⚠️ **Être appelé suppose d'émettre sa position.** Les candidats sortent de l'index géospatial du suivi : un livreur qui n'a jamais poussé de position n'est appelé par personne. C'est la raison d'ouvrir le socket **dès l'entrée dans le parcours**, pas seulement en course.

**Pendant un appel, la course est RÉSERVÉE** : elle disparaît de `/deliveries/available` pour les autres, et l'accepter leur renvoie `409 not_called`.

---

## 4. Ce qu'une course coûte, et ce qu'elle exige

```
GET /deliveries/available?limit=&cursor=
```

⚠️ **N'envoyez ni `lat` ni `lng`** : cette route n'accepte aucun paramètre de position. Le serveur trie lui-même par proximité, depuis la **dernière position connue du véhicule actif**. Conséquence : un livreur qui n'a jamais émis obtient une liste **non triée**.

**Deux chiffres à afficher AVANT le bouton « accepter »**, jamais après :

| Champ | Ce que c'est |
|---|---|
| `token_cost` | le prix d'entrée de **cette** course, en jetons — **variable** selon le parcours |
| `cash_required_xof` | ce que le livreur doit **avoir sur lui** — zéro si la commande est prépayée |

> ⚠️ **Rien ne vérifie côté serveur que le livreur a l'argent.** C'est une condition d'utilisation : il voit le montant, il accepte en connaissance de cause. L'écran d'acceptation est donc le **seul** endroit où cela se joue — sinon il découvre la somme devant le restaurant.

> **Le livreur ne doit RIEN à la plateforme.** Elle se rémunère par les jetons et ne prend aucune part des frais de livraison. Aucun écran ne doit suggérer une dette.

Les `pickups` sont **omis des listes** : ils n'arrivent qu'au détail. Nombre de collectes, distance prévue et destination suffisent à décider.

### Combien de temps — v1.3.0

`planned_duration_s` est la durée de la tournée **par le réseau routier** (dira-maps), et non à vol d'oiseau. C'est ce qu'il faut afficher à côté de `planned_distance_m` : cinq kilomètres ne veulent pas dire la même chose selon qu'on traverse le marché ou qu'on longe le boulevard.

Elle est calculée **une fois, à la création**, et figée sur la course : la redemander à chaque affichage ferait un appel externe par ouverture d'écran, et la durée changerait sous les yeux du livreur sans qu'il ait bougé.

> **Absente quand le SIG n'a pas répondu.** La course se crée sans durée plutôt que pas du tout : n'affichez rien, et ne remplacez pas par une estimation maison — une durée devinée serait indiscernable d'une durée calculée.

---

## 5. Accepter, collecter, terminer

```
POST /deliveries/{id}/accept
POST /deliveries/{id}/pickups/{pickup_id}/done
POST /deliveries/{id}/complete
GET  /deliveries/{id}
GET  /deliveries?status=&limit=&cursor=     # VOS courses, les plus récentes d'abord (v4.12.1)
```

**`GET /deliveries`** rend les courses qui vous ont été **attribuées** — en
cours et passées — triées par date de création, la dernière en tête ;
`?cursor=` (l'identifiant de la dernière ligne reçue) rend la page suivante,
plus ancienne. `?status=` prend plusieurs statuts séparés par des virgules
(`accepted,picking_up,in_transit` = l'onglet « en cours »,
`completed,cancelled` = l'historique) ; un statut inconnu est **refusé**.
Chaque ligne a la forme de `/deliveries/available` (sans le détail des
collectes ni le client, mais **avec `pickups_count`**) : ouvrez
`GET /deliveries/{id}` pour la course active. C'est aussi
par là qu'une application **retrouve sa course en cours** après un
redémarrage — plus besoin d'en garder l'identifiant en local.

| Réponse | Signification | Conduite |
|---|---|---|
| `200` | course attribuée | ouvrir la course active |
| `402 insufficient_tokens` | solde vide | proposer la recharge ; la course **reste disponible** |
| `409 no_active_vehicle` | aucun véhicule actif | renvoyer aux véhicules |
| `409 driver_at_capacity` | déjà autant de courses que le véhicule en porte | **l'annoncer avant**, griser les courses inaccessibles |
| `409 not_called` | course réservée aux appelés | ne devrait pas arriver — rafraîchir plutôt qu'insister |
| `409 delivery_not_available` | prise par un autre | retirer de la liste, **rafraîchir le solde** : le jeton est remboursé |

> **`402` est un `402`, pas un `409`.** Une application qui filtre sur le mauvais statut proposera une recharge au mauvais moment.

> **`delivery_not_available` arrive après le débit — et le jeton revient**, par un mouvement `order_accept_refund` visible dans `GET /wallet/transactions`. Rafraîchissez le solde : il a bougé deux fois. Si le remboursement échoue, seul le grand livre le montre — renvoyez vers l'historique plutôt que d'affirmer « aucun jeton débité ».

### ⚠️ UNE COURSE, PLUSIEURS COLLECTES — la tournée (v4.19.0)

Une commande peut être passée chez **plusieurs enseignes**. La course porte
alors **autant de points de collecte que de boutiques**, à parcourir **dans
l'ordre**, avant le dépôt unique chez le client.

> ⚠️ **CONSTATÉ EN RECETTE, ET C'EST LE PIÈGE.** Dans la **liste** des
> courses, `pickups` **n'est pas servi** — vingt courses feraient vingt
> lectures. Une commande à trois enseignes y ressemblait donc à une course à
> **un seul retrait**, et le livreur ne découvrait les trois qu'après avoir
> accepté.
>
> **`pickups_count` est servi dans les listes aussi.** Affichez-le **avant
> d'accepter** : « 3 collectes » est ce qui distingue une tournée d'une
> course ordinaire, et c'est sur quoi on décide.
>
> ⚠️ **ABSENT veut dire « on ne sait pas », pas « aucune ».** Une course a
> toujours au moins une collecte : le champ ne manque que sur les courses
> créées avant qu'il n'existe. N'affichez alors **rien** — « — » plutôt
> qu'un « 0 collecte » qui serait faux. Le **détail** le comble toujours.
>
> Puis, **dès l'acceptation, relisez `GET /deliveries/{id}`** : c'est là, et
> seulement là, que se trouve la tournée.

```jsonc
// GET /deliveries  (liste)        → pas de `pickups`, mais :
{ "id": "…", "status": "searching", "pickups_count": 3, "cash_required_xof": 7500, … }

// GET /deliveries/{id}  (détail)  → la tournée
{ "pickups_count": 3,
  "pickups": [ { "sequence": 1, "store_name": "Tantie Caro", "done": true,  … },
               { "sequence": 2, "store_name": "Chez Awa",    "done": false, … },
               { "sequence": 3, "store_name": "Mama Africa", "done": false, … } ] }
```

**`sequence` est l'ORDRE DE PASSAGE**, calculé par le serveur sur le réseau
routier — pas l'ordre dans lequel le client a composé son panier. Suivez-le.

> ⚠️ **CHAQUE COLLECTE SE CONFIRME SÉPARÉMENT.**
> `POST /deliveries/{id}/pickups/{pickup_id}/done`, une fois par boutique.
> Une seule confirmation ne clôt **pas** la tournée : tant qu'il reste un
> `done: false`, la course n'est pas prête à partir.
>
> N'enchaînez pas sur « Terminer » avant que **toutes** les collectes soient
> faites — c'est exactement l'erreur qui fait repartir un livreur avec deux
> paquets sur trois.

**Sur la carte**, chaque collecte prend le **pin numéroté du marchand** à son
rang — la collecte `sequence: 1` porte le pin 1 — et le dépôt le pin
principal du **client**. Ces images viennent des réglages
(`GET /map-markers`, § Les marqueurs de carte) : ne dessinez pas trois
pastilles identiques, c'est précisément ce qui empêche de lire une tournée.

### Ce qu'il y a à retirer

Chaque `pickup` porte de quoi **trouver la boutique** et **contrôler le paquet** :

```jsonc
{ "id": "…", "sequence": 1, "geo": [1.2312, 6.1352], "done": false,
  "store_id": "…", "store_name": "Tantie Caro — Hédzranawoé",
  "store_address": "Rue 12, Hédzranawoé", "store_phone": "+22890000001",
  "fragile": true, "pack_size": "large",
  "items": [ { "name": "Riz gras", "variant_name": "Grand", "qty": 2,
               "options": ["Accompagnement : Alloco"], "image": "https://…",
               "fragile": false, "pack_size": "medium" } ] }
```

- **Afficher le nom, jamais `store_id`.** Un identifiant hexadécimal ne désigne aucune porte.
- `store_phone` mérite un bouton d'appel : c'est le recours quand la boutique n'est pas prête.
- **`variant_name` et `options` distinguent deux paquets d'un même plat.** Affichez-les **à côté du nom**, pas repliés derrière un chevron — c'est la ligne qu'on lit devant le comptoir.
- **Aucun prix par LIGNE** : le livreur contrôle qu'il emporte le bon paquet, pas ce que chaque plat a coûté. `GET /orders/{id}` lui reste fermé.

#### Comment le porter — v1.3.0

`fragile` et `pack_size` (`small` · `medium` · `large`) disent le **transport**. Ils sont portés par le **plat au catalogue** : un gâteau l'est toujours, et le redemander à chaque préparation ferait oublier de le cocher au moment où le marchand est le plus pressé.

Ils apparaissent **deux fois**, et les deux servent :

| Niveau | Ce que c'est | Où l'afficher |
|---|---|---|
| ligne | ce plat-ci | dans la liste, à côté du nom |
| `pickup` | **la synthèse** : `fragile` dès qu'**une** ligne l'est, `pack_size` = le **plus encombrant** | sur la carte du point de collecte, **avant** d'ouvrir le détail |

> **La synthèse est celle qui compte.** Un livreur décide de son équipement avant de partir. L'obliger à parcourir les lignes pour savoir s'il lui faut une caisse, c'est le lui faire découvrir devant le comptoir.

> ⚠️ **`pack_size` absent = NON RENSEIGNÉ, pas « petit ».** N'affichez rien plutôt qu'une icône de petit sac : un plat dont personne n'a déclaré la taille ne doit pas faire croire qu'il tient dans une sacoche.

### ⚠️ Le client vous voit aussi — v1.6.0

La symétrie est entrée dans le contrat : sur une course **attribuée**, le client reçoit votre **nom**, votre **téléphone**, votre **note** et votre **véhicule**.

> Ce n'est pas un détail d'affichage, c'est à dire au livreur. Un écran qui laisserait croire à l'anonymat pendant que le client compose son numéro serait un mensonge par omission — et la première réaction, en cas d'appel inattendu, serait de ne pas décrocher.

> **La plateforme répond de l'éthique de ses livreurs**, et c'est ce qui rend l'échange acceptable. La note et le nom voyagent **avec** le numéro : ils laissent une trace de qui a mal agi.

Le livreur, lui, ne reçoit **pas** son propre contact en retour — chacun voit **l'autre**.

### Son client, et l'argent — sur la course ATTRIBUÉE seulement

Depuis la v1.2.0, une course **attribuée** porte :

```jsonc
{ "customer": { "name": "Awa Ndiaye", "phone": "+22890000123" },
  "order_total": 5200,
  "cash_to_collect_xof": 5200 }
```

- `cash_to_collect_xof` est ce qu'il **réclame au client** à l'arrivée. **Zéro sur une commande prépayée** : n'affichez alors rien — un montant ferait redemander une somme déjà payée.
- ⚠️ **À ne pas confondre avec `cash_required_xof`**, qui est ce qu'il **avance** aux marchands. L'un sort de sa poche, l'autre y entre. Deux libellés distincts, deux endroits distincts.

> ⚠️ **Ces champs n'apparaissent QUE sur une course attribuée** — acceptation, collecte, complétion, détail. **Jamais sur `/deliveries/available`.** Un livreur qui parcourt les offres ne moissonne pas des numéros ; celui qui porte la commande voit son client. N'affichez aucun contact sur une carte de la liste.

> **Le téléphone est un RECOURS, pas le premier geste.** La conversation reste le canal principal : elle est tracée, elle ne réveille personne, et elle passe sur un mauvais réseau. Appelez quand personne ne répond.
- Résolution **au mieux** : `store_name` et `items` peuvent manquer. La course reste affichable avec ses points sur la carte.

**L'ordre est imposé** : sauter un point donne `409 pickup_out_of_order`, revalider `409 pickup_already_done`. N'activez le bouton que sur la prochaine collecte attendue.

**En espèces**, le livreur paie le marchand **de la main à la main**, du montant de ses lignes, et se rembourse à l'arrivée. L'écran de collecte doit rappeler la somme à sortir.

**Terminer** n'est accepté qu'en `in_transit` — toutes les collectes faites. La réponse porte **`distance_source`** : `tracked` (positions réellement poussées) ou `planned` (repli). **Affichez la mention quand c'est `planned`** : c'est cette distance qui rémunère, et le livreur doit pouvoir la contester avant de la découvrir sur sa paie.

---

## 6. Émettre sa position

```
wss://tracking-staging.dira.llc/track/agent

{ "vehicle_id": "…", "mission_id": "<delivery_id>", "type": "moto",
  "plate": "…", "lng": …, "lat": …, "heading": …, "heading_source": "gps|compass", "speed": …, "ts": … }
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

> ### 🧭 ⚠️ LES DEUX PINS D'UN VÉHICULE, ET LEUR ORIENTATION (v4.20.0)
>
> **Un véhicule a DEUX images de marqueur, pas une.** Elles viennent de le genre `courier` — `GET /map-markers` :
>
> | | Quand | Comment elle est dessinée |
> |---|---|---|
> | `map_icon_url` | la carte à plat — suivi, liste, vue d'ensemble | **vue de dessus**, caméra à la verticale (**90°**) |
> | `nav_icon_url` | la carte de **navigation**, inclinée vers l'horizon | **vue 3D**, caméra penchée à **~45°** |
>
> ⚠️ **UNE SEULE IMAGE POUR LES DEUX SE VOIT.** Une carte à plat regarde le
> sol à la verticale ; une carte de navigation penche la caméra. Un dessin vu
> de dessus y paraît **écrasé, couché sur la chaussée** — et une vue 3D sur
> une carte à plat paraît, elle, tombée sur le côté.
>
> **`nav_icon_url` absent : retombez sur `map_icon_url`.** Une vue de dessus
> sur une carte penchée reste lisible ; l'absence de tout marqueur, non.
>
> Ce cap sert à **tourner votre marqueur** sur toutes les cartes de la
> plateforme, et sur les vôtres.
>
> ⚠️ **DANS LES DEUX, LE NEZ POINTE VERS LE HAUT DE L'IMAGE** — le capot (ou
> la roue avant) vers le bord supérieur, quel que soit le véhicule et quelle
> que soit la vue. C'est la convention des images envoyées depuis la console.
>
> Ne confondez pas les deux angles : **90° et 45° décrivent la CAMÉRA** (à la
> verticale, ou penchée), jamais l'orientation du véhicule dans l'image.
> Celle-ci ne change pas d'une vue à l'autre — c'est précisément ce qui
> permet à votre code de tourner l'une ou l'autre sans rien savoir de
> laquelle il s'agit.
>
> **Appliquez donc `heading` TEL QUEL** comme rotation du marqueur : 0°
> (plein nord) laisse l'image droite, 90° la tourne d'un quart de tour vers
> la droite. Pas de correction, pas d'offset de −90° — si vous avez besoin
> d'en ajouter un, c'est l'image qui est mal orientée, pas le code :
> signalez-le à l'exploitation plutôt que de le compenser chez vous. Une
> correction faite dans UNE application et pas dans les autres fait rouler
> les véhicules de côté sur un seul écran, et personne ne sait lequel a
> raison.
>
> ⚠️ **La règle vaut pour les VÉHICULES, pas pour les marqueurs de lieu.** Un
> pin de client, de marchand ou de collecte ne tourne jamais : il désigne un
> endroit, pas une direction.

- **`mission_id` présent** = la position est enregistrée dans le parcours ; **absent** = simple présence (en ligne, hors course).
- Cadence conseillée : 1 position / 1–3 s en course. Le serveur limite à 1 / 200 ms.
- Émission **écran éteint** pendant une course.
- Une coupure ne doit perdre aucune position : rejouez-les avec leur `ts` d'origine.
- Un jeton expiré doit mener à une reconnexion après refresh, pas à une boucle d'échecs.

---


## 6 bis. 🔥 La ZONE ROUGE — forte demande non servie (v3.3.0)

Quand **plusieurs clients** n'ont pas trouvé de livreur au même endroit en peu de
temps (par défaut : cinq en un quart d'heure dans un rayon d'un kilomètre),
la plateforme prévient les livreurs **libres et en ligne** à portée (5 km) par un
message FCM **data-only**, sur les mêmes appareils que l'appel :

```json
{ "type": "hot_zone", "signal_id": "…", "vertical": "food",
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
GET https://api-staging.dira.llc/api/v1/analytics/zones?vertical=food
→ { "items": [ { "id", "vertical", "center": [lng, lat], "radius_m", "polygon": [[lng, lat]…],
                 "polyline", "count", "opened_at", "updated_at" } ] }
```

La zone est servie **sans les clients qui la font** : l'application n'a rien à
faire de qui attend où.

---


## 6 ter. 📈 Les ZONES ACTIVES — où est le travail en ce moment (v3.4.0)

À l'ouverture de l'application (et à chaque retour au premier plan), lire :

```
GET https://api-staging.dira.llc/api/v1/analytics/zones/active?vertical=food
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

---

## 7. Parler au client — sans échanger de numéros

```
GET  /orders/{order_id}/messages
POST /orders/{order_id}/messages       { body }
POST /orders/{order_id}/messages/read
```

> ⚠️ **`order_id`, pas `delivery_id`.** Il vient de la course (`delivery.order_id`). Les confondre donne `404`.

C'est **la seule route de commande ouverte au livreur**. Elle ne rend que des messages — ni prix, ni identité. Une bulle ne porte que `from`.

Ouvert de l'affectation jusqu'à **2 h après la livraison** ; ensuite `409 conversation_closed` — **désactivez la saisie**, l'historique reste lisible.

> À ne pas confondre avec `store_phone`, qui reste un vrai numéro : **la boutique est un commerce, le client est une personne.**

Réception en temps réel sur le socket **de l'API** :

```
wss://api-staging.dira.llc/api/v1/food/ws/orders?token=<access_token>
{ "type": "order_message", "order_id": "…", "sender_role": "client", "body": "…", "ts": … }
```

Un livreur n'y voit **que les commandes qu'il porte**. La trame porte le texte : affichez-la sans relire la conversation.

> 🔔 **Le message est POUSSÉ à l'autre côté (v4.8.0, confirmé).** Quand
> l'application du destinataire est fermée ou en arrière-plan, chaque
> message part en notification `chat_message` — titre « Nouveau message »,
> corps = le texte, **jamais le nom de l'expéditeur** (le canal existe pour
> ne pas échanger d'identités) — avec `data: { type: "chat_message",
> order_id }`. Ouvrez la conversation de cette commande dessus, puis
> `GET /orders/{order_id}/messages?cursor=` pour rattraper. Le socket reste
> le canal quand l'écran est allumé : les deux peuvent porter le même
> message, la conversation le dédoublonne par identifiant. Un client peut
> couper la catégorie (`chat_messages` dans `PATCH /me/preferences`) — il
> ne reçoit alors ni la notification ni l'archive.

**v4.0.0** : sur ce même socket arrivent aussi les **`order_status`** des
commandes que vous portez — `accepted`, `picking_up`, `in_transit`,
`completed` (vos propres actions, en écho) et surtout **`cancelled`** (le
client, le marchand ou l'exploitation a annulé sous vous — §11). Gardez-le
ouvert pendant toute la course, pas seulement quand la conversation est
affichée.

---

## 8. Portefeuille — **SOCLE** (sans `/food`)

```
GET  /wallet
GET  /wallet/transactions?limit=&cursor=
POST /wallet/purchase   { tokens }
```

> **Deux soldes.** `balance` en **jetons**, `balance_xof` en **argent**. Ne les additionnez jamais, et n'affichez jamais « solde » sans dire lequel.

- Le solde de jetons doit être visible **en permanence** dans l'en-tête : c'est la ressource qui conditionne le métier. Un livreur qui le découvre vide au moment d'accepter a déjà perdu la course.
- Chaque acceptation produit un mouvement `order_accept`. **Le montant varie.**
- **Le pays peut ne pas prendre de jetons (v4.12.0) — le mode COMMISSION.** Une course rend alors `token_cost: 0` et **`commission_pct`** : la plateforme retient ce pourcentage des frais de livraison — sur ce qu'elle verse (commande payée en ligne : mouvement `delivery_fee` puis `commission`), ou sur le solde du livreur pour une commande en espèces (`commission`, et **`commission_due`** pour ce que le solde n'avait pas : c'est sa **dette**, `debt_xof`, remboursée d'office sur le prochain gain — mouvement `debt_repaid`). Affichez la commission **avant** d'accepter, comme les jetons.
- `balance_xof` reçoit les **frais de livraison** des courses **prépayées**. En espèces, rien n'y transite : le livreur a l'argent en main.
- Un mouvement **`reason: "equipment"`** (`unit: "xof"`, `ref_kind: "equipment"`, v4.11.0) est une retenue — ou, positif, un remboursement de caution — pour le **matériel** vendu ou loué par Dira (§8 bis).
- La recharge ne crédite qu'**après confirmation** du prestataire : suivez `GET /payments/{id}`.

---

## 8 bis. Le MATÉRIEL — gilet, sac, téléphone (v4.11.0) — **SOCLE** (sans `/food`)

> Dira vend, loue ou prête du matériel à ses livreurs. Le contrat, l'échéancier
> et les paiements sont tenus par le **socle** (`…/api/v1/`, pas `/food`) ; ce
> que vous devez est **retenu sur vos frais de livraison** et **prélevé sur
> `balance_xof`**, et vous pouvez **payer depuis l'application**.

```
GET  /equipment/catalogue?vertical=food     → { items: [ … ] }        ce que Dira propose dans votre pays
POST /equipment/requests                    { item_id, mode, quantity?, note? }   → 201 contrat `requested`
GET  /me/equipment                          → { items: [ contrats ], standing }
POST /me/equipment/{id}/accept              → le contrat `accepted`
POST /me/equipment/{id}/pay                 { amount_xof }   → le contrat, payé depuis `balance_xof`
```

**`vertical=food` est obligatoire** sur le catalogue et sur une demande : un
article peut être réservé aux livreurs ou aux chauffeurs (`audiences`), et le
socle ne devine pas depuis quelle application vous parlez. Rôle `driver`
exigé partout ; `X-Dira-Country` fixe le pays du catalogue et des réglages.

### Le catalogue

```json
{ "id": "…", "kind": "bag", "name": "Sac isotherme Dira 45 L", "description": "…",
  "photos": [ "https://…" ], "audiences": [ "food" ],
  "sale_price_xof": 20000, "rental_daily_xof": 0, "rental_weekly_xof": 1000, "rental_monthly_xof": 3500,
  "deposit_xof": 5000, "stock": 8, "track_stock": true, "active": true }
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
livreur passera par l'agence), `409 equipment_mode_not_allowed` (mode fermé
dans ce pays), `409 equipment_not_offered` (article sans prix sous ce
mode), `404` (article inactif ou réservé à l'autre verticale). Le contrat
rendu est `requested` : **rien n'est dû** tant que l'exploitation ne l'a pas
qualifié puis remis.

### Vos contrats — `GET /me/equipment`

```json
{ "items": [ {
    "id": "…", "vertical": "food", "item_id": "…", "item_name": "Sac isotherme Dira 45 L", "item_kind": "bag",
    "quantity": 1, "serial": "", "mode": "sale", "status": "active",
    "price_xof": 20000, "deposit_xof": 5000,
    "paid_xof": 5300, "outstanding_xof": 19700, "due_xof": 0, "overdue_since": null, "blocked": false,
    "plan": { "schedule": "installments", "installments": 4, "period": "weekly", "first_due_days": 7,
              "collect_from_earnings": true, "collect_from_wallet": true, "allow_partial": true,
              "earnings_percent": 15, "earnings_fixed_xof": 0, "min_left_xof": 500,
              "daily_cap_xof": 0, "weekly_cap_xof": 0, "earnings_only_when_due": false,
              "grace_days": 3, "late_fee_xof": 0, "late_fee_percent": 0, "block_after_days": 10,
              "reminder_days": 2, "deposit_refundable": true },
    "schedule": [
      { "n": 1, "kind": "deposit",     "due_at": "…", "amount_xof": 5000, "late_fee_xof": 0, "paid_xof": 5000, "owed_xof": 0,    "paid_at": "…", "status": "paid" },
      { "n": 2, "kind": "installment", "due_at": "…", "amount_xof": 5000, "late_fee_xof": 0, "paid_xof": 300,  "owed_xof": 4700, "status": "pending" },
      { "n": 3, "kind": "installment", "due_at": "…", "amount_xof": 5000, "late_fee_xof": 0, "paid_xof": 0,    "owed_xof": 5000, "status": "pending" } ],
    "payments": [
      { "id": "…", "at": "…", "amount_xof": 5000, "source": "wallet" },
      { "id": "…", "at": "…", "amount_xof": 300,  "source": "earnings", "ref_kind": "order", "ref_id": "…" } ],
    "next_period_at": null, "notes": "", "requested_at": null, "accepted_at": "…", "handed_at": "…",
    "returned_at": null, "closed_at": null, "created_at": "…", "updated_at": "…" } ],
  "standing": { "contracts": 1, "outstanding_xof": 19700, "due_xof": 0, "overdue_xof": 0, "blocked": false } }
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
afficher**, un livreur ne doit pas additionner ses contrats à la main.

Chaque ligne de `schedule` porte `kind` (`deposit` la caution, due à la
remise · `installment` une part du prix · `period` un loyer · `damage` des
dégâts constatés au retour), `status` (`pending` · `due` · `overdue` ·
`paid` · `waived` — remise gracieuse), `late_fee_xof` (pénalité de retard,
ajoutée **une fois** quand le délai de grâce est dépassé), `owed_xof` ce qui
reste sur la ligne.

### Accepter

Un contrat `draft` vous est notifié (**`equipment_contract_proposed`**) :
affichez l'article, le mode, le prix, la caution, **l'échéancier** et
surtout **les prélèvements** (`plan.earnings_percent` % de chaque frais de
livraison et/ou `earnings_fixed_xof` par course, jamais au-delà de ce qui
est dû, en laissant au moins `min_left_xof` sur chaque gain ; et, si
`collect_from_wallet`, l'échu pris sur `balance_xof`), puis un bouton
`POST /me/equipment/{id}/accept`. Autre statut : `409 equipment_bad_status`.
Pas de refus depuis l'application : on n'accepte pas, et l'exploitation
annule. Après acceptation, **rien ne démarre avant la remise physique** ;
`handed_at` et **`equipment_handed_over`** marquent le départ.

### Comment vous payez — trois canaux, cumulables

1. **La retenue sur les frais de livraison** (`collect_from_earnings`). À
   chaque course **prépayée** dont les frais vous sont crédités (§8), le socle
   retient la part du plan **avant** de créditer : `balance_xof` reçoit les
   frais, puis un mouvement **`reason: "equipment"`, `unit: "xof"`,
   `ref_kind: "equipment"`, `ref_id` = le contrat** apparaît dans
   `GET /wallet/transactions`, et le contrat enregistre un paiement
   `source: "earnings"` avec `ref_kind: "order"`. Une course en **espèces** ne
   retient rien — l'argent ne transite pas par Dira.
2. **Le prélèvement sur le solde** (`collect_from_wallet`) : quand une
   échéance arrive (ou que la remise réclame la caution), le socle prend
   l'échu sur `balance_xof` — tout, ou ce qu'il y a si `allow_partial`, jamais
   le solde promotionnel. Même mouvement `reason: "equipment"`.
3. **Le paiement depuis l'application** : `POST /me/equipment/{id}/pay
   { "amount_xof" }` prend sur `balance_xof`, plafonné à ce qui reste dû ;
   `402 insufficient_funds` si `balance_xof` ne couvre pas le montant —
   dites-le tel quel : **`POST /wallet/purchase` ne crédite que des jetons**,
   le solde en argent vient des courses prépayées ; proposez alors le montant
   disponible, ou l'agence. `409 equipment_nothing_owed` si tout est réglé,
   `409 equipment_bad_status` hors `active` · `returned`.

À chaque prélèvement vous recevez **`equipment_charged`** (« 300 F ont été
pris sur vos gains pour Sac isotherme »). Espèces ou mobile money **à
l'agence** restent possibles, enregistrés par l'exploitation
(`source: manual` · `mobile_money`), comme la remise gracieuse (`waiver`).

Le loyer (`rental`) se règle de la même façon, période après période.

### Le retard — et le BLOCAGE

- `reminder_days` jours avant une échéance : **`equipment_due`**.
- Échéance dépassée + `grace_days` : la ligne passe `overdue`, la pénalité
  (`late_fee_xof` et/ou `late_fee_percent` %) s'ajoute une fois, vous recevez
  **`equipment_overdue`**.
- Retard de plus de `block_after_days` jours (si > 0) : `blocked: true`,
  **`equipment_blocked`**, et **`PATCH /agent/availability { available: true }`
  répond `402 equipment_overdue`**. La course en cours se termine
  normalement. L'écran de prise de service doit dire **pourquoi** et
  renvoyer vers l'écran du matériel : `standing.blocked` et, contrat par
  contrat, `overdue_since` + `due_xof`, avec le bouton **payer**. Le blocage
  se lève dès que plus rien n'est en retard.

### Le retour

L'exploitation enregistre le retour : le contrat passe `returned`,
`return_condition` décrit l'état, une ligne `damage` apparaît si des dégâts
sont facturés, et la **caution** est rendue si `plan.deposit_refundable`
(moins les dégâts) : un paiement **négatif** `source: "refund"` dans
`payments[]`, et **`balance_xof` est crédité aussitôt** (mouvement
`reason: "equipment"` positif). Vous recevez **`equipment_returned`**
(« Caution rendue : 5000 F. Reste dû : 0 F »). Un contrat rendu où rien ne
reste dû passe `completed`.

### Notifications (catégorie `support` — non coupable)

| Clé | Quand | Données |
|---|---|---|
| `equipment_contract_proposed` | un contrat `draft` attend votre accord | `{ type: "equipment", contract_id, vertical }` |
| `equipment_handed_over` | remise faite, échéancier lancé | idem |
| `equipment_due` | `reminder_days` avant une échéance | idem |
| `equipment_charged` | une retenue ou un prélèvement a eu lieu | idem |
| `equipment_overdue` | une échéance est en retard | idem |
| `equipment_blocked` | prise de service bloquée | idem |
| `equipment_returned` | retour enregistré, caution rendue | idem |

`data.contract_id` ouvre le contrat ; toutes viennent avec `data.type: "equipment"`.

---

## 9. Notes reçues, support, notifications

> **Notes reçues** et **notifications** sont au **socle** (sans `/food`) ; le **support** reste à la livraison (`…/api/v1/food/tickets`).

```
GET  /agents/{id}/ratings        (public)
POST /tickets · GET /tickets · GET /tickets/{id} · POST /tickets/{id}/messages · POST /bug-reports
POST /tickets/{id}/lost-item     { "found": true | false, "note"? }      ← votre réponse sur un objet perdu (v4.9.0)
POST /me/devices · GET /me/notifications
```

### Le SUPPORT — et l'objet oublié chez vous (v4.9.0)

**Ouvrir une demande** : `POST /tickets { category, message, order_id? }`,
`category` ∈ `order` (un problème sur une commande que **vous** avez livrée —
`order_id` obligatoire, `403` sinon) · `payment` · `tokens` (jetons) ·
`account` · `behaviour` (un client, un marchand) · `other`. Le ticket rendu
porte `reference` (`TCK-000123`, à afficher), `status` et un fil
`messages[]` (`author_role` : `driver` vous, `admin` le support, `client`
sur un objet perdu). Vous recevez **`ticket_reply`** à chaque réponse et
**`ticket_resolved`** à la clôture — catégorie `support`, non coupable.

**🎒 Un client dit avoir perdu quelque chose sur une de vos livraisons** — le
seul ticket qui **vient à vous** : vous recevez **`lost_item_reported`**
(« Objet oublié · Un passager a oublié : Clés de maison (Commande #A1B2C3 ·
19/09 12:40). Vérifiez et répondez depuis l'application », données
`{ type: "lost_item", ticket_id, order_id }`). Ouvrez le ticket dessus
(`GET /tickets/{id}` → `lost_item.item`, `lost_item.details`) ; il figure
aussi dans `GET /tickets` (`counterpart_id` = vous). **Répondez** avec deux
boutons : `POST /tickets/{id}/lost-item { "found": true, "note": "…" }` ou
`{ "found": false }` — réservé au livreur de la commande (`403`), et à un
objet perdu (`409 not_a_lost_item`). Trouvé : **le support vous contacte
pour la restitution**, n'échangez pas de numéros dans le fil. Pas trouvé :
le ticket reste ouvert côté support, vous pouvez répondre à nouveau plus
tard. `lost_item.found` vaut `null` tant que vous n'avez pas répondu :
c'est l'état « à traiter » à mettre en avant.

- La moyenne ne s'affiche jamais sans son **nombre d'avis**.
- ⚠️ **L'appel de course n'est pas coupable dans les préférences** : c'est le gagne-pain du livreur. Les autres catégories le sont.
- Un rapport de bug doit joindre `app_version`, `platform` et l'écran courant : les incidents de ce parcours sont majoritairement des pertes de GPS en arrière-plan, indiagnosticables sans ce contexte.

---

## 10. Carte et itinéraire

Par le **SDK** `@kgtech-org/dira-maps-react-native`, jamais en direct :

- `RouteService` / `useRoute` — tracé de la tournée
- `client.reverseGeocode` — adresse d'un point
- `diraTileTemplate` — couches en surimpression (facultatif)

Ne réécrivez ni la conversion de coordonnées, ni le cache de tournée, ni le repli approximatif : ils sont dans le SDK. À votre charge : afficher l'état **approximatif** (`result.approximate`) et ouvrir l'application de navigation du téléphone.

- L'itinéraire suit le **réseau routier réel** : collectes dans l'ordre `sequence`, puis dépôt.
- `/api/calc/route` est appelé **une fois par tournée**, pas à chaque position GPS.
- La **ville** envoyée est le champ `city` de la course, jamais une déduction locale.

Sur la carte de la course : votre pastille est `courier`, chaque collecte `merchant`, la remise `client`.

### Les marqueurs de carte — `GET /map-markers` (v4.13.0)

```
GET /api/v1/map-markers          (public, SOCLE — sans `{PREFIX}`)
```

```json
{ "items": [
  { "kind": "courier", "icon_url": "…/courier.png", "map_icon_url": "…/courier-map.png",
    "nav_icon_url": "…/courier-nav.png" },
  { "kind": "client", "map_icon_url": "…/client-map.png", "max_numbered": 4,
    "numbered": [ { "index": 1, "map_icon_url": "…/stop-1.png" },
                  { "index": 2, "map_icon_url": "…/stop-2.png" },
                  { "index": 3 }, { "index": 4 } ] },
  { "kind": "merchant", "map_icon_url": "…/store-map.png", "max_numbered": 4,
    "numbered": [ { "index": 1, "map_icon_url": "…/store-1.png" }, { "index": 2 }, { "index": 3 }, { "index": 4 } ] } ] }
```

Toujours les **trois genres**, dans cet ordre : `courier` (le livreur — le
chauffeur VTC est dessiné par son mode de véhicule, `GET /classes`),
`client`, `merchant` (le point de vente, la collecte). Chacun porte deux
images **facultatives**, réglées depuis la console : `icon_url` (à côté d'un
nom, dans une liste) et `map_icon_url` (le pin **principal**, dans la
pastille du marqueur). **Absentes, gardez votre pictogramme** — le réglage
ajoute, il ne retire rien. Lisez la liste à l'ouverture, mettez les images en
cache par URL (elles changent d'URL quand elles changent). Pour toute la
plateforme, pas par pays.

#### 🔢 Les pins NUMÉROTÉS (v4.18.0)

Une carte porte souvent **plusieurs points du même genre** : une commande
collectée chez trois marchands, une course qui s'arrête deux fois avant
d'arriver. Un seul pictogramme pour tous ces points ne dit pas **dans quel
ordre** on y passe — et c'est justement ce qu'on cherche sur une carte.

`client` et `merchant` portent donc, en plus de leur pin principal, jusqu'à
**quatre pins numérotés** : `numbered`, rangs **1 à 4**, toujours servis au
complet même vides. `courier` n'en a pas — il n'y a qu'un livreur par course,
et le champ est **absent** chez lui (absent = « sans objet » ; une liste vide
se lirait « rien de réglé »).

#### 🏁 Et `client` porte, LUI, un pin de DESTINATION

`client.dest_icon_url` : le point d'**arrivée** — la fin d'une course.

⚠️ **Le client et sa destination ne sont pas le même point.** Le pin
**principal** marque **quelqu'un** : le passager qui attend au départ, la
personne à qui on remet une commande. Le pin de **destination** marque un
**lieu** où personne n'attend encore. Les dessiner pareil oblige à lire les
libellés pour savoir lequel est lequel — sur une carte, c'est exactement ce
qu'on n'a pas le temps de faire.

Le marchand n'en a pas : une boutique est une **étape**, la destination de
personne.

Absent : **retombez sur `map_icon_url`**.

Le client porte donc, à lui seul, **tous les points du trajet d'un
passager** : lui (principal), ses étapes (numérotés 1 à 4), son arrivée
(destination). C'est la contrepartie d'y avoir fusionné `stop`.

#### 🚗 Et `courier` porte, LUI, une image de navigation

`courier.nav_icon_url` : le même livreur en **vue 3D à ~45°**, pour une carte
de navigation inclinée. `client` et `merchant` n'en ont pas, et c'est voulu.

⚠️ **La distinction n'est pas « véhicule / pas véhicule », c'est « couché sur
la route / debout sur la carte ».** Un livreur est dessiné **à plat sur la
chaussée** : dès que la caméra penche, une vue de dessus paraît écrasée. Un
client, un marchand, une étape sont des pastilles qui **se dressent** — comme
une épingle plantée, elles gardent la même image quelle que soit
l'inclinaison.

Absent : **retombez sur `map_icon_url`**.

> ⚠️ **LE RANG EST CELUI DU PASSAGE**, pas un identifiant. La première
> collecte porte le pin **1**, la deuxième le **2**. Numérotez dans l'ordre où
> l'on s'y rend — celui que le serveur vous donne —, jamais dans l'ordre
> d'affichage de votre liste.

> ⚠️ **AU-DELÀ DE `max_numbered`, RETOMBEZ SUR LE PIN PRINCIPAL.** Cinq
> collectes, ou un rang qu'on n'a pas réglé : ce n'est pas une panne, c'est
> prévu. Un chiffre dans une pastille de vingt-quatre pixels ne se lit plus
> très loin, et un pin manquant vaut mieux qu'un pin vide.

Un pin numéroté ne porte **que** `map_icon_url` : dans une liste, le nom de la
boutique distingue déjà les points ; c'est sur la carte, où aucun nom ne
tient, que le chiffre sert.

> ⚠️ **`stop` A DISPARU (v4.18.0).** Il a fusionné dans `client` : un arrêt de
> course et l'adresse d'un client sont le même objet vu à deux moments. Les
> étapes d'une course se dessinent désormais avec les **pins numérotés du
> client**, et son arrivée avec le pin principal.

**Sur VOS cartes.** Les collectes se dessinent avec les pins numérotés du
**marchand**, dans l'ordre `sequence` : la collecte `sequence: 1` porte le pin
1. Le dépôt se dessine avec le pin **principal** du `client` — et non celui
de destination : quelqu'un vous y attend. Le pin de destination sert aux
courses, où l'arrivée est un lieu et personne ne s'y trouve encore. Au-delà de quatre
collectes, les suivantes prennent le pin principal du marchand.

---

## 11. Machine à états

```
searching ──accept──▶ accepted ──1re collecte──▶ picking_up ──toutes collectes──▶ in_transit ──complete──▶ completed
    │                    │                          │
    └────────────────────┴──────────────────────────┴── commande annulée ──▶ cancelled
```

> **v4.0.0 — le vocabulaire commun.** `available` est devenu **`searching`**,
> `assigned` → **`accepted`**, `delivering` → **`in_transit`**, `delivered` →
> **`completed`** : les mots d'une course VTC, et ceux de la commande elle-même
> à partir de `accepted`. La route `/deliveries/available` garde son nom — c'est
> une liste, pas un état.

| Statut | Ce que ça veut dire | Pour vous | La commande dit |
|---|---|---|---|
| `searching` | on cherche un livreur | dans `/deliveries/available` une fois le repas prêt ; appelée par vagues | `ready` |
| `accepted` | vous l'avez prise | partez au restaurant | `accepted` |
| `picking_up` | première collecte faite, il en reste | les suivantes, dans l'ordre | `picking_up` |
| `in_transit` | tout est retiré | en route vers le client ; `complete` est possible | `in_transit` |
| `completed` | remis au client | terminé — la distance rémunère | `completed` |
| `cancelled` | commande annulée sous la course | fermer, vous êtes libre | `cancelled` |

L'application ne pilote pas ces états : ils découlent des actions. Elle les **lit** dans la réponse de chaque appel. Le passage par `picking_up` a lieu même pour une collecte unique, pour que les deux machines à états restent linéaires.

### La course peut changer SOUS vous (v4.0.0)

Le client annule pendant que vous roulez vers le restaurant. Vous l'apprenez
de deux façons, et il faut tenir les deux :

- sur le **socket des commandes** (§7), déjà ouvert pour la conversation :
  vous recevez désormais **`order_status`** pour les commandes que vous
  **portez** — `{ "type": "order_status", "order_id": "…", "from": "accepted",
  "status": "cancelled" }` ;
- par **push**, application fermée : `delivery_cancelled`
  (`data.type: "delivery_status"`, `delivery_id`, `order_id`,
  `status: "cancelled"`).

Dans les deux cas : **`GET /deliveries/{id}`**, et si elle est `cancelled`,
fermer la course — vous êtes libre, aucune capacité n'est retenue. Une
collecte ou une remise sur une course annulée répond `409` : relire, pas
réessayer.

**Le flux, dans l'ordre — un signal, un `GET` :**

1. **À l'ouverture d'un écran** : `GET /deliveries/{id}`. C'est l'état de référence —
   jamais ce que dit le socket.
2. **Socket ouvert** : sur une trame d'état, comparez à ce que vous affichez ;
   si ça diffère, `GET /deliveries/{id}` et redessinez. La trame porte le statut : vous
   pouvez changer le badge **avant** la réponse. Une trame qui « recule »
   (un `from` qui n'est pas votre état) signale une trame manquée — relisez.
3. **Push reçu** (application en arrière-plan) : `data.type` dit quoi ouvrir,
   l'identifiant sur quoi, `data.status` ce qui a changé. Même geste : ouvrir
   l'écran, `GET /deliveries/{id}`.
4. **Reconnexion** du socket (back-off 1 s → 2 s → 4 s … 30 s) :
   `GET /deliveries/{id}` **immédiatement**, avant d'appliquer la moindre trame — tout
   ce qui s'est passé pendant la coupure n'est que dans la base.
5. **Sans socket** (refusé, réseau captif, batterie) : **sondez** `GET /deliveries/{id}`
   toutes les **10 s** tant que l'opération n'est ni `completed` ni
   `cancelled`, en comparant `updated_at` ; passez à 30 s au bout de cinq
   minutes sans changement. Ne sondez **jamais** une opération terminée.
6. **Retour au premier plan** : relire vos courses en cours — c'est ce qui
   remet l'écran de course en place après un redémarrage de l'application.

**Ce qu'aucun canal ne garantit** : l'ordre, l'unicité, la livraison. Deux
trames pour le même passage (socket **et** push) sont normales — le second
`GET` répond la même chose. Une application qui ferait du socket sa source
de vérité verrait, un jour, une course « en route » qu'un `GET` dit terminée.

> **`cancelled` — v3.8.0.** Une commande annulée (par le client, par le
> refus du marchand, par l'exploitation) **ferme sa course** : elle quitte le
> pot commun, l'appel en cours est clos (`call_closed`, `reason: cancelled`),
> et si vous la portiez, elle disparaît de vos courses en cours — vous êtes
> libre. Avant, elle restait `searching` et l'accepter rendait « le statut a
> déjà changé ». Une course `cancelled` ne s'accepte pas (`409`) ; l'écran
> doit la retirer, pas réessayer.

---

## 11 bis. ⚠️ Ce que la maquette demande et que l'API ne sert pas

Relevé sur la maquette du **10 septembre 2026**.

### ✅ L'appel de 30 s — entièrement servi

Le handoff classe l'appel parmi les choses « prototypées côté client, à déplacer côté serveur ». **C'est déjà fait**, et §3 en donne le contrat :

| Ce que le handoff exige | Où c'est |
|---|---|
| « le compte à rebours est côté serveur » | `expires_at` sur la trame `call` |
| « le client ne fait que rendre `expires_at` » | §3 — une app en arrière-plan ne peut pas le rallonger |
| « refus ou expiration cascade au suivant » | vagues successives, `attempt` 1→3 |
| « Accept idempotent, un accept tardif rend 409 » | `409 not_called` sur une course déjà réservée ou prise |
| « livrer par push **et** socket » | socket de suivi + gabarit `driver_call`, **non coupable** |

L'écran d'appel ne doit déclencher **aucune requête** avant de s'afficher : `meta` porte le coût en jetons, l'avance en espèces, la distance et le nombre de collectes.

### ✅ Les documents de conformité — servis depuis la v1.7.0

```
GET  /agent/documents
POST /agent/documents   { kind, file_url, vehicle_id?, expires_at? }
```

Quatre pièces, et **le type décide de son propriétaire** :

| Pièce | Rattachée à | `vehicle_id` |
|---|---|---|
| `licence` — permis | le livreur | **refusé** |
| `id_card` — pièce d'identité | le livreur | **refusé** |
| `registration` — carte grise | le véhicule | **requis** |
| `insurance` — assurance | le véhicule | **requis** |

> Un livreur avec deux motos a **deux assurances**. Vendre une moto n'invalide pas son permis ; une moto non assurée devient inutilisable **sans le mettre à l'arrêt** — il bascule sur l'autre.

**`state` est ce qu'il faut afficher**, calculé à la lecture :

| `state` | Ce que ça veut dire | À l'écran |
|---|---|---|
| `pending` | déposée, personne ne l'a regardée | « en cours de vérification » |
| `valid` | validée | rien à faire |
| `expiring` | valide, périme sous **15 jours** | rappel, **pas** une alerte |
| `expired` | la date est passée | **bannière bloquante** |
| `rejected` | refusée — `rejected_reason` dit pourquoi | **afficher le motif**, il dit quoi refaire |

- ⚠️ **`expiring` n'est PAS un défaut.** La pièce est encore valable ; le traiter comme une faute mettrait le livreur à l'arrêt deux semaines avant l'échéance.
- ⚠️ **`pending` reste `pending` quelle que soit la date.** Une pièce jamais regardée n'a jamais compté — ne l'affichez pas « expirée ».
- **`expires_at` absent = la pièce ne périme pas** (une carte grise). N'affichez pas « expiré » sur une absence de date.
- **`missing` nomme ce qui n'a jamais été déposé**, véhicule compris : `insurance:<vehicle_id>`. Sans lui, un livreur qui n'a rien déposé verrait un écran sans défaut.

> ⚠️ **Un dépôt REMPLACE et repasse en `pending`**, même si l'ancienne pièce était validée. Dites-le avant l'envoi : le livreur doit savoir qu'il repart en vérification.

> ⚠️ **RIEN N'EST BLOQUÉ CÔTÉ SERVEUR.** Une pièce expirée n'empêche ni l'appel ni l'acceptation — `compliant` est **informatif**, et c'est l'exploitation qui suspend. **Votre bannière est donc la seule barrière qui existe** : affichez-la en tête d'écran, bloquante, pas en badge discret au fond d'un onglet.

### 🟡 Ce qui manque encore autour de la conformité

L'écran dessiné liste quatre pièces — **permis, carte grise, assurance, pièce d'identité** — chacune avec un état (`valid` · `expiring` · `expired` · `pending`) et une date d'expiration. **Tout cela est servi** : voir juste au-dessus. Ce qui suit est ce qui reste ouvert autour.

Le handoff demande que *« l'expiration bloque le dispatch côté serveur »*. **Ce n'est pas ce qui a été retenu** : la décision produit est d'avertir sans bloquer, et de laisser l'exploitation suspendre depuis sa file de conformité. Le risque est assumé et écrit dans `docs/specs/06-delivery.md`.

Restent ouverts : quelles pièces sont obligatoires **par pays**, la durée de **conservation**, et qui peut relire une pièce déposée.

> ⚠️ **Ne remplissez jamais un état par défaut.** Une pièce marquée « valide » parce que rien ne la contredit est exactement ce qu'un audit ne pardonne pas.

### 🟡 Les statistiques du profil

L'écran `ag_profile` montre `depuis 2024`, `428 courses`, `96 % d'acceptation`, `152 h en ligne`.

| Chiffre | API |
|---|---|
| courses | ✅ `deliveries_count` |
| note + nombre d'avis | ✅ `rating_avg` · `rating_count` |
| **depuis** | ❌ — le profil livreur ne rend pas sa date de création |
| **taux d'acceptation** | ❌ — les refus d'appel ne sont pas comptés par livreur |
| **heures en ligne** | ❌ — rien ne mesure la durée de présence |

> ⚠️ **Le taux d'acceptation mérite d'être posé avant d'être construit.** Affiché à un livreur, il devient une note ; et une note d'acceptation pousse à prendre des courses qu'on refuserait à raison — trop loin, trop lourdes, mal payées. C'est une décision produit, pas un compteur.

---

## 12. Points ouverts

1. **Ni zone ni type de véhicule** dans la sélection des appelés : seule la distance au premier point de collecte compte. Un piéton peut être appelé pour douze kilomètres.
2. **Réglages du dispatch** (5 par vague, 30 s, 3 tentatives, 10 km) en variables d'environnement du suivi, pas dans un écran d'administration. À calibrer par ville.
3. **`delivery_agents.is_online` n'est écrit que par le seed.** La visibilité du dispatcher vient de l'émission de positions et de `PATCH /agent/availability` ; le drapeau reste un vestige.
4. ~~**Aucun écran de documents**~~ — **servi** depuis la v1.7.0, voir la section « Les documents de conformité ».
5. **Cadence d'émission** à mesurer sur le terrain avant de figer les 2–3 s.
6. ⚠️ **L'authentification du service de suivi peut être désactivée** (`TRACKING_JWT_SECRET` vide) : à activer conjointement avant mise en production.
