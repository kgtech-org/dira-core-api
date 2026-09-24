# Specs frontend — par rôle

> **Version 4.27.0** · 24 septembre 2026 · APIs `dira-core-api` + `dira-food-api` + `dira-vtc-api`

**Cinq** documents, un par application. Chacun est **autonome** : tout ce qu'un frontend doit savoir pour son rôle, sans avoir à ouvrir les vingt specs de modules.

| Document | Verticale | Rôle |
|---|---|---|
| [`FOOD-CLIENT.md`](FOOD-CLIENT.md) | livraison | client — découverte, commande, suivi, compte |
| [`FOOD-MERCHANT.md`](FOOD-MERCHANT.md) | livraison | marchand — enseigne, points de vente, catalogue, commandes |
| [`FOOD-DELIVERY.md`](FOOD-DELIVERY.md) | livraison | **livreur** — véhicules, appel de course, collectes, portefeuille, matériel |
| [`VTC-CLIENT.md`](VTC-CLIENT.md) | courses | client — devis, course, suivi, conversation |
| [`VTC-DRIVER.md`](VTC-DRIVER.md) | courses | **chauffeur** — véhicules, appel de 30 s, course, **dette**, matériel |

> ⚠️ **Le nom du fichier dit la VERTICALE.** « Livreur » et « chauffeur » se
> traduisent tous les deux par *driver* : un document nommé `DRIVER.md` à côté
> d'un `CHAUFFEUR.md` laissait deux équipes câbler la mauvaise base d'URL sans
> qu'aucune ligne ne les contredise. Le préfixe rend l'erreur impossible à
> commettre en silence.

Et, transverse aux cinq : [`DESIGN-BRIEF.md`](DESIGN-BRIEF.md) — **ce qui
manque aux maquettes**, l'écart entre le design et le contrat.

## ⚠️ v2.0.0 — DEUX BASES D'URL

La plateforme s'est scindée. Une seule chose change pour une application, mais elle change partout :

| Ce que vous appelez | Servi par | Base |
|---|---|---|
| connexion, profil, adresses, **envoi de fichiers**, portefeuille, paiements, notifications, **lecture** des avis | `dira-core-api` | `…/api/v1/…` — **inchangé** |
| commandes, catalogue, livraison, feed, nutrition, support | `dira-food-api` | `…/api/v1/food/…` — **nouveau préfixe** |
| courses (VTC) | `dira-vtc-api` | `…/api/v1/vtc/…` — **servi** |

`POST /api/v1/auth/login` ne bouge pas ; `GET /api/v1/food/orders` remplace `GET /api/v1/orders`.
**Prévoyez deux variables** par application : `API_BASE`, plus `FOOD_BASE` **ou**
`VTC_BASE`. Aucune application n'a besoin des deux métiers — un passager n'appelle
jamais la livraison, un chauffeur VTC n'est jamais livreur.

Le **jeton est le même** des deux côtés — même secret, même session. On ne se connecte pas deux fois.

> **Pourquoi.** Les courses sont arrivées, sur la même identité et le même portefeuille. Se connecter et payer sont le même geste quel que soit le service ; les dupliquer aurait donné deux annuaires, deux soldes, et une suspension qui ne vaut que d'un côté.

## Ce que ces documents couvrent

La maquette de septembre 2026 porte **cinq** rôles : client, livreur, marchand, **et deux de VTC** (`vtc` passager, `chauffeur`). Ses 49 écrans :

| Groupe | Écrans | État |
|---|---|---|
| client (food) | 17 | ✅ servi |
| livreur | 8 | ✅ servi |
| marchand | 11 | ✅ servi, sauf propulsion de plat et achat d'option (voir `FOOD-MERCHANT.md` §5) |
| **vtc + chauffeur** | **13** | ✅ servi, sauf conformité, notation d'une course et sécurité |

> Les écrans `vtc_*` et `ch_*` ont désormais leurs contrats : [`VTC-CLIENT.md`](VTC-CLIENT.md) et [`VTC-DRIVER.md`](VTC-DRIVER.md). Ce que l'API ne sert **pas** y est écrit noir sur blanc, section 9 et section 8 — à lire avant de câbler un écran.

Le partage de tokens de design et de composants entre les deux familles reste une bonne idée — c'est le **réseau** qui diffère, pas l'habillage.

## ⚠️ v4.0.0 — UN vocabulaire d'état pour toute la plateforme

Une application qui suit **une commande de repas et une course VTC** avait
deux écrans d'état à écrire : `assigned / delivering / delivered` d'un côté,
`accepted / approach / onboard / completed` de l'autre — pour dire, mot pour
mot, la même chose. Depuis la v4.0.0, **une opération** (une course de
livraison, une course VTC) vit dans **un seul cycle**, servi à l'identique
par les deux verticales, par le socket du suivi et par les notifications :

```
searching → accepted → picking_up → [arrived] → in_transit → completed
                                                       ↘ cancelled   (tout état avant completed)
```

| Statut | Ce que ça veut dire | Livraison (`Delivery`) | Course VTC (`Ride`) |
|---|---|---|---|
| `searching` | on cherche quelqu'un | la course attend un livreur — proposée dès que le repas est **prêt** | on appelle des chauffeurs |
| `accepted` | quelqu'un a pris l'opération | un livreur l'a acceptée, il part vers le restaurant | un chauffeur l'a prise |
| `picking_up` | il est au point de départ | la **première collecte** est faite, il en reste | il **roule vers le passager** |
| `arrived` | il est arrivé et attend (v4.14.0) | — (une course de livraison passe directement à `in_transit`) | il est **au point de départ**, le passager n'est pas encore monté ; l'attente offerte court, puis se facture à la minute |
| `in_transit` | le colis / le passager est à bord | toutes les collectes faites, en route vers le client | le passager est monté |
| `completed` | livré / déposé | remise au client | passager déposé |
| `cancelled` | fini sans être fait | commande annulée (client, marchand, exploitation) | par le passager, le chauffeur ou la plateforme |

**`arrived` (v4.14.0) n'existe que pour la course VTC**, entre `picking_up`
et `in_transit`, et il est FACULTATIF : un chauffeur qui passe directement
à `in_transit` reste accepté. Quand il signale son arrivée, le passager est
alerté (`ride_driver_arrived`) et l'attente compte : `waiting_free_min`
minutes offertes (5 par défaut), puis `waiting_per_min_xof` par minute
entamée, ajoutés au prix à la montée à bord (`waiting_fee_xof`,
ajustement `reason: waiting`). Voir `VTC-CLIENT.md` §5 et `VTC-DRIVER.md` §4.

**La commande de repas** (`Order`) garde ce qui n'existe que pour un repas —
`pending_payment → paid → preparing → ready` — puis **reprend mot pour mot**
le cycle de sa course : `accepted → picking_up → in_transit → completed`.
`ready` côté commande = `searching` côté course.

Ce qui a été **renommé** (rupture, d'où la version majeure) :

| Avant | Après | Où |
|---|---|---|
| `available` | `searching` | course de livraison |
| `assigned` | `accepted` | course de livraison, commande |
| `delivering` | `in_transit` | course de livraison, commande |
| `delivered` | `completed` | course de livraison, commande |
| `approach` | `picking_up` | course VTC |
| `onboard` | `in_transit` | course VTC |

Les **routes** n'ont pas bougé (`/deliveries/available` reste la liste des
courses à prendre), ni les **clés de gabarit** (`order_assigned`,
`order_delivered`) — ce sont des noms d'écran, pas des états. Les filtres
`?status=` prennent les nouveaux mots ; un ancien mot est **refusé** (`422`)
sur les listes de commandes et de courses de livraison, et ne trouve **rien**
sur la liste d'exploitation des courses VTC — jamais traduit en silence.

> Les données déjà en base ont été **migrées** (staging) : vous ne verrez
> jamais un ancien mot dans une réponse.

## ⚠️ v4.0.0 — Temps réel : DÉTECTER un changement, RELIRE par HTTP

Aucune trame ne porte la commande ni la course entière, et c'est voulu : une
trame perdue — socket fermé, téléphone en veille — ne doit rien casser. Le
temps réel **prévient**, HTTP **fait foi**. Le même mécanisme pour les cinq
applications :

```
signal (socket ou push)  →  GET /food/orders/{id}  ·  GET /food/deliveries/{id}  ·  GET /vtc/rides/{id}
```

Trois canaux, et chaque application en tient deux :

| Canal | Qui | Ce qui arrive | Portée |
|---|---|---|---|
| **Socket du suivi** `wss://tracking…/track/subscribe/{mission_id}` | client (commande **et** course), exploitation | `{ "type": "position", … }` et **`{ "type": "status", "status": "…" }`** à chaque changement d'état de l'opération | application ouverte, une opération à la fois — `mission_id` = `delivery_id` ou `ride_id` |
| **Socket des commandes** `wss://api…/api/v1/food/ws/orders?token=` | client, marchand, livreur, exploitation | `order_created`, `order_status { from, status }`, `order_message` | application ouverte, **toutes** les commandes qui vous concernent — le livreur y reçoit désormais l'état des commandes qu'il **porte** |
| **Push FCM** (socle, `POST /me/devices`) | tout le monde | un gabarit **et des données** : `type` (`order_status`, `ride_status`, `delivery_status`, `driver_call`, …), l'identifiant, le `status` | application fermée ou en arrière-plan |

**Le flux, dans l'ordre :**

1. **À l'ouverture d'un écran** : `GET` de la ressource. C'est l'état de
   référence — jamais ce que dit le socket.
2. **Socket ouvert** : sur `status` (suivi) ou `order_status` (commandes),
   comparez à ce que vous affichez ; si ça diffère, **`GET`** et redessinez.
   La trame porte `from` et `status` : une trame déjà appliquée s'ignore, une
   trame qui « recule » (`from` ≠ votre état) signale une trame manquée —
   relisez. Vous pouvez mettre le badge à jour **avant** la réponse, c'est
   ce que `status` est là pour permettre.
3. **Push reçu** (application en arrière-plan) : `data.type` dit quoi
   ouvrir, `data.order_id` / `ride_id` / `delivery_id` sur quoi, `data.status`
   ce qui a changé. Même geste : ouvrir l'écran, `GET`.
4. **Reconnexion** du socket (back-off 1 s → 30 s) : `GET` **immédiatement**,
   avant d'appliquer la moindre trame — tout ce qui s'est passé pendant la
   coupure n'est que dans la base.
5. **Sans socket** (refusé, réseau captif, batterie) : **sondez** la ressource
   toutes les **10 s** tant que l'opération n'est pas `completed` ou
   `cancelled`, en comparant `updated_at` ; passez à 30 s au-delà de cinq
   minutes sans changement. Ne sondez **jamais** une opération terminée.

**Ce qu'aucun canal ne garantit** : l'ordre, l'unicité, la livraison. Deux
trames pour le même passage (socket + push) sont normales — la seconde
`GET` répond la même chose. Une application qui ferait du socket sa source de
vérité verrait, un jour, une course « en route » qu'un `GET` dit terminée.

## ⚠️ v4.2.0 — Le PAYS : `X-Dira-Country`

