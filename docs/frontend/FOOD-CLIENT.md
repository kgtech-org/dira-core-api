# App CLIENT — LIVRAISON — contrat d'API

> **Version 4.15.0** · 22 septembre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Livraison : `https://api-staging.dira.llc/api/v1/food` · Suivi : `wss://tracking-staging.dira.llc`


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

## 1. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | `?limit=20&cursor=<id>` → `{ "items": [...], "next_cursor": "…" }` |
| Montants | **entiers**, en XOF. Jamais de flottant. |
| Dates | ISO 8601 UTC. Une **date seule** s'écrit `YYYY-MM-DD` (naissance, plan de repas). |
| Langue | `Accept-Language` |
| Pays | `X-Dira-Country: TG` — sur chaque requête ; la réponse porte le pays retenu (voir la section *Le pays*) |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ; le code est le contrat.

**Un `422 validation_failed` nomme ses champs — v3.0.0.** `fields` liste les **clés JSON** en cause : soulignez **ces** cases, pas une bannière sous tout le formulaire. `reason` précise, quand ce n'est pas la valeur d'un champ : `unknown_field` (une clé que la route ne connaît pas — **refusée, pas ignorée**, son nom est dans `fields` ; c'est un bug de l'application) ou `invalid_json`.

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

## 2. Compte — **SOCLE** (base `…/api/v1/`, sans `/food`)

```
POST   /auth/register        { phone (E.164, avec le +), name, password, role: "client", email?, first_name?, last_name? }
POST   /auth/login           { phone | email, password }
POST   /auth/refresh         { refresh_token }
POST   /auth/logout          { refresh_token }
GET    /me
PATCH  /me                   { name?, first_name?, last_name?, birth_date?, gender?, email?, avatar_url? }
```

- `birth_date` est `YYYY-MM-DD`. Une date future est refusée.
- `gender` ∈ `female` · `male` · `other`. Facultatif — personne n'est forcé de répondre.
- **`name` reste le nom d'affichage.** `first_name` / `last_name` servent le formulaire d'état civil : ne les déduisez pas de `name`, et n'affichez pas leur concaténation là où `name` existe.
- Le refresh est **sérialisé** : deux requêtes concurrentes avec le même refresh token en invalident un.
- ⚠️ **Le téléphone porte son indicatif** : `+22890200001`. Sans `+`, `422` avec `fields: ["phone"]` — le socle ne devine pas de pays. Espaces, points, tirets et `00` sont tolérés et retirés ; c'est la forme canonique qui est stockée et qui sert à se connecter. Pré-remplissez `+228` là où la personne le voit.
- **`phone_taken` (409)** à l'inscription : le numéro a déjà un compte → proposer la connexion, pas « une erreur est survenue ».
- **`account_suspended` (403)** à la connexion, mot de passe correct : le dire tel quel — ce n'est ni un mauvais mot de passe, ni une panne.

### Préférences

```
PATCH /me/preferences   { order_updates?, chat_messages?, promotions?, tombola?,
                          theme?, locale?, sounds?, payment_provider? }
```

**Fusion, pas remplacement** : envoyez l'interrupteur qu'on vient de basculer, pas les six autres.

- `theme` ∈ `system` · `light` · `dark`.
- `locale` est la langue **choisie**. Elle prime sur celle du téléphone.
- `payment_provider` **présélectionne** l'opérateur mobile money. Aucun jeton de paiement n'est conservé : chaque paiement passe par l'opérateur comme la première fois.
- Les catégories se coupent **une par une**. Un client qui refuse les promotions doit continuer de savoir que son livreur est en bas.

Les préférences arrivent dans `GET /me` — pas d'appel séparé au démarrage.

### Adresses

```
GET    /me/addresses
POST   /me/addresses        { label, address, geo: [lng, lat], details?, is_default? }
PUT    /me/addresses/{id}
DELETE /me/addresses/{id}
```

- L'adresse **par défaut sort en tête** : présélectionnez la première.
- `details` — « portail bleu », « 2e étage gauche » — est ce qui fait **trouver la porte**. Proposez-le explicitement : c'est ce que le client répétait au livreur à chaque commande.
- La **première** adresse devient celle par défaut. Supprimer celle par défaut en **promeut** une autre.
- 20 au maximum → `409 too_many_addresses`.

---

## 3. Découverte

```
GET /stores?lat=&lng=&radius=5000&q=&tags=&limit=
GET /stores/{id}
GET /stores/{id}/menu?tags=
GET /dishes/{id}
GET /dish-categories
GET /tags
GET /promotions
GET /banners
```

- **`lat` + `lng` vont ensemble.** Sans position **ni** `q`, la requête est refusée : rendre la plateforme entière n'aiderait personne.
- Avec position, chaque boutique porte `distance_m` et la liste est triée du plus proche. Sans position, tri par nom et **pas de `distance_m`** — c'est le cas d'un utilisateur qui a refusé la localisation.
- `tags=italien,libanais` **élargit** (l'un OU l'autre) : personne ne cherche un restaurant qui serait les deux.

### La carte d'une ENSEIGNE — v1.4.0

```
GET /merchants/{id}/menu?lat=&lng=&tags=
```

Le client lit la carte d'une **marque** ; le serveur retient le point de vente le plus proche de la position donnée et sert **ses** prix. La réponse dit lequel :

```jsonc
{ "store": { "id": "…", "name": "Tantie Caro — Hédzranawoé",
             "address": "Rue 12", "distance_m": 400 },
  "items": [ /* comme /stores/{id}/menu */ ] }
```

> ⚠️ **`lat` et `lng` sont obligatoires.** Un point de vente peut surcharger ses prix : servir la carte depuis une position par défaut afficherait des montants qu'une boutique choisie au hasard ne confirmerait pas, et le client découvrirait l'écart au paiement.

> **Passez la position de LIVRAISON, pas celle du téléphone**, dès que le client a choisi son adresse. C'est elle qui décidera de la boutique à la commande — les deux résolutions doivent être d'accord, sinon le prix change entre le panier et le paiement.

