# App CLIENT — LIVRAISON — contrat d'API

> **Version 3.3.0** · 12 septembre 2026
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

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ; le code est le contrat.

**Un `422 validation_failed` nomme ses champs — v3.0.0.** `fields` liste les **clés JSON** en cause : soulignez **ces** cases, pas une bannière sous tout le formulaire. `reason` précise, quand ce n'est pas la valeur d'un champ : `unknown_field` (une clé que la route ne connaît pas — **refusée, pas ignorée**, son nom est dans `fields` ; c'est un bug de l'application) ou `invalid_json`.

---

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

Les neuf statuts :

```
pending_payment → paid → preparing → ready → assigned → picking_up → delivering → delivered
                                     (tout état avant delivering → cancelled)
```

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
{ "type": "status",   "status": "completed", "ts": 1757… }
```

> **`mission_id` du suivi = `delivery_id` de l'API.** C'est la clé de jointure.

Attentes : reconnexion avec back-off, **interpolation** entre deux positions, et repli sur `GET /deliveries/{id}` si le socket est indisponible.

⚠️ **La base d'URL du suivi est distincte de celle de l'API.** Deux variables d'environnement.

---

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
- Le bouton n'apparaît **qu'à partir de `assigned`**. Avant → `409 no_driver_yet`.
- La conversation se ferme **2 h après la livraison** → `409 conversation_closed`. **Désactivez la saisie sur ce refus** ; l'historique reste lisible.
- L'accusé de lecture part **après** l'affichage : marquer lu sans montrer effacerait un non-lu que personne n'a vu.

Réception immédiate sur le socket des commandes (§8). Repli : `?cursor=<dernier id reçu>` ne rend que la suite — c'est aussi le rattrapage après coupure.

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

## 11. Assistant

```
POST /ai/suggestions            → combos, respectant le profil nutrition
POST /ai/chat      { message }  → { reply, plan? }
```

```jsonc
{ "reply": "Je vous propose deux riz gras.",
  "plan": { "items": [ { "dish_id": "…", "store_id": "…", "name": "Riz gras",
                         "price": 2000, "qty": 2, "calories": 700 } ],
            "unresolved": ["caviar béluga"] } }
```

> ⚠️ **`reply` est une phrase à lire ; seul `plan` alimente le panier.** Chaque ligne du plan a été retrouvée dans le catalogue par le serveur. Une application qui commanderait depuis `reply` contournerait cette garantie.

`plan` est absent quand le message n'était pas une commande. `unresolved` nomme ce que le catalogue ignore — proposez une recherche.

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
POST /tickets · GET /tickets · POST /tickets/{id}/messages
POST /bug-reports
POST /uploads?kind=avatar&entity={id}    (SOCLE, sans /food — kind ∈ avatar|vehicle|dish|store|brand|feed|banner)
```

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