Dira s'installe **pays par pays**, et toute donnée est bornée par pays : un
compte, une commande, une course, une enseigne, un chauffeur portent un
`country` (ISO 3166-1 alpha-2), et une application ne voit que ceux de son
pays. Trois gestes pour une application, détaillés dans chaque spec
(section *Le pays*) :

1. **Envoyer `X-Dira-Country: TG` sur chaque requête**, et lire le pays
   **retenu** dans le même en-tête de la réponse. Le pays du compte
   (dans le jeton) s'impose à un compte ordinaire ; l'en-tête ne fait
   qu'informer — et, sans jeton, il est admis s'il nomme un pays ouvert.
2. **Trouver son pays** : `POST /me/country/resolve` avec la position de
   l'appareil (`geo`), ou corps vide pour laisser l'adresse IP décider
   (`ip`). Hors zone → `supported: false`, le compte garde son pays.
   Avant l'inscription : `GET /countries` (public) et l'en-tête sur
   `POST /auth/register` ; sans en-tête, l'indicatif du téléphone décide.
3. **Rafraîchir la session** (`POST /auth/refresh`) quand la résolution
   répond `updated: true` : c'est le jeton qui borne les listes.

Côté exploitation, un compte de **direction** (`country_any` dans `GET /me`)
choisit le pays qu'il regarde par ce même en-tête ; tout autre compte de
staff voit le pays de son compte.

## Ce que ces documents remplacent

`docs/specs/*.md` reste la référence **du backend** : un module par fichier, avec ses raisons de conception. Ces trois-ci sont la référence **du frontend** : un rôle par fichier, avec les contrats.

Les deux décrivent le même système. En cas d'écart, `api/openapi.yaml` fait foi — il est validé à chaque build.

## Versionnage

`MAJEUR.MINEUR.CORRECTIF`, une seule version pour les trois documents.

- **MAJEUR** — une rupture de contrat : route retirée, champ retiré, sémantique changée.
- **MINEUR** — un ajout rétrocompatible.
- **CORRECTIF** — une correction de rédaction, sans changement d'API.

Chaque document porte la même version en en-tête, et son propre journal des changements. Un frontend qui cite « v1.0.0 » désigne un contrat précis.

## Liste de contrôle d'intégration

À cocher avant de considérer une application mobile prête. Reprise de l'ancien
`docs/MOBILE_INTEGRATION.md`, tenue à jour ici.

**Transverse**