> **Affichez `store`.** Ce n'est pas décoratif : c'est cette boutique qui préparera la commande, et le client doit savoir d'où part son repas. `distance_m` rend l'affirmation vérifiable.

### La carte d'une boutique

`GET /stores/{id}/menu` sert tout ce qu'il faut pour commander **sans rouvrir chaque fiche** : prix effectif (promotion comprise), `original_price` + `promo_title` s'il y a promotion, disponibilité, variantes, groupes d'options, note et **nombre d'avis**, allergènes.

```jsonc
{
  "dish_id": "…", "name": "Riz gras", "price": 2500, "base_price": 2500,
  "available": true, "has_variants": true,
  "variants": [ { "id": "…", "name": "Grand", "price": 3000 } ],
  "option_groups": [ { "id": "…", "name": "Accompagnement",
                       "min_select": 1, "max_select": 1,
                       "options": [ { "id": "…", "name": "Alloco", "price": 500 } ] } ],
  "rating_avg": 4.6, "rating_count": 212,
  "allergens": ["arachide"], "calories": 700
}
```

- **`has_variants` vrai ⇒ `price` est un prix « à partir de »**, et une variante devra être choisie à la commande.
- **La moyenne ne s'affiche jamais seule** : `rating_count` l'accompagne partout. 5,0 sur un avis et 4,6 sur deux cents ne disent pas la même chose. Zéro avis n'est **pas** une mauvaise note.

### Tags — l'axe de recherche

`GET /tags` rend le vocabulaire **actif**. Les mêmes slugs étiquettent les **enseignes**, les **points de vente** et les **plats** : un seul vocabulaire pour les trois.

| Route | Ce qu'elle filtre |
|---|---|
| `GET /stores?tags=italien,africain` | les points de vente |
| `GET /stores/{id}/menu?tags=africain` | les plats d'une carte |

Plusieurs valeurs **élargissent** (l'un OU l'autre). Un filtre sans correspondance rend une liste **vide**, pas une erreur.

> ⚠️ **À ne pas confondre avec `/dish-categories`.** La catégorie dit *ce que c'est* — « Boissons », « Desserts » — et un plat n'en a qu'une : c'est la structure de la carte, l'ordre des sections. Le tag dit *à quoi ça se rattache*, et il y en a plusieurs : c'est le filtre. Un tiramisu est une seule fois un dessert, et à la fois italien et sucré.

> `/store-tags` reste servi mais est **déprécié** — appelez `/tags`.

### Allergènes

Croisez `dish.allergens` avec `nutrition.allergies` et avertissez à l'intersection.

> ⚠️ **`allergens` absent ne veut PAS dire « sans allergène »** — cela veut dire « non renseigné ». N'affichez pas de pastille rassurante sur un plat qui n'a rien déclaré.

---

## 4. Commander

```
POST /orders
```

```jsonc
{
  "items": [ { "store_id": "…", "dish_id": "…", "qty": 2,
               "variant_id": "…", "option_ids": ["…"] } ],
  "delivery": { "address": "Rue 12, Hédzranawoé", "geo": [1.2255, 6.1319] },
  "payment_method": "online" | "cash" | "wallet",
  "scheduled_for": null
}
```

- **`store_id` est FACULTATIF depuis la v1.4.0.** Renseigné, il **fait foi** — le client a choisi son comptoir, et le serveur ne le remplace jamais, même si une autre boutique est plus proche. Absent, le serveur retient le point de vente de l'enseigne **le plus proche de `delivery.geo`** qui a réellement le plat.
- ⚠️ **`delivery.geo` devient obligatoire dès qu'une ligne omet son `store_id`** — c'est cette position qui choisit la boutique. `[0, 0]` est **refusé**, pas résolu : ce point est au large du Ghana.
- ⚠️ **Lisez le `store_id` de la RÉPONSE**, ligne par ligne. C'est lui qui fait foi, et il peut différer de ce que l'écran laissait attendre. C'est aussi lui qu'il faut grouper à l'affichage du suivi.
- **Une commande peut couvrir plusieurs boutiques** : groupez le panier par `store_id` à l'affichage, mais envoyez une seule commande.
- ⚠️ **Chaque ligne est résolue indépendamment.** Trois enseignes au panier donnent trois boutiques, chacune la plus proche de la sienne — ce qui ne minimise pas la tournée du livreur. C'est voulu : optimiser le trajet global ferait préparer un plat par une boutique plus lointaine à cause d'une autre enseigne du panier.
- `variant_id` est **obligatoire** quand le plat a des variantes, **refusé** sinon.
- `option_ids` mélange tous les groupes ; le serveur retrouve le groupe de chacune et vérifie les bornes.
- **Les prix sont résolus côté serveur.** Ceux que vous affichez sont indicatifs jusqu'à la réponse.

### Les trois moyens de paiement

| | Statut à la création | Qui encaisse, quand |
|---|---|---|
| `online` | `pending_payment` | mobile money, avant préparation |
| `wallet` | `paid` (après débit) | le solde Dira, à la création |
| `cash` | `paid` | le livreur, à l'arrivée |

> **`cash` est confirmée d'emblée alors que rien n'est encaissé.** `settled` dit si l'argent est **arrivé** — ne le confondez pas avec le statut.

`wallet` : la commande est créée en attente, débitée, **puis** confirmée. Solde insuffisant → **`402 insufficient_funds`**, et la commande est **annulée**. N'affichez pas de commande en attente dans ce cas.

> **Le pays peut débiter le portefeuille PLUS TARD (v4.12.0).** Quand la
> facturation du pays le règle, la commande `wallet` naît **confirmée sans
> débit** (`status: paid`, `settled: false`) et porte **`charge_at`** :
> `accept` (un livreur accepte), `start` (commande retirée) ou `complete`
> (livrée). L'argent part à cette étape — affichez « débité à … » sur la
> commande. Avant la livraison, un solde insuffisant **annule** la commande
> (`cancelled`, `payment_failed`, push `order_cancelled`). À la livraison, le
> repas est chez le client : l'impayé devient sa **dette** (`GET /wallet` →
> `debt_xof`), remboursée **d'office sur sa prochaine recharge** — dites-le
> lui plutôt que d'afficher un solde qui « disparaît » au moment de
> recharger. Sans `charge_at`, rien ne change : débité à la commande.

