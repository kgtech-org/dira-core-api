# Specs frontend — par rôle

> **Version 3.0.0** · 12 septembre 2026 · APIs `dira-core-api` + `dira-food-api` + `dira-vtc-api`

**Cinq** documents, un par application. Chacun est **autonome** : tout ce qu'un frontend doit savoir pour son rôle, sans avoir à ouvrir les vingt specs de modules.

| Document | Verticale | Rôle |
|---|---|---|
| [`FOOD-CLIENT.md`](FOOD-CLIENT.md) | livraison | client — découverte, commande, suivi, compte |
| [`FOOD-MERCHANT.md`](FOOD-MERCHANT.md) | livraison | marchand — enseigne, points de vente, catalogue, commandes |
| [`FOOD-DELIVERY.md`](FOOD-DELIVERY.md) | livraison | **livreur** — véhicules, appel de course, collectes, portefeuille |
| [`VTC-CLIENT.md`](VTC-CLIENT.md) | courses | client — devis, course, suivi, conversation |
| [`VTC-DRIVER.md`](VTC-DRIVER.md) | courses | **chauffeur** — véhicules, appel de 30 s, course, **dette** |

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

- [ ] Client HTTP : **deux bases** par application (`/api/v1` pour le socle, `/api/v1/food` **ou** `/api/v1/vtc` pour le métier), `Accept-Language`, erreurs typées **sur `code`** — jamais sur le message, qui est traduit
- [ ] ⚠️ Un **seul** jeton pour les deux bases : ne dupliquez pas la session
- [ ] Auth : stockage sécurisé, refresh **sérialisé**, déconnexion au second échec
- [ ] Pagination par curseur générique (`items` / `next_cursor`)
- [ ] Montants en **entiers** — aucun flottant, formatage XOF à l'affichage seulement
- [ ] Rôles : masquer les parcours non autorisés **avant** l'appel
- [ ] ⚠️ Téléphones en **E.164 avec le `+`** (`+22890000000`). Sans indicatif, le socle **refuse** (`422`, `fields: ["phone"]`) — il ne devine pas de pays. Pré-remplissez `+228` là où la personne le voit ; espaces et tirets sont tolérés
- [ ] Un `422` se lit par **`fields`** : souligner **la** case nommée, jamais une bannière « vérifiez vos informations » sous un formulaire correct. `reason: unknown_field` est un **bug de l'application** — une clé que la route ne connaît pas, refusée pour que rien ne soit cru enregistré
- [ ] Fichiers : **une seule porte**, `POST /api/v1/uploads` au **socle** — `/api/v1/food/uploads` n'existe plus (404)

**Client**

- [ ] Machine à états de la commande alignée sur les statuts servis, pas sur une liste recopiée
- [ ] Paiement : ne **jamais** conclure sans confirmation serveur
- [ ] `store_id` omis à la commande ⇒ `delivery.geo` **obligatoire**, et le `store_id` de la **réponse** fait foi
- [ ] Carte d'enseigne (`/merchants/:id/menu`) lue avec la position de **livraison**, pas celle du téléphone
- [ ] `no_store_nearby` et `dish_unavailable_nearby` traités **différemment** (changer d'enseigne · changer de plat)
- [ ] Suivi : WebSocket de tracking, reconnexion + repli REST ; `mission_id = delivery_id`
- [ ] Conversation : saisie désactivée sur `no_driver_yet` et `conversation_closed`, historique toujours lisible
- [ ] Compteurs d'engagement appelés **sans `await`**, sans jamais afficher d'erreur

**Livreur**

- [ ] Position émise **dès l'entrée dans le parcours** — sans elle, aucun appel n'arrive
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

**Passager (VTC)**

- [ ] Le **devis fait foi** : envoyer `quote_id`, jamais un montant recalculé côté application
- [ ] Compte à rebours du devis rendu depuis **`expires_at`** ; expiré, on **redemande**, on ne commande pas
- [ ] ⚠️ Majoration (`surge_bp`) **affichée avant** la commande — découverte au paiement, elle se lit comme une arnaque
- [ ] ⚠️ Le passager est **débité à la commande**, pas à l'arrivée : `402 insufficient_funds` routé vers la recharge
- [ ] Suivi par le socket de **`dira-tracking`**, `mission_id = ride_id`, repli REST
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
- [ ] Course en **espèces** : ce qu'il encaisse n'est pas ce qu'il gagne — la commission part en dette

**Exploitation**

- [ ] ⚠️ **`TRACKING_JWT_SECRET` renseigné dans chaque environnement déployé.** Vide, l'authentification du service de suivi est **désactivée** : n'importe qui connaissant un `delivery_id` suit la course. Le secret doit valoir **exactement** le `JWT_SECRET` de `dira-food-api`.

## Journal

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