- [ ] Client HTTP : **deux bases** par application (`/api/v1` pour le socle, `/api/v1/food` **ou** `/api/v1/vtc` pour le métier), `Accept-Language`, **`X-Dira-Country`** sur chaque requête (et `?country=` sur les WebSockets), erreurs typées **sur `code`** — jamais sur le message, qui est traduit
- [ ] Pays : `POST /me/country/resolve` au démarrage (position de l'appareil si possible), `GET /countries` avant l'inscription, **`POST /auth/refresh` quand `updated: true`**, message « pas encore disponible » quand `supported: false` — sans bloquer
- [ ] ⚠️ Un **seul** jeton pour les deux bases : ne dupliquez pas la session
- [ ] Auth : stockage sécurisé, refresh **sérialisé**, déconnexion au second échec
- [ ] Pagination par curseur générique (`items` / `next_cursor`)
- [ ] Montants en **entiers** — aucun flottant, formatage XOF à l'affichage seulement
- [ ] Rôles : masquer les parcours non autorisés **avant** l'appel
- [ ] ⚠️ Téléphones en **E.164 avec le `+`** (`+22890000000`). Sans indicatif, le socle **refuse** (`422`, `fields: ["phone"]`) — il ne devine pas de pays. Pré-remplissez `+228` là où la personne le voit ; espaces et tirets sont tolérés
- [ ] Un `422` se lit par **`fields`** : souligner **la** case nommée, jamais une bannière « vérifiez vos informations » sous un formulaire correct. `reason: unknown_field` est un **bug de l'application** — une clé que la route ne connaît pas, refusée pour que rien ne soit cru enregistré
- [ ] Fichiers : **une seule porte**, `POST /api/v1/uploads` au **socle** — `/api/v1/food/uploads` n'existe plus (404)
- [ ] Support (v4.9.0) : « Signaler un problème » et « J'ai oublié quelque chose » **depuis l'écran d'une course ou d'une commande terminée** (`ride_id` / `order_id` pré-rempli), la `reference` `TCK-…` affichée, le fil relu à l'ouverture et sur `ticket_reply` ; côté chauffeur / livreur, `lost_item_reported` ouvre le ticket et **deux boutons** répondent (`POST /tickets/{id}/lost-item`)

**Client**

- [ ] Machine à états de la commande alignée sur les statuts servis — le **vocabulaire commun** v4.0.0, le même que pour une course VTC
- [ ] Temps réel : socket **prévient**, `GET` **fait foi** — sur `order_status` / `status` / push, relire la commande (README, « Temps réel »)
- [ ] Paiement : ne **jamais** conclure sans confirmation serveur
- [ ] `store_id` omis à la commande ⇒ `delivery.geo` **obligatoire**, et le `store_id` de la **réponse** fait foi
- [ ] Carte d'enseigne (`/merchants/:id/menu`) lue avec la position de **livraison**, pas celle du téléphone
- [ ] `no_store_nearby` et `dish_unavailable_nearby` traités **différemment** (changer d'enseigne · changer de plat)
- [ ] Suivi : WebSocket de tracking (positions **et** `status`), reconnexion + repli REST ; `mission_id = delivery_id`
- [ ] Conversation : saisie désactivée sur `no_driver_yet` et `conversation_closed`, historique toujours lisible
- [ ] Compteurs d'engagement appelés **sans `await`**, sans jamais afficher d'erreur

**Livreur**

- [ ] Position émise **dès l'entrée dans le parcours** — sans elle, aucun appel n'arrive
- [ ] Socket des commandes ouvert pendant une course : `order_status: cancelled` sur une commande portée = **arrêter et relire** ; le push `delivery_cancelled` dit la même chose (v4.0.0)
- [ ] Écran d'appel monté sur la trame `call`, fermé sur `call_closed`
- [ ] Compte à rebours rendu depuis **`expires_at`**, jamais depuis une horloge locale
- [ ] Refuser **explicitement** plutôt que laisser expirer : la vague suivante part plus tôt
- [ ] `token_cost` **et** `cash_required_xof` affichés **AVANT** d'accepter
- [ ] `402 insufficient_tokens` routé vers le portefeuille, pas vers un message d'erreur
- [ ] `cash_to_collect_xof` (encaisser) jamais confondu avec `cash_required_xof` (avancer)
- [ ] `pack_size` absent affiché comme **non renseigné**, jamais comme « petit »

**Marchand**

- [ ] `capabilities` **lues**, jamais recalculées depuis le rôle
- [ ] Bouton « Refuser » masqué au-delà de `ready`, et confirmation disant que **toute** la commande est annulée
- [ ] `cash_to_collect` affiché **au moment de la validation**, pas après le retrait
- [ ] Socket des commandes ouvert sur l'écran des commandes : `order_created` = nouvelle commande, `order_status` = relire ; sinon sonder toutes les 10 s (v4.0.0)

**Passager (VTC)**

- [ ] Le **devis fait foi** : envoyer `quote_id`, jamais un montant recalculé côté application
- [ ] Compte à rebours du devis rendu depuis **`expires_at`** ; expiré, on **redemande**, on ne commande pas
- [ ] ⚠️ Majoration (`surge_bp`) **affichée avant** la commande — découverte au paiement, elle se lit comme une arnaque
- [ ] ⚠️ Le passager est **débité à la commande**, pas à l'arrivée : `402 insufficient_funds` routé vers la recharge
- [ ] Suivi par le socket de **`dira-tracking`**, `mission_id = ride_id` : positions **et** `status` ; sur `status` → `GET /rides/{id}` (v4.0.0, « Temps réel »)
- [ ] Un seul écran d'état pour la course et la commande : le **vocabulaire commun** (v4.0.0)
- [ ] Conversation : mêmes refus que la livraison (`no_driver_yet`, `conversation_closed`)
- [ ] Aucun écran ne promet ce que l'API ne sert pas — notation d'une course, sécurité (voir `VTC-CLIENT.md` §9)
- [ ] Adresses « Maison » / « Bureau » câblées sur `/me/addresses` du **socle** — `is_default` est unique, ne le doublez pas en local

**Chauffeur (VTC)**

- [ ] ⚠️ **La dette est affichée en permanence.** `balance_xof` négatif ; au plafond, **plus aucun appel n'arrive** — sans cet écran, il croit à une panne
- [ ] `online` et `status` traités comme **deux axes** : hors ligne n'est pas occupé
- [ ] Position émise dès la mise en ligne — sans elle, aucun appel
- [ ] L'appel arrive sur le socket du **suivi**, pas sur un troisième socket
- [ ] Compte à rebours de 30 s rendu depuis **`expires_at`**
- [ ] Les **quatre refus** de prise de course distingués, chacun avec son geste (voir `VTC-DRIVER.md` §4)
- [ ] Une course en cours peut être **annulée sous vous** : push `ride_cancelled_by_rider` → relire `GET /rides/{id}`, libérer l'écran (v4.0.0)
- [ ] Course en **espèces** : ce qu'il encaisse n'est pas ce qu'il gagne — la commission part en dette

**Exploitation**

- [ ] ⚠️ **`TRACKING_JWT_SECRET` renseigné dans chaque environnement déployé.** Vide, l'authentification du service de suivi est **désactivée** : n'importe qui connaissant un `delivery_id` suit la course. Le secret doit valoir **exactement** le `JWT_SECRET` de `dira-food-api`.

## Journal

### 4.27.0 — 24 septembre 2026

🧳 **La course du voyageur** (`VTC-CLIENT.md` §3, `VTC-DRIVER.md` §4).

**Une course se fait dans le pays où elle SE FAIT**, et non dans celui où
son passager s'est inscrit. Un Togolais de passage à Dakar commande
normalement.

⚠️ **Ce qui se passait avant** : le pays du COMPTE décidait de tout. Un
compte togolais à Dakar recevait « ce lieu est à 2 249 km de Lomé », et
l'application — qui ne recevait que les villes du Togo — refusait le point
**avant même d'appeler le serveur**. Écran bloqué, aucune raison lisible,
alors que Dakar est desservie depuis des mois.

Suivent le passager : villes desservies, tarifs et modes, majorations,
promotions de ville, réglages d'appel, chauffeurs appelés, facturation. Le
pays vient du **point de départ**, et il est **figé sur le devis** comme le
prix. Restent chez lui : son compte, son historique, son portefeuille.

**Deux choses à coder côté application :**

1. ⚠️ **`country` sur le devis et sur la course dit LA MONNAIE du prix.**
   Un montant est un entier dans la monnaie de son pays. Formatez avec la
   monnaie de `country`, jamais avec celle du compte — sinon c'est le bon
   nombre derrière le mauvais symbole.
2. ⚠️ **`GET /cities` rend maintenant TOUTES les villes, tous pays
   confondus**, chacune avec son `country`. Votre contrôle local fonctionne
   tel quel ; c'est la borne au pays du compte qui bloquait à tort.

**Le portefeuille, lui, ne traverse pas une monnaie** : `422
wallet_other_currency` à la confirmation, et **ce moyen-là seulement** est
refusé — proposez les espèces ou le paiement en ligne plutôt que d'annuler.
Entre deux pays de même monnaie (Togo–Sénégal, Gabon–Tchad), il marche comme
chez soi.

**Côté chauffeur**, rien ne change — sauf que le numéro du passager peut
porter un autre indicatif : composez-le tel que l'API le rend.

### 4.26.1 — 24 septembre 2026

**Rectification de la 4.26.0, publiée le jour même.** La section `app`
annonçait un `meta.open_instead` sur le refus `wrong_app`. **Ce champ
n'existe pas** : l'enveloppe d'erreur du socle ne rend que `code`,
`message`, `fields` et `reason`, et le reste ne sert qu'à composer la phrase
traduite. C'est **`error.reason`** qui nomme l'application à ouvrir.

Écrit plutôt que corrigé en silence : une spec qui promet un champ absent se
paie à l'intégration, et savoir qu'on a failli le croire vaut mieux qu'un
document propre.

### 4.26.0 — 24 septembre 2026

**Deux portes qui étaient ouvertes.** L'une laissait entrer le mauvais
compte, l'autre laissait sonner le mauvais téléphone.

**1. `app` à la connexion — `POST /auth/login`.** Un compte client se
connectait dans l'application chauffeur, et l'inverse : le mot de passe est
bon, le jeton est émis, puis chaque écran répond `403` et la personne croit
l'application cassée. La connexion déclare désormais **qui demande** —
`app: "client" | "driver" | "merchant" | "console"` — et le socle refuse un
compte étranger avec **`403 wrong_app`**, dont **`error.reason` nomme
l'application à ouvrir** (`client` · `driver` · `merchant` · `console`).
Affichez l'orientation, jamais « identifiants invalides ». ⚠️ `reason`, et
rien d'autre : l'enveloppe d'erreur du socle ne porte pas de `meta`.

⚠️ **Rétrocompatible, et c'est un choix qui vous concerne** : `app` omis ne
vérifie **rien**, pour qu'une version pas encore mise à jour continue de
fonctionner — mais le mauvais compte y entre encore. La porte ne se ferme
que dans les versions qui envoient le champ. ⚠️ La règle sépare des
FAMILLES : `driver` couvre le chauffeur VTC **et** le livreur (même rôle au
socle), `client` la course **et** la livraison ; deux applications d'une
même famille ne se distinguent pas entre elles. Un compte de **direction**
entre partout, volontairement — le support doit pouvoir reproduire ce qu'on
lui décrit.

**2. Hors ligne veut dire aucun appel — y compris par le véhicule**
(`VTC-DRIVER.md` §2, `FOOD-DELIVERY.md` §4). L'attribution juge le COMPTE
et le VÉHICULE. Le compte était bien filtré ; le véhicule ne regardait que
son état mécanique, jamais la disponibilité de son propriétaire — et il
décide **seul** quand une vague ne nomme aucun compte. Un chauffeur hors
ligne ou un livreur retiré pouvait donc encore être appelé. Corrigé dans
les deux verticales. Rien à coder côté application : c'est un bug à
signaler désormais, plus une tolérance.

### 4.25.0 — 23 septembre 2026

**Ajout rétrocompatible** — **une recherche épuisée finit par être
abandonnée** (`VTC-CLIENT.md` §5).

Quand le pays l'a réglé (`search_expiry_min`, **0 = jamais**, le défaut),
une recherche épuisée depuis trop longtemps est annulée par la plateforme —
`cancelled_by: "system"`, `cancelled_reason: "search_expired"` — le passager
**remboursé** et prévenu par `ride_cancelled`.

⚠️ **Ce n'est pas la fin de la recherche, c'est la fin de l'attente.** La
recherche s'arrête déjà d'elle-même et la course reste ouverte pour que le
passager relance : c'est une bonne règle, il a payé. Mais quelqu'un qui a
fermé l'application ne relancera jamais — sa course reste sur le tableau de
bord de l'exploitation, indistinguable d'une attente réelle, et son argent
reste débité.

⚠️ **Le délai se compte depuis l'ÉPUISEMENT, pas depuis la commande** :
chaque `POST /rides/{id}/relaunch` remet le compteur à zéro.

Côté application, rien de spécial à coder : c'est une annulation comme une
autre. Affichez le motif, proposez de commander à nouveau.

### 4.24.0 — 23 septembre 2026

**Ajout rétrocompatible** — **l'APPROCHE enregistrée toute seule**, et **le
KLAXON** (`VTC-DRIVER.md` § 4, `VTC-CLIENT.md` § 5).

**1. L'approche.** Au moment où le chauffeur envoie `arrived`, la plateforme
fige ce qu'il a roulé **pour venir** : `approach_duration_s` (depuis
l'**acceptation**), `approach_distance_m`, `approach_polyline`,
`approach_source`. Rien à faire côté application.

⚠️ **C'est la moitié de la course que personne ne mesurait.** Le passager la
vit entièrement — c'est son attente —, le chauffeur la roule sans être payé
pour, et aucune des deux ne pouvait la raconter. « Il a dit cinq minutes et
il en a mis vingt » ne se tranchait sur rien.

⚠️ **Conséquence à connaître :** `actual_distance_m` **ne compte plus les
kilomètres de l'approche**. La distance de la course commence là où le
passager monte. Une course de 8 km après 6 km d'approche affichait 14 km
avant cette version — un chiffre qui ne voulait rien dire, ni pour le
passager qui le lisait, ni pour les moyennes.

**2. Le klaxon.** `POST /rides/{id}/honk` (chauffeur, **seulement une fois
`arrived`**) → le passager reçoit `ride_driver_honked` : « [driver] est
devant, dans [vehicle]. Il vous cherche. »

⚠️ **Ce n'est pas l'arrivée redite.** L'arrivée est une *information* ; le
klaxon est un *appel* — il arrive plus tard, il veut dire « maintenant ».
Côté passager : **son et vibration d'urgence**. Côté chauffeur : **grisez le
bouton 60 s**, compte à rebours visible, décompte calé sur `honked_at` et
non sur un minuteur local. Le service refuse de toute façon un second klaxon
trop rapproché (`409 honk_too_soon`) — mais un bouton qui répond « non » est
un bouton cassé aux yeux de celui qui appuie.

Un bouton qui sonne fort et qu'on peut presser dix fois devient du
harcèlement en dix secondes, et c'est le passager — celui qui descend les
escaliers avec ses sacs — qui le prend.

### 4.23.0 — 23 septembre 2026

**Ajout rétrocompatible** — **l'ABONNEMENT aux courses** (`VTC-CLIENT.md`,
section 4 ter), et **une occurrence qui se saute ou se reporte**
(`VTC-CLIENT.md`, section 4 bis).

Le trajet de tous les jours — bureau, école, retour — se décrit **une fois**,
se paie **d'avance**, et coûte **moins cher**. `POST /subscriptions/estimate`
rend ce que la semaine ET le mois coûtent, côte à côte ; `POST
/subscriptions` engage ; `/pay` active. À l'activation, chaque trajet devient
une **programmation récurrente** : les abonnements ne sont pas un second
ordonnanceur, ils se posent sur celui qui existe.

⚠️ **Trois choses à ne pas rater côté application :**

1. **Les deux périodes s'affichent ENSEMBLE**, avec `saving_xof` en évidence.
   Le passager choisit en comparant ; servir une période à la fois l'oblige à
   comparer de mémoire.
2. **Le mois compte les jours RÉELS** — 23 jours ouvrés du 5 octobre au 5
   novembre, pas « quatre semaines ». C'est le nombre que quelqu'un vérifie
   avant de s'engager.
3. **Deux rappels avant chaque départ** (`alert_leads_min: [30, 1]`) : 30 min
   pour se préparer, 1 min pour renoncer. ⚠️ **Chaque rappel porte ses
   boutons** — `POST /scheduled-rides/{id}/skip-next` (« pas aujourd'hui ») et
   `/postpone` (« dans 30 min »). Un rappel sans bouton pour dire non est une
   alarme, pas un service.

`payment_method: "subscription"` se **lit** sur une course née d'un
abonnement : elle est **déjà payée**, pas d'écran de paiement à la fin. Il ne
se commande pas sur `POST /rides`.

Les conditions sont un réglage **du pays** (`GET /settings/subscription`) :
remises, plancher de courses, moments des rappels, délai avant qu'un
abonnement impayé expire. Un tarif d'abonnement se règle **pour la suite** —
les abonnements déjà vendus ne changent pas de prix.

### 4.22.0 — 22 septembre 2026

**Ajout rétrocompatible** — **le client porte enfin son pin de DESTINATION.**

`client.dest_icon_url` : le point d'**arrivée** d'une course.

⚠️ **Le client et sa destination ne sont pas le même point.** Le pin
principal marque **quelqu'un** — le passager qui attend au départ, la
personne à qui on remet une commande ; celui de destination marque un **lieu**
où personne n'attend encore. Les dessiner pareil oblige à lire les libellés
pour savoir lequel est lequel — sur une carte, c'est exactement ce qu'on n'a
pas le temps de faire.

Le `client` porte donc, à lui seul, **tous les points du trajet d'un
passager** : lui (principal), ses étapes (numérotés 1 à 4), son arrivée
(destination). C'est la contrepartie d'y avoir fusionné `stop` en 4.18.0.

Le marchand n'en a pas : une boutique est une **étape**, la destination de
personne. Le poser ailleurs que sur `client` est **refusé** (422) ; absent,
retombez sur `map_icon_url`.

⚠️ **Côté livraison, le dépôt garde le pin PRINCIPAL** — quelqu'un y attend.
Le pin de destination est pour les courses, où l'arrivée est un lieu.

### 4.21.0 — 22 septembre 2026

**Ajout rétrocompatible + correction de spec** — **le livreur aussi a sa vue
de navigation.**

La 4.20.0 donnait deux images de marqueur à « un véhicule », mais ne disait
pas D'OÙ elles viennent — et elles ne viennent pas du même endroit : pour un
chauffeur VTC, du **mode de véhicule** (`GET /classes`) ; pour un livreur, du
genre **`courier`** de `GET /map-markers`, qui n'avait alors pas de
`nav_icon_url`. Les specs de la livraison promettaient donc un champ que
l'API ne servait pas. Corrigé des deux côtés : le champ existe, et chaque
spec nomme sa source.

`courier.nav_icon_url` : le livreur en vue 3D à ~45°. `client` et `merchant`
n'en ont pas, et c'est voulu — ⚠️ **la distinction n'est pas « véhicule / pas
véhicule », c'est « couché sur la route / debout sur la carte »**. Un livreur
est dessiné à plat sur la chaussée et paraît écrasé dès que la caméra penche ;
un client, un marchand, une étape sont des pastilles qui se **dressent** et
gardent la même image quelle que soit l'inclinaison.

Absent : retombez sur `map_icon_url`. Envoyer une image de navigation sur un
genre qui ne bouge pas est **refusé** (422).

### 4.20.0 — 22 septembre 2026

**Ajout rétrocompatible** — **un véhicule a DEUX images de marqueur, pas
une.**

| | Quand | Comment elle est dessinée |
|---|---|---|
| `map_icon_url` | la carte à plat | **vue de dessus**, caméra à la verticale (**90°**) |
| `nav_icon_url` | la carte de **navigation**, inclinée | **vue 3D**, caméra penchée à **~45°** |

⚠️ **Une seule image pour les deux se voit.** Une carte à plat regarde le sol
à la verticale ; une carte de navigation penche la caméra vers l'horizon, et
un dessin vu de dessus y paraît **écrasé, couché sur la chaussée**.

⚠️ **Dans les deux, le nez pointe vers le haut de l'image.** 90° et 45°
décrivent la **caméra**, jamais l'orientation du véhicule dans l'image :
celle-ci ne change pas d'une vue à l'autre, et c'est ce qui permet
d'appliquer `heading` **tel quel** sans savoir laquelle on tourne.

`nav_icon_url` absent : **retombez sur `map_icon_url`** — une vue de dessus
sur une carte penchée reste lisible, l'absence de tout marqueur non.

Côté console, `PUT /admin/classes/{key}` accepte `nav_icon_url`.

### 4.19.0 — 22 septembre 2026

**Ajout rétrocompatible + précision de spec** — **une tournée se lit avant
de l'accepter**, et **un pin de véhicule pointe vers le haut**.

⚠️ **Constaté en recette** : une commande passée chez TROIS enseignes
ressemblait, dans la liste du livreur, à une course à **un seul retrait** —
les listes ne portent pas le détail des collectes, et il ne découvrait les
trois qu'après avoir accepté.

`pickups_count` est donc servi dans les listes aussi, sur les courses
(`FOOD-DELIVERY`) comme sur la commande du client
(`order.delivery.pickups_count`, `FOOD-CLIENT`). ⚠️ **Absent = « on ne sait
pas »**, jamais « aucune » : une course a toujours au moins une collecte, et
le champ ne manque que sur celles créées avant lui. Affichez-le **avant
d'accepter** ; relisez `GET /deliveries/{id}` **dès l'acceptation** pour la
tournée elle-même. Chaque collecte se confirme **séparément** — une seule
confirmation ne clôt pas la tournée, et c'est ce qui fait repartir un livreur
avec deux paquets sur trois. Côté client, `picking_up` peut **durer** : dites
« il récupère votre commande chez 3 enseignes » plutôt que de laisser une
barre qui n'avance pas.

Chaque collecte se dessine avec le **pin numéroté du marchand** à son rang
(v4.18.0) : trois pastilles identiques ne se lisent pas.

🧭 **L'orientation d'un pin de véhicule.** Les images envoyées depuis la
console sont dessinées **nez vers le haut, soit 90°**. Appliquez `heading`
**tel quel** comme rotation : pas d'offset de −90°. Si vous en avez besoin,
c'est l'image qui est mal orientée, pas le code — une correction faite dans
une application et pas dans les autres fait rouler les véhicules de côté sur
un seul écran. La règle vaut pour les **véhicules** seulement : un pin de
lieu ne tourne jamais.

### 4.18.0 — 22 septembre 2026

**⚠️ Changement de contrat** — **les pins de carte se numérotent, et `stop`
disparaît.**

`GET /map-markers` rend désormais **trois** genres et non quatre. `client` et
`merchant` portent en plus jusqu'à **quatre pins numérotés** (`numbered`,
rangs 1 à 4, toujours servis au complet même vides, avec `max_numbered`).
Une carte porte souvent plusieurs points du même genre — une commande
collectée chez trois marchands, une course qui s'arrête deux fois — et un
seul pictogramme ne dit pas dans quel ORDRE on y passe.

**Le rang est celui du PASSAGE** : la 1re collecte porte le pin 1. Au-delà
de `max_numbered`, ou sur un rang non réglé, **retombez sur le pin
principal** — c'est prévu, pas une panne.

⚠️ **`stop` A FUSIONNÉ DANS `client`.** Un arrêt de course et l'adresse d'un
client sont le même objet vu à deux moments ; les régler séparément obligeait
l'exploitation à envoyer deux fois la même image pour que la carte reste
cohérente. Les étapes d'une course se dessinent avec les pins numérotés du
client. **Une application qui lit `kind: "stop"` ne le trouvera plus** : elle
retombe alors sur son pictogramme, ce qui est le comportement prévu pour un
genre absent — mais la corriger vaut mieux.

Chaque spec dit lequel de ses points prend quel pin.

### 4.17.0 — 22 septembre 2026

**Précision de spec** — **une promotion a une enveloppe, et elle peut
s'arrêter.** Chaque opération porte désormais un budget, un nombre total
d'utilisations et un nombre d'utilisations **par personne** ; ce qu'elle
coûte est mesuré au fil de l'eau, des deux côtés.

`VTC-CLIENT.md` §3 : **`GET /promotions` annonce, le devis décide.** Une
offre listée peut ne pas s'appliquer à VOTRE course — enveloppe consommée,
droit déjà utilisé, ou remise qui ne tient plus dans ce qui reste. N'affichez
jamais un prix calculé depuis cette liste : le seul prix vrai est `fare_xof`
du devis.

`FOOD-CLIENT.md` §3 : le menu et la commande appliquent **les mêmes** règles,
donc le menu ne promet jamais une remise que la commande refuserait. Rien à
calculer côté application — mais **un prix mis en cache vieillit** : relisez
le menu à l'ouverture de l'écran de commande.

Aucune route ne change pour les applications. Côté console, les promotions
gagnent `budget_xof`, `max_uses`, `max_uses_per_user`, leurs compteurs de
consommation et `GET /admin/promotions/{id}/uses`.

### 4.16.0 — 22 septembre 2026

**Ajout rétrocompatible** — **les PROMOTIONS arrivent sur les courses.** Elles
n'existaient que pour la livraison.

`VTC-CLIENT.md` §3 : le devis porte `promo_title` et `promo_discount_xof`
quand une offre s'applique, et **`fare_xof` est DÉJÀ remisé** — ne soustrayez
rien. Nouvelle route `GET /promotions` : les offres en cours, pour une
bannière. Une seule s'applique par course, la plus avantageuse ; il n'y a pas
de cumul.

`VTC-DRIVER.md` : une course en promotion porte les mêmes champs. **Par
défaut, la remise ne coûte rien au chauffeur** — elle sort de la commission de
la plateforme, et `driver_xof` reste ce que la course vaut. Quand une
opération fait participer les chauffeurs, c'est `driver_xof` qui le dit.
`fare_xof = commission_xof + driver_xof` reste vrai dans tous les cas.

Côté console : `GET|POST /admin/promotions`, `PATCH|DELETE
/admin/promotions/{id}` (portées `global`, `class`, `city` ; plafond de remise
et prix plancher).

### 4.15.2 — 22 septembre 2026

**Correction** — `GET /orders/{id}` sert enfin **la course** dans
`delivery` : `id` (la mission du suivi), `tracking_mission_id`, `status`,
`courier`, `planned_route`, `planned_distance_m`, `planned_duration_s`.

⚠️ La 4.15.1 les décrivait déjà ; **l'API ne les servait pas.** `delivery`
ne portait que `{address, geo, fee}`, et aucune route cliente ne remontait de
la commande vers sa course — l'écran de suivi n'avait donc aucun `mission_id`
à qui s'abonner. La jointure existait dans l'autre sens seulement (une course
porte son `order_id`). Rien à changer côté application : ce que §5 décrit est
maintenant vrai.

Servis sur **`GET /orders/{id}` uniquement**, jamais dans les listes — une
liste en ferait une lecture par ligne, et personne ne suit dix livreurs à la
fois. Absents tant qu'aucune course n'existe : c'est l'état « nous cherchons
un livreur », pas une panne.

### 4.15.1 — 22 septembre 2026

**Précision de spec** — `FOOD-CLIENT.md` §5 dit maintenant **comment suivre
le livreur en direct dès qu'il accepte** : le moment exact (`accepted`, et
les trois chemins par lesquels on l'apprend), ce que la commande porte alors
(`delivery.id` = la mission, `delivery.courier`, la route prévue), l'ouverture
du socket **avec le jeton** (`?token=` quand l'en-tête est impossible, et
rouvrir à la rotation), ce qu'on dessine à chaque état, et comment tenir la
connexion (back-off, arrière-plan, repli à 10-15 s, fermeture à la fin).
Aucune route ne change.

### 4.15.0 — 22 septembre 2026

**Ajout rétrocompatible** — **parler à l'application, et lui parler pour de
bon.**

**Les courses ont un assistant** (`POST /vtc/ai/chat`, `VTC-CLIENT.md`
§3 bis) : « je veux aller de Tokoin à l'aéroport en van » rend une phrase
et un **plan vérifié** — les lieux géocodés et bornés à la ville
desservie, les modes validés, et de vrais **devis**. Le passager confirme
avec `POST /rides { quote_id }`, comme un devis composé à la main :
**l'assistant ne commande jamais**, et le prix ne vient jamais du modèle.

**Les deux applications acceptent un VOCAL** (`POST /ai/voice`, ≤ 2 Mio,
~60 s, opus ou AAC mono ; le WAV est refusé). Il revient en **texte**, que
l'application **affiche et laisse corriger** avant de l'envoyer : une
transcription est une saisie, pas un ordre — partir à « l'aéroport » quand
quelqu'un a dit « la gare » serait pire que pas d'assistant du tout.

**Toute l'IA de la plateforme est dans un seul service** (`dira-analytics`),
y compris la transcription : aucune verticale n'embarque de fournisseur, de
clé ni de prompt. Conséquence visible : quand ce service ne répond pas,
l'assistant **dit qu'il ne peut pas répondre** au lieu de rendre une phrase
fabriquée localement (ce que faisait la livraison). Ce qui ne demande aucun
modèle continue de marcher — les suggestions filtrées par le profil, et le
**plan** d'une commande écrite.

### 4.14.0 — 22 septembre 2026

**Ajout rétrocompatible** — **le statut `arrived` de la course VTC, et
l'attente facturée.** Arrivé au point de départ, le chauffeur le signale
(`PATCH /rides/{id}/status {status: "arrived"}`) ; le passager est
**alerté aussitôt** (`ride_driver_arrived`, socket `status: arrived`) ;
`waiting_free_min` minutes sont offertes (5 par défaut), puis chaque minute
entamée coûte `waiting_per_min_xof`, réglés par mode dans la console et
**figés sur la course au devis**. À la montée à bord, `waiting_minutes` et
`waiting_fee_xof` s'ajoutent au prix (ajustement `reason: waiting`,
`ride_fare_adjusted` au passager) — débités du solde Dira, portés à la dette
si le solde ne suit pas, réglés au chauffeur en espèces. Le passage
`picking_up → in_transit` sans `arrived` reste accepté : rien à changer
pour une application qui ne signale pas l'arrivée, sinon qu'elle ne
facture pas l'attente.