### Payer en ligne — **SOCLE** (sans `/food`)

```
POST /payments/initiate   { purpose: "order", ref_id: <order_id>, amount, provider? }
GET  /payments/{id}
```

`provider` omis ⇒ l'opérateur mémorisé du client, sinon le défaut. Un opérateur **explicitement demandé** et inconnu est une **erreur**.

> **Ne concluez jamais un paiement sur la seule réponse HTTP.** Suivez `GET /payments/{id}` jusqu'à `succeeded`, ou attendez le passage de la commande à `paid`.

### Suivre

```
GET /orders?status=paid,preparing&limit=20&cursor=
GET /orders/{id}
POST /orders/{id}/cancel
```

`?status=` prend plusieurs statuts séparés par des virgules — c'est l'onglet « en cours » qui en couvre six. Un statut inconnu est **refusé** plutôt que rendu vide.

**Les plus récentes d'abord** (v4.12.1) : la liste est triée par date de création, la dernière commande en tête, et `?cursor=` (l'identifiant de la dernière ligne reçue) rend la page suivante, plus ancienne. Avant, la liste commençait par la plus ancienne — n'inversez plus rien côté application.

Les neuf statuts :

```
pending_payment → paid → preparing → ready → accepted → picking_up → in_transit → completed
                                     (tout état avant in_transit → cancelled)
```

> **v4.0.0 — le vocabulaire commun.** De `accepted` à `completed`, la
> commande reprend **mot pour mot** le cycle de sa course de livraison — et
> celui d'une course VTC : `accepted → picking_up → in_transit → completed`.
> `assigned`, `delivering`, `delivered` n'existent plus (un filtre
> `?status=delivered` répond `422`). Un seul écran d'état pour les repas et
> les courses :

| Statut | Ce que ça veut dire | Commande de repas | Course VTC (même application) |
|---|---|---|---|
| `searching` | on cherche quelqu'un | — (la commande dit `ready`) | on appelle des chauffeurs |
| `accepted` | quelqu'un a pris l'opération | un livreur part au restaurant | un chauffeur l'a prise |
| `picking_up` | il est au point de départ | il retire les plats | il roule vers vous |
| `in_transit` | le colis / le passager est à bord | en route vers vous | vous êtes monté |
| `completed` | livré / déposé | livrée | déposé |
| `cancelled` | fini sans être fait | annulée, remboursée si payée | annulée |

| Statut | Ce que voit le client |
|---|---|
| `pending_payment` | en attente du paiement mobile money |
| `paid` | payée, le restaurant est prévenu |
| `preparing` | en préparation |
| `ready` | prête — on cherche un livreur |
| `accepted` | un livreur a pris la course, il part au restaurant |
| `picking_up` | il retire les plats (plusieurs boutiques : collecte en cours) |
| `in_transit` | en route vers vous |
| `completed` | livrée — noter (§7) |
| `cancelled` | annulée — remboursée si elle était payée |

L'annulation est possible jusqu'à `picking_up` inclus. Au-delà → `409 cannot_cancel`.

---

## 5. Suivi de livraison

```
GET /deliveries/{id}
```

Puis le **microservice de suivi**, pas l'API :

```
wss://tracking-staging.dira.llc/track/subscribe/{delivery_id}

{ "type": "hello",    "mission_id": "…" }
{ "type": "position", "vehicle_id": "…", "vehicle_type": "moto", "plate": "…",
  "lng": 1.2255, "lat": 6.1319, "heading": 122.5, "speed": 8.3, "ts": 1757… }
{ "type": "status",   "status": "in_transit", "ts": 1757… }
```

> **`mission_id` du suivi = `delivery_id` de l'API.** C'est la clé de jointure.

La trame `status` arrive à **chaque changement d'état de la course**
(`accepted`, `picking_up`, `in_transit`, `completed`, `cancelled`) — les mots
du vocabulaire commun. Elle ne porte que le mot : relisez `GET /orders/{id}`
(la commande porte le même état) et redessinez.

Attentes : reconnexion avec back-off, **interpolation** entre deux positions, et repli sur `GET /deliveries/{id}` si le socket est indisponible.

⚠️ **La base d'URL du suivi est distincte de celle de l'API.** Deux variables d'environnement.

---

Sur la carte du suivi : le livreur est `courier`, la boutique `merchant`, votre adresse `client`.

### Les marqueurs de carte — `GET /map-markers` (v4.13.0)

```
GET /api/v1/map-markers          (public, SOCLE — sans `{PREFIX}`)
```

```json
{ "items": [
  { "kind": "courier",  "icon_url": "https://files.dira.llc/marker/…/courier.png", "map_icon_url": "https://files.dira.llc/marker/…/courier-map.png" },
  { "kind": "client" }, { "kind": "merchant" }, { "kind": "stop" } ] }
```