**Et le TEMPS RÉEL**, par mode (`bill_actual_time`, `time_tolerance_min`,
`per_min_xof` — `GET /classes`, figés sur la course) : les minutes roulées
au-delà de la durée prévue du devis, tolérance déduite, s'ajoutent au prix
à l'arrivée (`actual_duration_s`, `extra_minutes`, `time_fee_xof`,
ajustement `reason: duration`). Cinq kilomètres en une heure ne coûtent
plus cinq kilomètres en dix minutes.

**L'assistant du client de la livraison** (`FOOD-CLIENT.md` §11) : la
section dit maintenant comment l'appeler — mono-tour, langue de la
requête, plan vérifié seul admis au panier, latence et repli.

### 4.13.0 — 21 septembre 2026

**Ajout rétrocompatible** — **les marqueurs de carte hors véhicules se
règlent depuis la console** : `GET /map-markers` (public, socle) rend
toujours quatre genres — `courier`, `client`, `merchant`, `stop` — avec
deux images facultatives chacun, `icon_url` et `map_icon_url`, comme un
mode de véhicule. Absentes, l'application garde son pictogramme. Chaque
spec dit quel genre dessine quoi sur ses cartes. *(`stop` a fusionné dans
`client` en 4.18.0.)*

### 4.12.1 — 21 septembre 2026

**Correctif** — **les listes de courses et de commandes rendent les plus
récentes d'abord**, par date de création puis identifiant : `GET /orders`
(client — rendait les plus ANCIENNES d'abord), `GET /stores/{id}/orders`
(marchand — idem, alors que cette spec promettait « la nouvelle en tête »),
`GET /rides` (passager et chauffeur — triait par identifiant, ce qui rangeait
mal un jeu de données antidaté). Le curseur ne change pas de forme
(`?cursor=<id de la dernière ligne>`).

**Ajout** — **`GET /deliveries`** pour le livreur : SES courses, en cours et
passées, les plus récentes d'abord, `?status=` à virgules comme ailleurs.
Jusqu'ici il n'avait que le pot commun (`/deliveries/available`) et la
course par identifiant.

### 4.12.0 — 21 septembre 2026

**Ajout rétrocompatible** — **la FACTURATION par pays et par métier, et la
comptabilité.** L'exploitation choisit, pour les courses et pour la
livraison, comment la plateforme se paie sur un agent — des **jetons** à
l'acceptation ou une **commission** en pourcentage (sur ce qu'elle verse,
ou portée à la dette de l'agent pour l'argent en espèces) — et **à quelle
étape le client au portefeuille est débité** (`request`, `accept`,
`start`, `complete`). Rien ne change sans réglage : courses en commission
débitées à l'acceptation, livraison en jetons débitée à la commande.