Toujours les **quatre genres**, dans cet ordre : `courier` (le livreur — le
chauffeur VTC est dessiné par son mode de véhicule, `GET /classes`),
`client`, `merchant` (le point de vente, la collecte), `stop` (le départ ou
une étape d'une course, quand ce n'est ni le client ni un marchand). Chacun
porte deux images **facultatives**, réglées depuis la console : `icon_url`
(à côté d'un nom, dans une liste) et `map_icon_url` (dans la pastille du
marqueur). **Absentes, gardez votre pictogramme** — le réglage ajoute, il ne
retire rien. Lisez la liste à l'ouverture, mettez les images en cache par
URL (elles changent d'URL quand elles changent). Pour toute la plateforme,
pas par pays.

## 6. Parler au livreur — sans échanger de numéros

```
GET  /orders/{id}/messages?limit=50&cursor=<dernier id>
POST /orders/{id}/messages          { body }   (≤ 1000 caractères)
POST /orders/{id}/messages/read
```

```jsonc
{ "items": [ { "id": "…", "from": "client" | "driver", "body": "…",
               "read_at": "…", "created_at": "…" } ],
  "unread": 1, "next_cursor": "…" }
```

- **Une bulle ne porte que `from`.** Ni identifiant, ni nom, ni téléphone : l'API n'en rend aucun, et il ne faut pas en inventer.
- Le bouton n'apparaît **qu'à partir de `accepted`**. Avant → `409 no_driver_yet`.
- La conversation se ferme **2 h après la livraison** → `409 conversation_closed`. **Désactivez la saisie sur ce refus** ; l'historique reste lisible.
- L'accusé de lecture part **après** l'affichage : marquer lu sans montrer effacerait un non-lu que personne n'a vu.

Réception immédiate sur le socket des commandes (§8). Repli : `?cursor=<dernier id reçu>` ne rend que la suite — c'est aussi le rattrapage après coupure.

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

---

## 7. Noter

> ⚠️ **Le DÉPÔT d'une note reste à la livraison** (`POST /api/v1/food/orders/{id}/rating`) : c'est elle qui sait qu'une commande est livrée et ce qu'elle contenait. Les **LECTURES** — `/stores/{id}/ratings`, `/dishes/{id}/ratings`, `/agents/{id}/ratings` — sont au **socle**, sans `/food` : les courses noteront leurs chauffeurs avec la même collection.

```
POST /orders/{id}/rating
GET  /stores/{id}/ratings · /agents/{id}/ratings · /dishes/{id}/ratings   (public)
```

```jsonc
{ "driver": { "score": 5, "comment": "rapide" },
  "stores": [ { "store_id": "…", "score": 4 } ],
  "dishes": [ { "dish_id": "…", "score": 2, "comment": "trop salé" } ] }
```

- Seulement une commande **livrée** → sinon `409 order_not_delivered`.
- Les trois parties sont **facultatives** : on peut noter un plat sans juger le livreur.
- Une note par cible et par commande → `409 already_rated`.
- Le **point de vente** est noté, pas l'enseigne. Les plats sont dédoublonnés.

---

## 8. Temps réel — le socket des commandes

```
wss://api-staging.dira.llc/api/v1/food/ws/orders?token=<access_token>

{ "type": "hello", "role": "client" }
{ "type": "order_created" | "order_status", "order_id": "…", "from": "paid",
  "status": "preparing", "total": 5200, "ts": 1757… }
{ "type": "order_message", "order_id": "…", "sender_role": "driver",
  "body": "Je suis en bas", "ts": 1757… }
```

- Un client ne reçoit **que ses commandes**.
- La trame de message **porte le texte** : affichez-la sans relire la conversation.
- ⚠️ **Deux sockets, deux services** : celui-ci (API) et celui du suivi. Cycles de vie indépendants, bases d'URL distinctes. Ne les factorisez pas sous prétexte que ce sont deux WebSockets.
- Diffusion **au mieux**, rien n'est rejoué : relisez la ressource REST à la reconnexion.

### Détecter, relire — le flux (v4.0.0)

`order_status` ne porte **pas** la commande : `from` et `status`, rien
d'autre. Le geste est toujours le même — **un signal, un `GET`** :

1. écran ouvert → `GET /orders/{id}` (ou la liste) : l'état de référence ;
2. `order_status` reçu → si `status` ≠ ce que vous affichez, `GET` et
   redessinez ; si `from` ≠ votre état, une trame vous a échappé — `GET` ;
3. **push** reçu, application en arrière-plan (`data.type: "order_status"`,
   `order_id`, **`status`**) → ouvrir la commande, `GET` ;
4. reconnexion du socket → `GET` **avant** d'appliquer quoi que ce soit ;
5. **sans socket** : sonder la commande toutes les **10 s** tant qu'elle
   n'est ni `completed` ni `cancelled` (`updated_at`), 30 s au bout de cinq
   minutes sans changement.

Les pushs par statut : `order_confirmed` (`paid`), `order_preparing`,
`order_ready`, `order_assigned` (`accepted` — la clé garde son nom, l'état
non), `order_delivered` (`completed`), `order_cancelled`. Tous portent
`data.type: "order_status"`, `data.order_id`, `data.status`.

**Trois canaux, et l'application en tient deux à la fois :**

| Canal | Ce qui arrive | Portée |
|---|---|---|
| **Socket des commandes** (ci-dessus) | `order_created`, `order_status { from, status }`, `order_message` | application ouverte, **toutes** vos commandes |
| **Socket du suivi** `wss://tracking…/track/subscribe/{delivery_id}` (§5) | `position`, et **`status`** à chaque passage de la course | application ouverte, une livraison à la fois |
| **Push** (socle, `POST /me/devices`, §12) | un gabarit **et** des données (`type`, `order_id`, `status`) | application fermée ou en arrière-plan |

**Le flux, dans l'ordre — un signal, un `GET` :**

1. **À l'ouverture d'un écran** : `GET /orders/{id}`. C'est l'état de référence —
   jamais ce que dit le socket.
2. **Socket ouvert** : sur une trame d'état, comparez à ce que vous affichez ;
   si ça diffère, `GET /orders/{id}` et redessinez. La trame porte le statut : vous
   pouvez changer le badge **avant** la réponse. Une trame qui « recule »
   (un `from` qui n'est pas votre état) signale une trame manquée — relisez.
3. **Push reçu** (application en arrière-plan) : `data.type` dit quoi ouvrir,
   l'identifiant sur quoi, `data.status` ce qui a changé. Même geste : ouvrir
   l'écran, `GET /orders/{id}`.
4. **Reconnexion** du socket (back-off 1 s → 2 s → 4 s … 30 s) :
   `GET /orders/{id}` **immédiatement**, avant d'appliquer la moindre trame — tout
   ce qui s'est passé pendant la coupure n'est que dans la base.
5. **Sans socket** (refusé, réseau captif, batterie) : **sondez** `GET /orders/{id}`
   toutes les **10 s** tant que l'opération n'est ni `completed` ni
   `cancelled`, en comparant `updated_at` ; passez à 30 s au bout de cinq
   minutes sans changement. Ne sondez **jamais** une opération terminée.

**Ce qu'aucun canal ne garantit** : l'ordre, l'unicité, la livraison. Deux
trames pour le même passage (socket **et** push) sont normales — le second
`GET` répond la même chose. Une application qui ferait du socket sa source
de vérité verrait, un jour, une course « en route » qu'un `GET` dit terminée.

---

## 9. Portefeuille — « Dira Cash » — **SOCLE** (sans `/food`)

```
GET  /wallet
GET  /wallet/transactions?limit=&cursor=
POST /payments/initiate   { purpose: "wallet_topup", ref_id: <user_id>, amount, provider? }
```

```jsonc
{ "balance_xof": 12000, "promo_xof": 2000, "spendable_xof": 14000 }
```

- **`promo_xof` est ce que la plateforme a offert**, servi à part. Affichez « dont X offerts » — ne l'additionnez pas vous-même.
- **`spendable_xof` est le nombre sur lequel se décide un achat.** Calculé par le serveur pour que deux applications ne l'additionnent pas différemment.
- **`debt_xof` (v4.12.0) est ce que le client DOIT** : une commande ou une course au portefeuille débitée à la livraison (`charge_at`) que le solde n'a pas couverte. Remboursée d'office sur la prochaine recharge — affichez « dont X de dette à régler » quand il est non nul, et **avant** la recharge, ce qu'il en restera.
- Le promotionnel se dépense **en premier**.
- ⚠️ **Un client n'a pas de jetons.** `balance` reste à zéro, et `POST /wallet/purchase` lui est **refusé** (`403`) : les jetons sont le droit d'entrée d'un livreur et l'outil de promotion d'un marchand.
- La recharge n'est créditée qu'à la **confirmation** du prestataire.
- **Le portefeuille s'ouvre à la première lecture — v3.0.0.** `GET /wallet` répond toujours `200` à un client, vide au besoin ; il n'y a pas de « pas encore de portefeuille » à gérer.

---

## 10. Nutrition

```
GET /nutrition/profile · PUT /nutrition/profile
POST /nutrition/reminders · DELETE /nutrition/reminders/{idx}
```

`404 profile_not_found` quand aucun profil n'existe : c'est l'état « jamais configuré », proposez la création.

### Planificateur de repas

```
GET    /nutrition/plan?from=YYYY-MM-DD&days=7
PUT    /nutrition/plan/{date}          { slot, dish_id?, dish_name, kcal? }
DELETE /nutrition/plan/{date}/{slot}
```

- Créneaux : `breakfast` · `lunch` · `dinner` · `snack`.
- **Les jours vides sont rendus** : n'essayez pas de reconstituer le calendrier.
- `kcal` d'une journée et `goal_kcal` viennent du **serveur** — ne les recalculez pas.
- Poser un repas **remplace** celui du même créneau.
- `dish_id` est **facultatif** : on planifie « riz gras » avant de savoir chez qui.
- Horizon : 31 jours.

---

## 11. Assistant — la chatbox IA du client (v4.14.0 : règles d'implémentation)

```
POST /ai/suggestions   { limit? }    → { suggestions }   combos, respectant le profil nutrition
POST /ai/chat          { message }   → { reply, plan? }
```

```jsonc
{ "reply": "Je vous propose deux riz gras.",
  "plan": { "items": [ { "dish_id": "…", "store_id": "…", "name": "Riz gras",
                         "price": 2000, "qty": 2, "calories": 700 } ],
            "unresolved": ["caviar béluga"] } }
```

> ⚠️ **`reply` est une phrase à lire ; seul `plan` alimente le panier.** Chaque ligne du plan a été retrouvée dans le catalogue par le serveur. Une application qui commanderait depuis `reply` contournerait cette garantie.

`plan` est absent quand le message n'était pas une commande. `unresolved` nomme ce que le catalogue ignore — proposez une recherche.

### Ce que l'assistant EST — et n'est pas

- Un **modèle de langage** derrière le service d'analytique, qui **ne
  connaît que le contexte que le serveur lui donne** : il n'invente ni
  plat, ni prix, ni délai, et **n'agit jamais** — il ne commande pas, ne
  paie pas, n'annule pas. Tout passe par les routes normales (`POST
  /orders`, §4) après un geste du client.
- **Mono-tour** : le serveur ne garde **aucun historique**. Chaque
  `message` est lu seul. Le fil de conversation est **à vous** (mémoire de
  l'écran, perdue à la fermeture — c'est voulu, rien n'est stocké côté
  serveur).
- **Deux sorties indépendantes** : `reply` (le texte du modèle) et `plan`
  (les plats que le serveur a **reconnus** dans le message, contre le
  catalogue du pays). Le plan ne dépend pas du texte : un modèle
  indisponible rend quand même un plan si le message nommait des plats.

### Écrire la requête

1. **Un message, une requête**, `Authorization` + `X-Dira-Country` comme
   partout ; `message` ≤ 2 000 caractères (`422` au-delà ou vide).
2. **`Accept-Language`** décide de la langue de la réponse (`fr` par
   défaut, `en`). Envoyez la langue de l'interface, pas celle du texte.
3. **Une seule requête en vol** : désactivez l'envoi tant que la réponse
   n'est pas là. Il n'y a pas d'idempotence — un double envoi coûte deux
   appels au modèle et rend deux réponses.
4. Pour une **commande**, le serveur reconnaît « *quantité + nom* »,
   séparés par des virgules : « 2 riz gras, 1 bissap ». Si le client
   écrit « et un bissap » après coup, **recomposez un message complet**
   (« 2 riz gras, 1 bissap ») avant d'envoyer — le serveur n'a pas la
   phrase d'avant.
5. Ne mettez **ni numéro de téléphone, ni adresse, ni code** dans le
   message : l'assistant n'en a pas besoin, et le texte part chez un
   fournisseur de modèle.

### Attendre la réponse

- Comptez **2 à 10 s** (modèle), jusqu'à **45 s** au pire (délai serveur,
  après quoi la livraison répond avec son texte de repli). Montrez
  « l'assistant écrit… » dès l'envoi ; **pas de sablier bloquant** — le
  client peut continuer à naviguer, la réponse arrive dans le fil.
- **Ne réessayez pas automatiquement** en boucle : une erreur réseau ou un
  `5xx` → un bouton « Réessayer », une fois.

### Lire la réponse

- `reply` : affichée telle quelle, dans une bulle. Elle peut dire « Je
  n'ai pas pu répondre pour le moment. Réessayez dans un instant. » — c'est
  le **repli** quand le modèle est indisponible (la route rend quand même
  `200`) : affichez-la, proposez la recherche du catalogue, n'insistez pas.
- `plan.items[]` : une **carte de proposition** sous la bulle, avec les
  plats, quantités, prix et calories **tels que servis** (ils viennent du
  catalogue, pas du modèle), et **un bouton « Ajouter au panier »** —
  jamais d'ajout automatique, jamais de commande depuis l'assistant. Une
  ligne dont `store_id` est vide n'est disponible dans aucun point de
  vente ouvert : montrez-la grisée.
- `plan.unresolved[]` : « Je ne trouve pas *caviar béluga* » avec un lien
  vers la recherche (§3). Ce sont aussi les mots que l'exploitation lit
  pour compléter son catalogue.
- Un `plan` avec des lignes **et** une `reply` qui dit autre chose : le
  plan gagne — c'est lui qui a été vérifié.

### 🎙️ Le VOCAL — 60 s, transcrit, MONTRÉ, puis envoyé (v4.15.0)

```
POST /ai/voice        multipart : file=<audio>        → { "text": "…" }
```

⚠️ **La transcription n'est PAS exécutée.** Elle revient à vous ; vous
l'**affichez dans le champ de saisie**, corrigible, et c'est le client qui
l'envoie à `/ai/chat`. Commander depuis un vocal non relu ferait livrer
« riz gras » à qui a dit « riz sauce ».

**Enregistrez en opus ou AAC, MONO, 16-24 kbit/s** — 60 s pèsent alors
~150 Ko. Le WAV est refusé : une minute fait 5 Mo, soit **des minutes**
d'envoi sur un réseau lent, et c'est l'envoi, pas l'IA, qui fait attendre.
Limite **2 Mio**, ~60 s.

| Étape | à montrer | 3G lent | 4G |
|---|---|---|---|
| envoi de l'audio | la barre d'envoi | 10-16 s | 1-2 s |
| transcription | « transcription… » | 1-3 s | 1-3 s |
| réponse | « l'assistant écrit… » | 2-10 s | 2-10 s |

Une seule requête en vol, pas de réessai automatique en boucle.
`413 audio_too_large` = l'application n'a pas compressé ;
`503 assistant_unavailable` = la voix n'est pas servie — **gardez le clavier
disponible**, tout marche sans elle.

### Les suggestions de l'accueil

`POST /ai/suggestions` une fois par ouverture d'écran d'accueil (mettez en
cache 10 min) ; `limit` 1-20, 5 par défaut. Elles respectent le profil
nutrition du client (§9) — une suggestion refusée pour allergie ne sort
jamais. Sans modèle, elles restent servies (règles déterministes).

| Réponse | Conduite |
|---|---|
| `200` avec `plan` | la carte de proposition, bouton « Ajouter au panier » |
| `200` sans `plan` | la bulle seule — ce n'était pas une commande |
| `200`, `reply` de repli | l'afficher, proposer la recherche, ne pas réessayer en boucle — **le `plan`, lui, peut être là quand même** : reconnaître des plats ne demande aucun modèle |
| `401` | jeton expiré — rotation (§1 bis) |
| `422` | message vide ou > 2 000 caractères |
| réseau / `5xx` | « Réessayer », une fois |

---

## 12. Notifications — **SOCLE** (sans `/food`)

```
POST   /me/devices              { token, platform, locale }
DELETE /me/devices/{token}
GET    /me/notifications?limit=&cursor=      → { items, unread, next_cursor }
POST   /me/notifications/read
POST   /me/notifications/{id}/read
```

- Déclarez le jeton FCM **à la connexion et à chaque rotation** — le système le remplace sans prévenir.
- **Retirez-le à la déconnexion** : sinon le téléphone continue de recevoir les notifications d'un compte dont son propriétaire est sorti.
- `locale` est accepté sous n'importe quelle forme (`fr`, `fr-FR`, `FR_fr`).
- Le centre de notifications est écrit **même quand aucun appareil n'est joignable** : c'est ce qui permet de retrouver ce qu'on a manqué, téléphone éteint.
- `data.order_id` ouvre la bonne commande depuis la liste **et** depuis la bannière.

---

## 13. Tombola, support, fichiers

```
GET  /tombola/draws · POST /tombola/draws/{id}/enter · GET /tombola/me
POST /tickets · GET /tickets · GET /tickets/{id} · POST /tickets/{id}/messages
POST /bug-reports
POST /uploads?kind=avatar&entity={id}    (SOCLE, sans /food — kind ∈ avatar|vehicle|dish|store|brand|feed|banner)
```

### Le SUPPORT — réclamations et objets perdus (v4.9.0)

> Les routes existaient ; l'application ne les proposait pas, et une plainte
> finissait au téléphone. Voici ce que l'écran doit faire, et le cas de
> l'**objet oublié** (dans le sac, chez le livreur), qui a son propre
> parcours.

```
POST /tickets   { category, message, order_id?, priority?, lost_item? }   → 201 ticket
```

| `category` | Quand |
|---|---|
| `order` | un problème **sur une commande** — plat manquant, froid, retard : `order_id` obligatoire |
| `lost_item` | **objet oublié** (remis au livreur par erreur, laissé dans le sac) — parcours ci-dessous |
| `payment` | débit, remboursement, mobile money |
| `account` | connexion, profil, téléphone |
| `behaviour` | comportement du livreur : `order_id` recommandé |
| `other` | le reste (`tokens` existe mais concerne livreurs et marchands) |

- `order_id` rattache la commande — **la vôtre** seulement (`403` sinon) ; le
  serveur fige `ref_label` (« Commande #A1B2C3 · 19/09 12:40 »). Depuis
  l'écran d'une commande livrée, proposez « Signaler un problème » avec
  `order_id` déjà rempli.
- `priority` est facultative : ne la demandez pas, le support la règle.
- ⚠️ `ride_id` n'existe pas ici — `422 fields: ["ride_id"], reason:
  "wrong_vertical"`.

Le ticket rendu porte `reference` (`TCK-000123`, **à afficher** — c'est ce
que le client dira au téléphone), `status` (`open` · `in_progress` ·
`waiting` · `resolved` · `closed`), `messages[]` avec `author_role` (`client`
vous, `admin` le support, `driver` le livreur sur un objet perdu — trois
bulles, jamais un nom), et pour un objet perdu le bloc `lost_item`.

**🎒 Objet perdu** : `{ "category": "lost_item", "order_id": "…", "message":
"…", "lost_item": { "item": "Clés de maison", "details": "trousseau bleu" } }`.
`order_id` et `lost_item.item` obligatoires (`422` qui les nomme), la
commande doit avoir eu un livreur (`409 no_driver_yet`), priorité `high`
d'office. **Le livreur de cette commande est prévenu à l'instant** et
répond depuis son application ; vous recevez **`lost_item_found`** ou
**`lost_item_not_found`** (données `{ type: "lost_item", ticket_id,
order_id }` — ouvrir le ticket). `lost_item.found` a trois états : `null`
(pas encore regardé), `true` (retrouvé, `note` dit où, statut `in_progress`,
**le support organise la restitution**), `false` (pas trouvé, le ticket
reste ouvert). La restitution passe par le support, jamais par un échange
de numéros dans le fil.

**Le fil** : `POST /tickets/{id}/messages` ; chaque réponse du support (ou
du livreur) vous arrive en **`ticket_reply`**, la clôture en
**`ticket_resolved`** — catégorie `support`, non coupable. Pas de socket :
relisez `GET /tickets/{id}` à l'ouverture et sur chaque notification.

⚠️ **`POST /uploads` est au socle — v3.0.0** : `…/api/v1/uploads`, plus `/food/uploads` (404). Multipart, champ `file`, le **type déclaré** de la part fait foi (jpeg, png, webp, svg ; ≤ 5 MiB). Réponse `201 { url }` : téléversez **d'abord**, rattachez l'URL ensuite (`PATCH /me { avatar_url }`) — un envoi qui échoue ne doit pas faire perdre la saisie.

Un crédit tombola en attente est appliqué **automatiquement** en remise à la commande suivante — il apparaît dans `discount`.

Un rapport de bug doit joindre `app_version`, `platform` et l'écran courant.

---

## 13 bis. Compter l'engagement — v1.5.0

```
POST /feed/{id}/click     · une ouverture de contenu
POST /feed/{id}/share     · un partage
POST /banners/{id}/click  · une ouverture de carte publicitaire
```

Trois compteurs, **204 sans corps**, publics. Ils existaient depuis le feed et n'étaient cités nulle part — c'est ce qui alimente les statistiques d'un marchand et le rendement d'un emplacement.

> **Ne les attendez jamais.** Ils répondent `204` même quand le comptage échoue : l'application est en train de naviguer, et une statistique perdue ne justifie pas d'interrompre le geste. Appelez-les **sans `await`**, et n'affichez aucune erreur.

---

## 13 ter. ⚠️ Ce que la maquette demande et que l'API ne sert pas

Relevé sur la maquette du **10 septembre 2026**. Ces écrans sont dessinés ; les données n'existent pas. À arbitrer avant de les câbler.

### ✅ La carte livreur du suivi — servie depuis la v1.6.0

L'écran `tracking` montre le livreur avec son **nom**, sa **note**, et deux actions. Tout est servi.

```jsonc
// GET /deliveries/{id} — course ATTRIBUÉE
{ "courier": { "name": "Mamadou Diallo", "phone": "+22890000456",
               "rating_avg": 4.9, "rating_count": 212,
               "vehicle": "moto · TG-4417" } }
```

> ⚠️ **DÉCISION PRODUIT.** La conception initiale prévoyait un relais téléphonique **masquant les deux numéros**. Ce relais demande un prestataire, non choisi ; l'attendre laissait le client sans aucun moyen d'atteindre celui qui a son repas. **La plateforme répond de l'éthique de ses livreurs** — c'est ce qui rend l'échange acceptable dans ce sens.

- **`rating_avg` ne s'affiche jamais sans `rating_count`** : 5,0 sur un avis et 4,6 sur deux cents ne disent pas la même chose.
- **`vehicle` est ce qu'on reconnaît dans la rue** — « moto · TG-4417 ». C'est celui de la **course**, pas celui actif aujourd'hui : le livreur a pu en changer, et le client guette celui qui vient chez lui.
- **`courier` est ABSENT tant qu'aucun livreur n'a pris la course.** C'est le cas normal au début du suivi : n'affichez pas une carte vide, affichez l'étape « recherche d'un livreur ».
- Un contact irrésolvable laisse `phone` vide **et le reste servi** : grisez le bouton **Appeler**, gardez la carte.

> Le client ne reçoit **pas** `customer` sur cette route — c'est son propre contact, il n'a rien à en faire. Chacun voit **l'autre**.

### ✅ Le remboursement d'une annulation — v1.6.0

Annuler une commande payée **rend l'argent sur le solde Dira**, quel que soit le moyen de paiement d'origine.

> ⚠️ **Y compris un paiement mobile money** : la somme ne repart **pas** vers l'opérateur, elle arrive sur le portefeuille. **Écrivez-le dans la confirmation d'annulation** — un client qui attend un virement sur son compte mobile appellera le support, et il aura raison de s'inquiéter.

Rien n'est rendu quand rien n'a été pris : commande en espèces, ou commande en ligne encore en attente de paiement.

### ✅ Le sélecteur d'opérateur — servi depuis la v1.7.0

```
GET /payments/providers   → { "items": [ { "id": "orange", "label": "Orange Money" } ] }
```

> ⚠️ **Ne codez plus la liste en dur.** C'est le **registre du serveur**, pas un catalogue d'intentions : ce qui n'est pas branché n'apparaît pas. La maquette en propose quatre ; il n'y en aura pas quatre tant que les intégrations ne sont pas faites, et en afficher quatre ferait échouer trois paiements sur quatre — au moment précis où le client vient de valider son panier.

- **Le `label` vient du serveur.** Brancher un opérateur se déploie côté serveur : ne faites pas dépendre son apparition d'une mise à jour de l'application.
- **Une liste à un seul élément** ne mérite pas de sélecteur : affichez-le directement.
- **Une liste vide** veut dire qu'aucun paiement en ligne n'est possible : proposez les espèces ou le solde Dira, et n'affichez pas un écran de paiement vide.
- `POST /payments/initiate` refuse toujours un nom inconnu (`unknown_provider`, 422) — c'est le filet, pas la source.

### ❌ « ≈ N commandes couvertes » sur la carte Dira Cash

L'écran de portefeuille dérive ce chiffre du **panier moyen réel** du client. Aucune route ne le rend.

> Le calculer côté application depuis l'historique paginé donnerait un nombre qui change selon le nombre de pages chargées. Mieux vaut ne rien afficher qu'un repère qui bouge tout seul.

### 🟡 L'heure d'arrivée

`planned_duration_s` (sur la course) est la durée **de la tournée**, calculée **une fois à la création** par le réseau routier — pas une ETA vivante qui se recalcule à mesure que le livreur avance.

> Affichez-la comme une **estimation initiale**, pas comme un compte à rebours. Un chiffre qui ne bouge pas pendant que la moto avance passe pour cassé ; un chiffre annoncé comme « estimé au départ » se lit correctement.

### ✅ Ce que le handoff croit manquant et qui existe

| Le handoff dit | En réalité |
|---|---|
| « la tombola tourne sur un mock, spécifier le service » | **`/tombola/draws`, `/tombola/me`, `…/enter` existent**, avec tirages, gains et crédit déduit d'une commande (§10) |
| « l'assistant doit rendre un plan typé, pas de la prose » | **C'est déjà le cas** — `POST /ai/chat` rend `plan.items[].dish_id` résolus contre le catalogue, et `unresolved` pour le reste. Le nom de route diffère (`/ai/chat`, pas `/assistant/intent`) |

---

## 14. Codes d'erreur à traiter nommément

| Code | HTTP | Conduite |
|---|---|---|
| `missing_token` · `invalid_token` | 401 | refresh, puis déconnexion au second échec |
| `invalid_credentials` | 401 | identifiants erronés |
| `forbidden` | 403 | rôle insuffisant |
| `insufficient_funds` | **402** | solde Dira insuffisant → proposer la recharge |
| `*_not_found` | 404 | `order_`, `store_`, `dish_`, `address_`, `profile_`, `reminder_` |
| `invalid_transition` | 409 | transition interdite |
| `cannot_cancel` | 409 | trop tard pour annuler |
| `dish_unavailable` | 409 | plat indisponible dans ce point de vente |
| `no_store_nearby` | **404** | l'enseigne ne livre pas à cette adresse → **changer d'enseigne ou d'adresse** |
| `dish_unavailable_nearby` | 409 | elle livre, mais aucune boutique proche n'a **ce plat** → **proposer autre chose de la même enseigne** |
| `no_driver_yet` | 409 | écrire avant qu'un livreur ait pris la course |
| `conversation_closed` | 409 | conversation fermée — **désactiver la saisie** |
| `order_not_delivered` | 409 | trop tôt pour noter |
| `already_rated` | 409 | cible déjà notée |
| `too_many_addresses` | 409 | 20 au maximum |
| `wallet_unavailable` | 409 | paiement au portefeuille indisponible |
| `phone_taken` | 409 | le numéro a déjà un compte → **proposer la connexion** |
| `account_suspended` | 403 | compte suspendu, mot de passe correct → le dire tel quel |
| `validation_failed` | 422 | **`fields`** nomme les clés JSON fautives ; `reason: unknown_field` = bug de l'app |
| `payload_too_large` | **413** | la passerelle : corps > 64 MiB — vérifier le poids **avant** d'envoyer |
| `storage_unavailable` | 503 | le stockage de fichiers n'a pas démarré → réessayer plus tard, ne pas perdre la saisie |

> ⚠️ **`no_store_nearby` et `dish_unavailable_nearby` ne se traitent pas pareil.** Le premier dit que l'enseigne entière est hors de portée : proposer un autre plat de cette enseigne enverrait le client réessayer indéfiniment. Le second dit que l'enseigne livre bien ici — c'est ce plat-ci qui manque, et le reste de la carte est commandable.

---

## 15. Points ouverts

1. ~~**Aucun appel masqué** client ↔ livreur.~~ **Tranché en v1.6.0** : chacun voit le numéro de l'autre sur une course attribuée. Pas de relais masqué — la plateforme répond de l'éthique de ses livreurs. Un relais reste possible plus tard sans rupture, en vidant `phone`.
2. **Aucun moyen de paiement enregistré** au sens strict : seul l'opérateur est mémorisé, jamais un jeton de paiement.
3. **Aucune modération** de la conversation ni des avis, **aucune purge**.
4. **Pas de notification push hors socket** pour un client application fermée si aucun appareil n'est déclaré.
5. **Le VTC n'est pas couvert** par cette API. Développement ultérieur.