Ce que les applications voient : une commande ou une course rend
**`charge_at`** quand le débit est différé, et après `accept` un solde
insuffisant ne s'annule plus — l'impayé devient une **dette** du client,
**`debt_xof`** sur `GET /wallet`, remboursée d'office sur la prochaine
recharge. Une course en mode jetons rend **`token_cost`** au chauffeur
(`402 insufficient_tokens` à l'acceptation, pas de commission) ; une
livraison en mode commission rend `token_cost: 0` et **`commission_pct`**
(mouvements `commission`, `commission_due`, `debt_repaid` au portefeuille ;
`402 debt_over_limit` à la prise de service au-delà du plafond).
(`FOOD-CLIENT.md` §4, §9 ; `VTC-CLIENT.md` §4 ; `FOOD-DELIVERY.md` §2, §8 ;
`VTC-DRIVER.md` §3.)

Au socle, chaque mouvement de portefeuille écrit désormais son **écriture
comptable en partie double** (`GET /admin/finance/journal`, aperçu
`GET /admin/finance/overview`), et un **balayage d'intégrité** recalcule
chaque solde depuis ses mouvements, vérifie le journal et alerte le staff
(`staff_finance_alert`). Console seulement.

### 4.11.1 — 21 septembre 2026

**Sans changement de contrat** — **deux pays ouverts de plus : le Tchad
(`TD`, +235, XAF, N'Djamena) et le Gabon (`GA`, +241, XAF, Libreville).**
`GET /countries` les rend désormais d'office avec le Togo, le Sénégal et la
Guinée ; l'indicatif décide du pays du compte à l'inscription ;
`POST /me/country/resolve` les situe (frontières embarquées). Les montants y
sont en francs CFA d'Afrique centrale (`XAF`, symbole `FCFA`, 0 décimale).

### 4.11.0 — 21 septembre 2026

**Ajout rétrocompatible** — **LE MATÉRIEL : vente, location, prêt aux
agents, avec toutes les modalités de prélèvement et de remboursement.**
Le socle tient un catalogue par pays (`kind` vest · bag · phone · helmet ·
box · other, prix de vente, loyers jour/semaine/mois, caution, stock,
audiences `food` · `vtc`), des contrats (`sale` · `rental` · `loan`) avec
un **plan** entièrement paramétrable — échéancier (`upfront`,
`installments` × période, `per_period`, `none`), première échéance à N
jours, canaux de recouvrement cumulables (retenue sur gains en % et/ou
fixe, minimum laissé, plafonds jour/semaine, seulement l'échu ou en
avance ; prélèvement sur le solde, partiel ou non), retard (grâce,
pénalité fixe/%, blocage de la mise en ligne après N jours, rappel N jours
avant), caution remboursable — hérité en cascade réglages du pays →
article → contrat. Chaque contrat rend ses totaux calculés (`paid_xof`,
`outstanding_xof`, `due_xof`, `overdue_since`, `blocked`) et `standing`
les cumule.

Côté agent (rôle `driver`, socle sans `/vtc` ni `/food`) :
`GET /equipment/catalogue?vertical=`, `POST /equipment/requests` (si le
pays l'ouvre), `GET /me/equipment`, `POST /me/equipment/{id}/accept`,
`POST /me/equipment/{id}/pay` (livreurs, sur `balance_xof` ; chauffeurs :
`409 equipment_no_wallet`). **Livreurs** : retenue sur les frais de
livraison crédités et prélèvements sur le solde, mouvements
`reason: "equipment"` dans `GET /wallet/transactions`, caution rendue sur
le solde. **Chauffeurs** : retenue portée au relevé de courses
(`kind: "equipment"`, négatif ; caution rendue en positif), elle compte
dans la dette. Blocage : `PATCH /drivers/me/online` et
`PATCH /agent/availability` répondent **`402 equipment_overdue`**.
Notifications de catégorie `support` : `equipment_contract_proposed`,
`equipment_handed_over`, `equipment_due`, `equipment_charged`,
`equipment_overdue`, `equipment_blocked`, `equipment_returned` ; staff :
`staff_equipment_requested`, `staff_equipment_overdue`.
(`VTC-DRIVER.md` §2, §6, §6 bis ; `FOOD-DELIVERY.md` §2, §8, §8 bis.)

Correction au passage : la mise en ligne d'un chauffeur au-delà du plafond
de dette répond `402 debt_over_limit` (le tableau de `VTC-DRIVER.md` §2
disait `debt_limit_reached`, qui est le code de l'**acceptation** d'une
course).

### 4.10.0 — 20 septembre 2026

**Ajout rétrocompatible** — **le véhicule : couleur, description générée,
galerie de photos.** Tout véhicule rendu (courses **et** livraison) porte
`description`, générée par le serveur — *marque modèle couleur*, « Toyota
Avensis rouge » — à afficher telle quelle, jamais à saisir ; `color` se
propose au formulaire. `images[]` (8 au plus, jamais `null`) porte toutes
les photos, `photo_url` la couverture (celle choisie, sinon la première) ;
en modification, `images` remplace la galerie. La carte du chauffeur d'une
course (`driver.vehicle`) porte `description` et `photo_url`.
(`VTC-DRIVER.md` §2, `VTC-CLIENT.md` §5, `FOOD-DELIVERY.md` §2.)

### 4.9.0 — 19 septembre 2026

**Ajout rétrocompatible** — **LE SUPPORT, depuis l'application, et l'OBJET
PERDU.** Les courses n'avaient aucune porte de réclamation ; la livraison en
avait une que les écrans ne proposaient pas. Le guichet est désormais le
même des deux côtés (`pkg/support` du socle) : `POST · GET /tickets`,
`GET /tickets/{id}`, `POST /tickets/{id}/messages` sous `/vtc` **et** sous
`/food`, avec `ride_id` ou `order_id` selon la verticale, des catégories
communes (`ride`|`order`, `lost_item`, `payment`, `tokens`, `account`,
`behaviour`, `other`), `ref_label`, `author_role` sur chaque message, et
des notifications de catégorie `support` (non coupable) : `ticket_reply`,
`ticket_resolved`.

Le **cas de l'objet perdu** (`category: lost_item` + `lost_item.item`) :
le chauffeur — ou le livreur — de **cette** course est prévenu à l'instant
(`lost_item_reported`), devient partie au ticket et répond depuis son
application (`POST /tickets/{id}/lost-item { found }`) ; le passager reçoit
`lost_item_found` / `lost_item_not_found` ; `lost_item.found` a trois états
(`null` · `true` · `false`) ; la restitution passe par le support.
(`VTC-CLIENT.md` §7 ter, `VTC-DRIVER.md` §5 bis, `FOOD-CLIENT.md` §13,
`FOOD-DELIVERY.md` §9, `FOOD-MERCHANT.md` §8.)

L'équipe est alertée à chaque ouverture (`staff_ticket_opened`) et à
chaque réponse sur un objet perdu (`staff_lost_item_answered`).

⚠️ **Deux files, deux profils.** Les plaintes des courses et celles de la
livraison ne sont jamais mêlées côté serveur : chaque guichet exige la
PORTÉE de sa verticale d'un membre du staff (`vtc` ou `food`) — lire,
répondre, modifier. Un support « courses » ne voit pas la file de la
livraison, et inversement ; la console ne lui propose que la sienne
(`GET /me` rend `scopes` aux administrateurs). Ne concerne pas les
applications mobiles.

### 4.8.1 — 19 septembre 2026

**Précision, rien ne change côté serveur** — **la photo et les noms du
compte** (`VTC-DRIVER.md` §7, `FOOD-DELIVERY.md` §2). `GET /me` rend
`avatar_url`, et c'est LA photo du compte, celle que la console montre
aussi : l'app l'affiche partout où elle dessine un rond d'initiale.
`first_name` / `last_name` sont **absents** tant que la personne ne les a
pas saisis — `name` reste le nom d'affichage, rien ne se déduit de rien, et
un profil aux cases prénom / nom vides n'est pas un mélange de données.
Parcours pour changer la photo : `POST /uploads?kind=avatar` puis
`PATCH /me { avatar_url }`.

### 4.8.0 — 16 septembre 2026

**Ajout rétrocompatible** — **chaque message de conversation est poussé à
l'autre côté** (`chat_message`, `data.type: "chat_message"` +
`order_id` ou `ride_id`), courses comprises — la livraison le faisait, les
courses non. Le corps est le texte, l'expéditeur n'est jamais nommé.
(`FOOD-CLIENT.md` §6, `FOOD-DELIVERY.md` §7, `VTC-CLIENT.md` §7,
`VTC-DRIVER.md` §5.)

⚠️ **Pour recevoir une notification, l'appareil doit être enregistré**
(`POST /me/devices` avec son jeton FCM et sa plateforme) — et, sur iOS, le
projet Firebase doit porter une **clé APNs valide** : sans elle, FCM
refuse chaque envoi (`401 Invalid APNs credential`) et rien n'arrive, quel
que soit le gabarit.

### 4.7.0 — 15 septembre 2026

**Ajout rétrocompatible** — **les livreurs s'appellent comme les
chauffeurs.** Réglage d'exploitation par pays (tous en même temps ou un
par un, rayon, temps de réponse, bornes — `GET /food/settings/dispatch`),
présence (`last_seen_at`, `tracking_stale`, retrait du service après cinq
minutes de silence avec `offline_reason`), et la vague qui écarte, avant de
sonner, un livreur retiré ou un véhicule immobilisé par l'exploitation
(`FOOD-DELIVERY.md` §2, §3). Rien à changer dans l'app pour être appelé ;
à afficher : « suivi arrêté » sur `tracking_stale`, et proposer de se
redéclarer après un retrait `stale`.

### 4.6.0 — 15 septembre 2026

**Sans changement d'API** — **la nouvelle commande en temps réel chez le
marchand, le pop-up de 5 s** de la maquette mobile : un seul socket ouvert
pour toute l'application dès la connexion, `status: "paid"` comme unique
signal (jamais `pending_payment`), carte qui surgit avec `store_ids` +
`total` puis se complète par `GET`, 5 secondes, remplacement (jamais deux
cartes), badge sur l'onglet *Nouvelle*, aucun pop-up à la reconnexion
(`FOOD-MERCHANT.md` §4).

**Ajout rétrocompatible** — **le MODE du véhicule décide qui sonne.** Un
véhicule sert les courses de son mode et des modes AVANT lui dans
`GET /classes` (eco < confort < van) : une confort est appelée pour une
course eco, une eco ne l'est jamais pour une confort. Le chauffeur qui
forcerait quand même l'acceptation reçoit `409 vehicle_class_mismatch`
(`VTC-DRIVER.md` §3). Rien à changer dans les applications : c'est le
serveur qui trie — mais l'écran d'appel ne montrera plus jamais un mode
au-dessus du sien.

### 4.5.0 — 15 septembre 2026

**Ajout rétrocompatible** — les MODES (classes) portent deux images
envoyées depuis la console : `icon_url` (l'icône d'affichage) et
`map_icon_url` (l'icône du marqueur, avec `map_icon` comme silhouette de
repli), réglables avec le nom ; un mode peut s'ajouter. Affichez
`icon_url` + `name` tels que servis, jamais en dur (`VTC-CLIENT.md` §2).

### 4.4.0 — 15 septembre 2026

**Ajout rétrocompatible** — le TRAJET peut changer en cours de route.
`PATCH /rides/{id}/stops` (passager) et `PATCH /admin/rides/{id}/stops`
(console) reçoivent le nouveau trajet entier ; les arrêts déjà atteints sont
figés ; le prix est recalculé avec la majoration du devis, l'argent suit
tout de suite (différence débitée ou rendue sur le solde Dira, ou encaissée
par le chauffeur en espèces — `fare_adjustments[]` sur la course) ; un
solde insuffisant refuse le changement. Pushes `ride_stops_changed`
(chauffeur) et `ride_fare_adjusted` (passager). Voir `VTC-CLIENT.md` §5 et
`VTC-DRIVER.md` §4.

**Le CAP des véhicules** : `heading` (degrés depuis le nord vrai) est attendu
sur chaque position, avec `heading_source` — `gps` en mouvement, `compass`
(capteurs, déclinaison corrigée) à l'arrêt. Voir `VTC-DRIVER.md` §4,
`FOOD-DELIVERY.md` §6.

**Le solde Dira part à l'ACCEPTATION, plus à la commande** : `POST /rides`
en `wallet` vérifie le solde (`402 insufficient_funds` sinon) et l'appel
part sans débit ; le débit a lieu quand un chauffeur accepte. Si le solde a
fondu entre-temps, la course est annulée (`cancelled_reason:
payment_failed`, push `ride_cancelled`) et le chauffeur reçoit
`409 rider_cannot_pay`. Voir `VTC-CLIENT.md` §4, `VTC-DRIVER.md` §3.

### 4.3.0 — 15 septembre 2026

**Ajout rétrocompatible** — la MONNAIE vient du pays : `GET /countries`
porte `currency` (en vigueur — réglable depuis la console), `currency_name`,
`currency_symbol`, `currency_decimals`. Tout montant est un entier dans la
plus petite unité de la monnaie du pays où il a été créé ; rien n'est
converti, les champs `…_xof` portent la monnaie du pays. Formatez avec le
symbole et les décimales du pays courant.

### 4.2.0 — 15 septembre 2026

**Ajout rétrocompatible** — la COUCHE PAYS. En-tête `X-Dira-Country` sur
chaque requête (la réponse porte le pays retenu) ; `GET /countries`
(public, les pays ouverts — Togo, Sénégal et Guinée d'office — avec
indicatif, monnaie, langue, fuseau) ;
`POST /me/country/resolve` (position de l'appareil, puis adresse IP ; met
le compte à jour, `supported: false` hors zone) ; `country` et
`country_any` dans `GET /me` et dans le jeton (`cty`, `cty_any`). Toute
liste est bornée au pays de la requête. Une application qui n'envoie rien
continue de fonctionner dans le pays de son compte — d'où « rétrocompatible »
— mais ne saura pas où elle a été rangée.

### 4.1.1 — 15 septembre 2026

**Précisions** — un chauffeur hors ligne ne reçoit plus d'appel : la mise
hors ligne le retire du vivier sur-le-champ, et chaque vague est confirmée
par le métier avant de sonner (un chauffeur passé hors ligne ou suspendu
entre-temps est écarté). `tracking_stale` passe à **2 min** sans position
(90 s avant) — aligné sur la carte de la console, qui ne fait plus
clignoter « muet » (`VTC-DRIVER.md` §2). Passage hors ligne inchangé : 5 min.

### 4.1.0 — 15 septembre 2026

**Ajout rétrocompatible** — la politique d'appel des chauffeurs, et la fin
d'une recherche côté passager.

- **Un chauffeur en cours d'appel n'est plus appelé pour une autre course**
  (le second appel remplaçait le premier sur son écran). Un seul écran
  d'appel à la fois, dans les deux verticales.
- **Réglage d'exploitation** (console, `PUT /vtc/admin/settings/dispatch`) :
  `call_mode` `parallel` (tous les chauffeurs libres du rayon d'un coup) ou
  `sequential` (un à la fois, du plus proche), `call_radius_m`, `call_ttl_s`
  (le temps de réponse de chacun), et la **fin** : `max_drivers` et/ou
  `max_minutes`. Le compte à rebours d'un appel peut être plus court qu'avant
  quand la recherche touche à sa fin (`VTC-DRIVER.md` §3).
- **Passager** : quand la recherche s'arrête sans preneur, la course reste
  `searching` avec **`dispatch_state: exhausted`** (`calling` sinon), push
  **`ride_search_exhausted`**, et **`POST /vtc/rides/{id}/relaunch`** relance
  l'appel (`409 search_running` pendant un appel, `409 not_searching` sur une
  course prise). L'exploitation a le même geste
  (`POST /vtc/admin/rides/{id}/relaunch-search`) et l'attribution manuelle
  (`VTC-CLIENT.md` §5).

### 4.0.0 — 13 septembre 2026

**RUPTURE** — un seul vocabulaire d'état pour toute la plateforme, et le
temps réel expliqué de bout en bout. Voir les deux sections « v4.0.0 » en
tête de ce document.

- **Statuts renommés** — course de livraison : `available → searching`,
  `assigned → accepted`, `delivering → in_transit`, `delivered → completed` ;
  commande : `assigned → accepted`, `delivering → in_transit`,
  `delivered → completed` ; course VTC : `approach → picking_up`,
  `onboard → in_transit`. Les anciens mots sont **refusés** (`422`) par
  `PATCH /vtc/rides/{id}/status` et par les filtres `?status=` des commandes
  et des courses de livraison. Routes et clés de gabarit inchangées. Données
  staging migrées.
- **Course VTC en temps réel** : le socket du suivi (`/track/subscribe/{ride_id}`)
  porte désormais **`{ type: "status" }`** à chaque passage, comme pour une
  livraison ; pushs `ride_accepted` (nom et voiture), `ride_driver_on_the_way`,
  `ride_cancelled` au passager, `ride_cancelled_by_rider` au chauffeur — tous
  avec `data.type: "ride_status"`, `ride_id`, `status` (`VTC-CLIENT.md` §6,
  `VTC-DRIVER.md` §4).
- **Livreur** : le socket des commandes lui sert les `order_status` des
  commandes qu'il **porte** (annulation sous lui, en premier lieu) ; push
  `delivery_cancelled` (`data.type: "delivery_status"`) (`FOOD-DELIVERY.md`
  §7, §11). Les pushs `order_*` du client portent `data.status`.
- **Toute annulation est annoncée** — y compris celle du client, qui
  n'émettait rien : `order_status { status: cancelled }` au marchand et au
  livreur qui la portait, push `order_cancelled` au client. Une attribution
  manuelle par l'exploitation produit le même état qu'une acceptation
  (commande `accepted`, appel clos, trame `status`).
- Le flux **« un signal, un `GET` »**, la reconnexion et le sondage de repli
  sont écrits une fois (`README`, « Temps réel ») et rappelés dans chaque
  document : `FOOD-CLIENT.md` §8, `FOOD-MERCHANT.md` §4, `FOOD-DELIVERY.md`
  §11, `VTC-CLIENT.md` §6, `VTC-DRIVER.md` §4.

### 3.8.0 — 13 septembre 2026

**Ajout rétrocompatible** — la note et le pourboire d'une course.

- **Noter** : `POST /vtc/rides/{id}/rating` `{ score 1..5, comment? }`, la
  même route pour les deux rôles — le passager note le chauffeur, le
  chauffeur note le passager. Une fois par partie, dans les **7 jours** ;
  chacun ne lit que **sa** note sur la course (`rating`). La moyenne du
  chauffeur est sur son profil (`rating_avg` / `rating_count`, ses avis sur
  `GET /agents/{id}/ratings`) ; celle du passager arrive aux chauffeurs
  **à l'appel** (`meta.rider_rating_avg` / `rider_rating_count`). Le passager
  reçoit `ride_rate_prompt` à l'arrivée (`VTC-CLIENT.md` §7 bis,
  `VTC-DRIVER.md` §4 bis).
- **Pourboire** : `POST /vtc/rides/{id}/tip` `{ amount_xof 100..50 000 }`,
  du **solde Dira** du passager au grand livre du chauffeur, sans commission,
  une fois par course ; `402 insufficient_funds` laisse la course sans
  pourboire (recharger, réessayer). Le chauffeur est prévenu
  (`ride_tip_received`) et le relevé porte une écriture `tip`
  (`VTC-DRIVER.md` §6). `Ride` gagne `tip_xof` / `tipped_at`.
- `GET /vtc/rides/{id}` sert au passager la **carte du chauffeur** (`driver` :
  nom, note, voiture) une fois la course prise — elle manquait.
- `rides_count` du profil chauffeur compte désormais réellement les courses
  terminées (il restait à zéro).
- **Livraison** : une course de commande **annulée** passe `cancelled` et
  quitte le pot commun (`FOOD-DELIVERY.md` §11).

### 3.7.1 — 13 septembre 2026

**Précision** — un livreur ne voit une course qu'une fois la commande
prête (`409 order_not_ready` avant). La course existe dès la confirmation
pour le direct, pas pour le pot commun (`FOOD-DELIVERY.md` §3).

### 3.7.0 — 13 septembre 2026

**Ajout rétrocompatible** — l'expiration des courses sans livreur.

- **15 min** après `ready` sans livreur, la course **expire** : retirée des
  livreurs (`409 delivery_expired` à l'acceptation), appel clos, marchand
  prévenu (`delivery_expired`). Elle n'est pas annulée : le marchand
  **relance** — `POST /food/stores/{id}/orders/{order_id}/relaunch` — et le
  délai repart (`FOOD-MERCHANT.md` §4, `FOOD-DELIVERY.md` §3).
- `Delivery` : `dispatch_state` gagne `expired` ; `dispatch_started_at`,
  `dispatch_expired_at`.

### 3.6.0 — 13 septembre 2026

**Ajout rétrocompatible** — la borne géographique d'une course.

- Le départ et l'arrivée d'une course doivent être dans la **même ville
  desservie**, à 50 km près de sa limite. Refus **au devis** :
  `422 out_of_service_area` (ville la plus proche, distance, maximum dans le
  message) · `422 different_cities`. `GET /vtc/cities` (public) donne les
  villes (centre, rayon, tolérance) pour prévenir avant le devis
  (`VTC-CLIENT.md` §3 bis).

### 3.5.0 — 12 septembre 2026

**Ajout rétrocompatible** — les courses programmées.

- **`POST /vtc/scheduled-rides`** — ponctuelle (`at`) ou récurrente
  (`recurrence` hebdomadaire, heure locale) ; `pause` / `resume` / `cancel` ;
  `runs` par occurrence. Le passager est **prévenu 5 min avant l'appel**
  (`ride_scheduled_soon`), puis `ride_scheduled_started` ou
  `ride_scheduled_failed`. La course lancée porte `schedule_id`
  (`VTC-CLIENT.md` §4 bis).

### 3.4.0 — 12 septembre 2026

**Ajout rétrocompatible** — les zones actives, et l'enchaînement.

- **`GET /analytics/zones/active?vertical=`** — le top 5 des cellules de
  3 km² les plus demandées de l'heure écoulée, avec le barycentre des
  demandes, le contour (polygone et polyline), le quartier et la part de la
  demande. À lire à l'ouverture de l'application ; une lecture, pas un
  appel (`VTC-DRIVER.md` §3, `FOOD-DELIVERY.md` §6 ter).
- **L'enchaînement des appels en fin de course** (chauffeurs VTC) : quand
  `GET /vtc/settings/dispatch` dit `chain_calls: true`, un chauffeur passager
  à bord, à moins de `chain_radius_m` de sa destination, reçoit déjà l'appel
  suivant ; la course acceptée porte `chained_from` et attend la fin de la
  première. Une seule suivante ; `driver_busy` (409) sinon
  (`VTC-DRIVER.md` §4).

### 3.3.0 — 12 septembre 2026

**Ajout rétrocompatible** — la zone rouge.

- **Push data-only `type: hot_zone`** (chauffeurs VTC et livreurs, libres et
  en ligne, à 5 km) quand plusieurs clients n'ont pas trouvé de chauffeur ou
  de livreur au même endroit en peu de temps : centre, rayon, `polyline` du
  contour, nombre de clients, `expires_at`. Une information à afficher,
  **pas un appel** — `VTC-DRIVER.md` §3, `FOOD-DELIVERY.md` §6 bis.
- **`GET /analytics/zones?vertical=`** — les zones ouvertes, à relire à
  l'ouverture ou après un message manqué. Nouveau service `dira-analytics`,
  même jeton, derrière la passerelle.

### 3.2.0 — 12 septembre 2026

**Ajout rétrocompatible** — le parcours d'une course.

- **Une course terminée garde son parcours** : `traveled_polyline` (polyline
  Google, recalée sur la route), `actual_distance_m`, `distance_source`
  (`tracked` · `planned`) sur `GET /vtc/rides/{id}` et dans l'historique —
  passager et chauffeur. Absents avant `completed` et sur une annulation.
  Le prix ne change pas : il vient du devis.
- **Côté chauffeur, c'est `mission_id` = `ride_id` sur chaque position**
  poussée au suivi, de l'acceptation à la fin, qui fait exister ce parcours
  (`VTC-DRIVER.md` §4). Sans lui : `distance_source: "planned"`, aucun tracé.

### 3.1.0 — 12 septembre 2026

**Ajout rétrocompatible** — la remédiation de l'audit de l'app chauffeur
(`2026-09-12-audit-remediation.md`, §3), côté serveur.

- **Le socket du suivi ferme proprement à l'échéance du jeton** : code
  **4401 `token_expired`**. Rafraîchir, puis reconnecter — jamais avec le même
  jeton. Poignée de main : **401** = rafraîchir, **403** = ce rôle n'a pas le
  droit, ne pas réessayer. Ping serveur toutes les 25 s, fermeture après 60 s
  de silence.
- **Durées de vie des jetons écrites** : access **15 min** (staging **10 min**),
  refresh **30 jours**, consommé à la rotation ; l'ancien access reste valide
  jusqu'à son échéance (chevauchement socket / REST normal).
- **La présence sur le profil chauffeur** (`GET /vtc/drivers/me`) :
  `last_seen_at`, **`tracking_stale`** (90 s sans position : « suivi arrêté »,
  plus appelé), `offline_at`, `offline_reason` (`driver` · **`stale`** ·
  `admin`). ⚠️ **5 min sans position = hors ligne par le serveur**, avec la
  raison — à afficher, pas à deviner.
- **`GET /vtc/drivers/me/stats?date=&tz=`** — courses, gains et temps en ligne
  du jour, dans le fuseau du chauffeur ; remplace le calcul local.
- **L'appel arrive aussi par FCM** (data-only, priorité haute, TTL = temps
  restant) : `{type: call | call_closed, call_id, ref, expires_at}`. Déclarer
  le jeton FCM par `POST /me/devices` à la connexion et à chaque rotation ;
  idempotence par `call_id`.
- `PATCH /me/preferences` : `locale` ∈ `fr` · `en` (autre : 422).
- Passerelle : `GET /api/v1/healthz` public, pour distinguer « pas de
  réseau » de « service en panne ».

### 3.0.0 — 12 septembre 2026

**Rupture — deux choses à faire tout de suite**, le reste est rétrocompatible.

- ⚠️ **`POST /uploads` est au SOCLE** : `…/api/v1/uploads`, plus
  `…/api/v1/food/uploads`, qui répond **404**. Mêmes `kind`, mêmes règles,
  même réponse. Une seule variable à changer par application.

  **Pourquoi.** L'envoi vivait dans la livraison par accident d'histoire — écrit
  dans le monolithe, resté là à la scission. Un avatar est un objet du profil,
  une photo de véhicule ou un permis servent aux deux métiers, et **les courses
  n'avaient aucune porte d'envoi** : un chauffeur VTC photographiait sa berline
  via `/food/uploads`. Une porte, une règle de poids, un bucket.
- ⚠️ **Le `+` est obligatoire dans un téléphone.** `22899000001` était
  accepté et **stocké tel quel**, à côté de `+22899000001` — deux comptes pour
  une personne, qui ne retrouvait plus le sien à la connexion. Désormais `422`
  avec `fields: ["phone"]`. Espaces, points, tirets et le `00` international
  sont tolérés et retirés : `+228 99 00 00 01` retrouve `+22899000001`.

**Ajouts.**

- **Un `422` nomme ses champs.** L'enveloppe d'erreur porte `fields` — les
  **clés JSON** en cause (`["name","password"]`) — et, quand ce n'est pas la
  valeur d'un champ, `reason` : `unknown_field` (une clé que la route ne
  connaît pas, avec son nom dans `fields`) ou `invalid_json`. Une clé inconnue
  reste **refusée**, pas ignorée : une application ne doit pas croire avoir
  enregistré ce qui a été jeté — mais elle sait maintenant laquelle. Jusqu'ici
  le message était « Requête invalide », sans plus, et l'app livreur affichait
  une bannière générique sous un formulaire correct.
- **`first_name` / `last_name` à l'inscription**, facultatifs — l'app les
  envoyait, le socle les refusait. `name` reste le nom d'affichage.
- **Le Dira Cash d'un client s'ouvre à la première lecture.** `GET /wallet`
  répond `200` (vide au besoin) à tout client — il répondait `404` à tous
  jusqu'ici, et une recharge confirmée pouvait échouer faute de portefeuille
  où la poser. Corrigé côté socle, y compris pour les comptes existants.
- **Un compte suspendu** se connecte en `403 account_suspended`, pas en
  `401` : dites-le, ne renvoyez pas vers « mot de passe oublié ».
- **Les envois de fichiers** : images ≤ 5 MiB, vidéos de feed ≤ 60 MiB et
  ≤ 60 s, `duration_seconds` rendu ; au-delà de 64 MiB la **passerelle**
  répond `413 payload_too_large`, en JSON, au même format ; `503
  storage_unavailable` si le stockage n'a pas démarré.
- **Console** : ouvrir, corriger et supprimer un compte (`POST /admin/users`,
  `PATCH · DELETE /admin/users/{id}`) — sans objet pour les applications
  mobiles.

### 2.1.0 — 10 septembre 2026

**Ajout rétrocompatible.** Les **courses** sont servies : deux applications de
plus, et rien ne change pour les trois autres.

- **La conformité passe au socle, SANS rien changer pour les applications.**
  Mêmes chemins (`/agent/documents`, `/admin/compliance`,
  `/admin/agents/{id}/documents`, `/admin/documents/{id}`), mêmes états, même
  format de `missing`, mêmes règles. Deux détails de réponse bougent, et
  uniquement côté **console** : la file de conformité rend `driver_id` au lieu
  d'`agent_id` (`user_id` est inchangé), et un compte sans fiche de livreur
  reçoit `driver_not_found` au lieu d'`agent_not_found`. **Une application
  mobile n'a rien à faire.**
- ⚠️ **LES CINQ DOCUMENTS SONT RENOMMÉS**, et c'est la seule chose à faire
  tout de suite : `CLIENT` → `FOOD-CLIENT`, `MERCHANT` → `FOOD-MERCHANT`,
  `DRIVER` → **`FOOD-DELIVERY`**, et les deux nouveaux sont `VTC-CLIENT` et
  `VTC-DRIVER`. Rien de leur contenu ne dépend de ce changement — mettez à
  jour vos liens, c'est tout.

  **Pourquoi.** « Livreur » et « chauffeur » se traduisent tous les deux par
  *driver*. Un `DRIVER.md` posé à côté d'un `CHAUFFEUR.md` laissait deux
  équipes câbler la mauvaise base d'URL — `/api/v1/food` au lieu de
  `/api/v1/vtc` — sans qu'aucune ligne des documents ne les contredise :
  l'erreur ne se serait vue qu'au premier 404. Le préfixe la rend impossible à
  commettre en silence.
- **`VTC-CLIENT.md`** et **`VTC-DRIVER.md`** — les contrats des deux rôles VTC,
  sur la base `…/api/v1/vtc/…`.
- **Le devis fait foi.** Un prix est mémorisé côté serveur et **expire en deux
  minutes** ; l'application envoie son identifiant, jamais un montant. Une
  majoration, quand elle s'applique, **doit être affichée** — découverte au
  paiement, elle se lit comme une arnaque.
- ⚠️ **Le passager est débité À LA COMMANDE**, pas à l'arrivée. `402
  insufficient_funds` route vers la recharge, pas vers un message d'erreur : un
  chauffeur qui accepte une course impayable aura roulé pour rien.
- ⚠️ **La DETTE du chauffeur est l'écran le plus important de son application.**
  Une course en espèces lui laisse l'argent et lui fait devoir la commission ;
  au-delà du plafond, **il ne reçoit plus aucun appel**. Sans affichage
  permanent, il croit à une panne.
- **L'appel de 30 s arrive par le socket du SUIVI**, celui-là même où le
  chauffeur pousse ses positions — pas un troisième socket.
- **La conversation passager ↔ chauffeur** est la même mécanique que celle de
  la livraison : mêmes règles, mêmes refus, même fenêtre de deux heures après
  l'arrivée.
- **Le carnet d'adresses est PARTAGÉ**, et il porte déjà les libellés. « Maison »
  et « Bureau » se câblent sur `/me/addresses` du socle, `is_default` compris —
  il n'y a rien à redemander. Une adresse saisie côté courses apparaît côté
  livraison : c'est le même compte.
- **Le portefeuille est de nouveau servi.** `GET /wallet`,
  `/wallet/transactions`, `POST /wallet/purchase` : ces routes annoncées à la
  v2.0.0 n'étaient montées par aucun service. Corrigé côté socle — elles
  répondent.
- ⚠️ **La connexion de l'administration se fait par E-MAIL.** Le compte
  d'administration provisionné n'en avait pas ; corrigé. Sans objet pour les
  applications mobiles, qui se connectent par téléphone.


### 2.0.0 — 10 septembre 2026

**RUPTURE DE CONTRAT.** La plateforme s'est scindée en un **socle** partagé (`dira-core-api`) et des **verticales** (`dira-food-api`, `dira-vtc-api`).

- ⚠️ **Toutes les routes de la livraison prennent le préfixe `/food`.** `GET /api/v1/orders` devient `GET /api/v1/food/orders`. Aucun champ, aucun code d'erreur, aucune sémantique ne change — **seule la base bouge**.
- ⚠️ **Les routes du socle NE bougent PAS.** Connexion, profil, carnet d'adresses, portefeuille, paiements, notifications et **lecture** des avis restent à `…/api/v1/…`. Prévoyez **deux variables de configuration**.
- Le **jeton reste unique** : même secret, même session, valable des deux côtés. Le socle n'est pas interrogé à chaque requête — la vérification est locale, ce qui évite d'en faire le point de panne unique de la plateforme.
- ⚠️ **Conséquence assumée :** suspendre un compte **ne ferme pas** les sessions en cours. Le rafraîchissement est refusé, donc la porte se referme au plus tard à l'expiration de l'accès.
- ⚠️ **Deux routes marchandes sont momentanément absentes** : `POST /stores/:id/dishes/:dish_id/boost` et `POST /stores/:id/options`. Elles dépensent un portefeuille désormais au socle sur des objets de la livraison ; le découpage est identifié, pas encore fait. **Masquez ces boutons.** Voir `FOOD-MERCHANT.md` §5.
- **Le DÉPÔT d'une note reste à la livraison** (`/food/orders/:id/rating`) — elle seule sait qu'une commande est livrée. Les **lectures** (`/stores/:id/ratings`, `/dishes/:id/ratings`, `/agents/:id/ratings`) passent au socle : les courses noteront leurs chauffeurs avec la même collection.
- **Le VTC n'est plus « non prévu »** : `dira-vtc-api` est en construction et aura sa propre spec versionnée.


### 1.7.0 — 10 septembre 2026

**Ajout rétrocompatible.** Les deux derniers manques de la passe de compatibilité sont comblés.

- **Conformité du livreur** — `GET · POST /agent/documents`, plus `GET /admin/compliance`, `GET /admin/agents/:id/documents` et `PATCH /admin/documents/:id`. Quatre pièces : permis et pièce d'identité au **livreur**, carte grise et assurance à **chaque véhicule**.
- ⚠️ **Rien n'est bloqué côté serveur.** Contrairement à ce que demande le handoff, une pièce expirée n'empêche ni l'appel ni l'acceptation : elle rend le livreur non conforme, et l'exploitation suspend. **La bannière bloquante de l'écran est la seule barrière qui existe.**
- **`GET /payments/providers`** — les opérateurs mobile money que le serveur sait **réellement** encaisser. Ne codez plus la liste en dur.


### 1.6.0 — 10 septembre 2026

**Ajout rétrocompatible**, et **deux décisions produit** qui referment deux manques relevés à la v1.5.0.

- ⚠️ **Le client voit son livreur.** `GET /deliveries/:id` sert `courier` — nom, **téléphone**, note et véhicule — sur une course **attribuée**. Le relais masquant les deux numéros demandait un prestataire non choisi ; l'attendre laissait le client sans moyen d'atteindre celui qui a son repas. **La plateforme répond de l'éthique de ses livreurs.** Le bouton **Appeler** de la maquette a désormais un numéro.
- ⚠️ **Une commande annulée est remboursée sur le PORTEFEUILLE Dira**, quel que soit le moyen de paiement — solde Dira comme mobile money. Pas de rappel chez l'opérateur : immédiat, réellement au client, dépensable sur la commande suivante.
- ⚠️ **Correctif de fond :** l'annulation **par le client** d'une commande payée ne remboursait **rien**. Trois chemins menaient à `cancelled`, un seul rendait l'argent. Le remboursement est désormais attaché à l'écriture du statut — il ne peut plus être oublié.
- `GET /deliveries/:id` sert désormais **le contact de l'autre partie**, et seulement lui : le livreur reçoit `customer`, le client reçoit `courier`, l'administration les deux.


### 1.5.0 — 10 septembre 2026

**Passe de compatibilité** sur la maquette du 10 septembre 2026 (`.resources/dira-mobile-apps`, 49 écrans). Aucune rupture ; trois routes documentées, et **ce que le design demande sans que l'API le serve** est désormais écrit dans chaque spec de rôle plutôt que découvert à l'intégration.

**Ajouts au contrat documenté** — ces routes existaient et n'étaient citées nulle part :

- `POST /feed/:id/click` · `POST /feed/:id/share` · `POST /banners/:id/click` — les compteurs d'engagement.

**Ce que la passe a confirmé comme SERVI**, contre ce que le handoff annonçait comme manquant :

- **La tombola a une API complète** (`/tombola/draws`, `/tombola/me`, `…/enter`) — le handoff la dit encore « sur un mock ».
- **L'assistant rend déjà un plan TYPÉ** (`POST /ai/chat` → `plan.items[].dish_id`), exactement le contrat que le handoff réclame sous le nom `/assistant/intent`.
- **L'appel de 30 s est entièrement côté serveur** : `expires_at`, `attempt`, cascade de vagues, `409 not_called`. Le handoff le classait « à déplacer côté serveur ».

**Ce que la passe a confirmé comme MANQUANT** — détaillé par rôle :

- Client : la **carte livreur** du suivi (nom, note) et son bouton **Appeler**, la **liste des opérateurs** mobile money, le **panier moyen** de « ≈ N commandes couvertes ».
- Marchand : le **nom du livreur** sur la carte de commande, la **progression de préparation**, et toujours le compte à rebours de 30 s.
- Livreur : les **documents de conformité** (`ag_docs`) et les statistiques `depuis`, `taux d'acceptation`, `heures en ligne`.

**Cadrage** — la maquette porte désormais **13 écrans de VTC** que cette API ne couvre pas et ne couvrira pas. Dit en tête de ce document.


### 1.4.0 — 9 septembre 2026

**Ajout rétrocompatible.** Le **dispatch marchand** : le serveur peut choisir le point de vente le plus proche du client.

- ⚠️ **`store_id` devient FACULTATIF** sur une ligne de commande. Renseigné, il **fait foi** — rien ne change pour l'app actuelle. Absent, le serveur retient le point de vente de l'enseigne le plus proche de **l'adresse de livraison** qui a réellement le plat.
- `GET /merchants/:id/menu?lat=&lng=` — la carte d'une **enseigne**, aux prix du point de vente le plus proche, en disant lequel. Le prix affiché est le prix payé.
- Deux nouveaux codes : `no_store_nearby` (l'enseigne ne livre pas ici) et `dish_unavailable_nearby` (elle livre, mais ce plat manque partout).
- ⚠️ **Correctif :** `fragile` et `pack_size` n'arrivaient jamais sur une commande — la traduction entre le catalogue et la commande ne les recopiait pas. Annoncés en v1.3.0, ils fonctionnent à partir d'ici.

### 1.3.0 — 9 septembre 2026

**Ajout rétrocompatible.** Six manques du [brief de design](DESIGN-BRIEF.md) sont comblés : les écrans qui n'avaient rien derrière eux en ont désormais.

- ⚠️ **Refuser une commande existe** — `POST /stores/:id/orders/:order_id/refuse`, jusqu'à `ready` inclus. **Le refus annule la commande ENTIÈRE**, multi-boutiques comprise. Le remboursement est automatique au portefeuille, **incomplet en paiement en ligne**.
- **Le personnel d'une enseigne** — `GET · POST /me/staff`, `PATCH · DELETE /me/staff/:id`. Des **comptes** avec deux rôles (`manager`, `staff`), affectables à un point de vente. Réservé au **propriétaire**.
- `GET /stores/:id/sales?days=30` — ce que le point de vente a vendu, par plat.
- `GET /stores/:id/orders?q=` — recherche dans les noms de plats.
- Un plat porte `fragile` et `pack_size` ; le livreur les voit **par ligne et par point de collecte**.
- Une course porte `planned_duration_s` — la durée par le réseau routier, figée à la création.

### 1.2.0 — 9 septembre 2026

**Ajout rétrocompatible**, plus un [brief de design](DESIGN-BRIEF.md) qui liste l'écart entre les maquettes et ce contrat.

- ⚠️ **Le livreur AFFECTÉ voit désormais son client** (`customer.name`, `customer.phone`) et les montants (`order_total`, `cash_to_collect_xof`). Décision produit assumée, qui revient sur la précédente. **Jamais sur `/deliveries/available`** : la ligne tenue est que seul celui qui porte la commande voit son client.
- `GET /stores/{id}/orders?status=` — les onglets marchand.
- Gabarit `merchant_new_order` : un marchand est enfin prévenu quand une commande tombe.

### 1.1.0 — 9 septembre 2026

**Ajout rétrocompatible.** Le vocabulaire de classification s'appelle désormais **tags** et étiquette aussi les **plats**, en plus des enseignes et des points de vente.

- `GET /tags` et `/admin/tags` remplacent `/store-tags` et `/admin/store-tags`, qui restent servis et **dépréciés**. Rien à changer dans l'immédiat ; le nouveau nom est le bon.
- Un plat porte `tags` — mêmes slugs, cinq au maximum.
- `GET /stores/:id/menu?tags=africain,italien` filtre la carte.

### 1.0.0 — 8 septembre 2026

Première consolidation par rôle. Couvre l'état de l'API à cette date, y compris le dispatch par appel, la conversation de commande, les notifications poussées et l'alignement sur le handoff mobile.
