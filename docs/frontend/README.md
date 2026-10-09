# Specs frontend — par rôle

> **Version 4.61.1** · 9 octobre 2026 · APIs `dira-core-api` + `dira-food-api` + `dira-vtc-api` + `dira-analytics`

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

### 🆕 L'APPLICATION CLIENTE UNIFIÉE — [`DIRA-CLIENT.md`](DIRA-CLIENT.md) (v4.43.0)

**Un client Dira commande à manger ET prend des taxis.** Il n'a aucune raison
d'installer deux applications pour un compte, un solde et une boîte de
notifications. `DIRA-CLIENT.md` est le contrat de **l'application qui réunit
les deux** : les quatre bases d'URL, l'identité et l'argent partagés,
**l'assistant unifié** (un champ de saisie, deux métiers), **le fil d'activité**
(courses et commandes par semaine), les **deux** canaux temps réel et la boîte
unique.

⚠️ **ELLE NE REMPLACE PAS `VTC-CLIENT` NI `FOOD-CLIENT`.** Les deux parcours
métier — composer un panier, chiffrer une course — y sont décrits en entier et
restent à jour ; les recopier dans un troisième document aurait fait deux
sources de vérité pour les mêmes routes. `DIRA-CLIENT` §10 donne l'inventaire
route par route de ce qui vit où.

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

### 🧪 Le GABON est le pays d'ESSAI (v4.38.0)

**Vous avez le feu vert pour y créer ce que vous voulez** — courses, livraisons,
resynchronisations, rattrapages de positions — sur la recette. C'est la réponse
à ce que les équipes mobiles demandaient depuis le 26 septembre : éprouver
`POST /rides/sync`, `POST /deliveries/sync` et `backfill: true` **contre le vrai
serveur**, et non contre le contrat.

| | |
|---|---|
| Pays | `GA` — `X-Dira-Country: GA`, monnaie XAF |
| Chauffeurs VTC | `+241 06 100 101`, `…102`, `…103` — mot de passe `dira12345`, `app: "driver"` |
| Livreurs | `+241 06 100 001`, `…002`, `…003` — mot de passe `dira12345`, `app: "courier"` |
| Enseignes | trois enseignes semées à Libreville, avec leur catalogue |
| Ville | Libreville — centre `[9.4500, 0.4100]` |

⚠️ **Ce qui rend ce pays SÛR, et qu'il faut savoir** : `GET /countries` le
rend avec `"testing": true`. Ce qui s'y passe **n'entre ni dans la supervision,
ni dans les rapports, ni dans les classements** — un test de charge de cinq
cents courses n'y réveille personne et ne fait partir aucun courriel annonçant
une journée record. Nous l'avons vérifié en créant dix-huit courses : les
jauges de la plateforme n'ont pas bougé d'une unité.

⚠️ **N'y mettez rien de vrai.** Une course faite au Gabon ne sera jamais payée,
jamais comptée, jamais lue par l'exploitation. Et **ne testez jamais sur un
autre pays** : le Sénégal et le Togo sont des pays réels, et une course d'essai
y déclenche les alertes qui notifient six personnes — c'est arrivé.

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
- [ ] 🚪 **`403 wrong_app`** (v4.61.1, **toutes** les applications) : `error.reason` lu, mais une valeur **inconnue** repliée sur `error.message` — jamais une phrase à trou (« Ouvrez l'application  ») ; la liste s'allonge (`courier` 4.37.0, `partner` 4.60.0) ; ⚠️ `partner` est une console **web** → **ni** bouton « ouvrir l'application », **ni** lien vers un magasin ; et **jamais** « identifiants invalides » — le mot de passe était juste
- [ ] 🔑 **Porte par code** (v4.46.0, applications de CLIENT) : compte à rebours rendu depuis `expires_at`, bouton « renvoyer » gouverné par `resend_after`, nom demandé **seulement** si `created: true`, et l'écran fonctionne **sans** `dev_code` — il disparaîtra. Les applications d'agent et de marchand gardent `POST /auth/login` (`403 otp_not_available` sinon)
- [ ] 📱 **Appareils** : `device_id` envoyé à la connexion **par toutes les applications** (il ne va pas dans le jeton d'un client, il reconnaît le même téléphone qui revient) ; un `401` au rafraîchissement se traite comme une session expirée, sans message d'erreur technique
- [ ] 💸 **Annulation** (v4.50.0, applications de CLIENT) : `cancellation` lu **avant** d'afficher le bouton ; montant dit à l'écran de confirmation quand `fee_xof > 0`, jamais un « Annuler » nu ; `why` affiché quand c'est gratuit ; compte à rebours rendu depuis `grace_left_s` et **relu à chaque rafraîchissement** (le chauffeur peut arriver avant la fin de la grâce) ; « remboursement intégral » jamais promis sur une course en **espèces** — les frais y deviennent une dette
- [ ] 📄 **COLLECTE DES PIÈCES** (v4.57.0, applications d'AGENT) : `documents_missing` **routée vers l'écran des documents**, jamais vers l'accueil ni une bannière muette ; **pas présentée comme une menace de suspension** (rien ne bloque le travail, le serveur n'a jamais bloqué) ; catégorie `support` donc **non coupable**, et rare (72 h par personne) → **pas** de bannière permanente
- [ ] 📷 **PHOTO D'UN VÉHICULE NON MOTORISÉ** (v4.57.0, LIVRAISON) : `vehicle_side` **demandée** pour un vélo — c'est le seul endroit où le non-motorisé demande *plus* (pas de plaque, donc la photo est la seule identification) ; **aucun papier** en revanche (ni carte grise, ni assurance, ni contrôle) ; `vehicle_front` **jamais** pour un vélo (« plaque lisible » est une attente qu'il ne peut pas satisfaire) ; un livreur **à pied** n'a aucune photo à envoyer
- [ ] 📦 **RETOUR SCANNÉ** (v4.57.0, applications d'AGENT) : l'écran montre **l'état, les dégâts et ce qui sera rendu de la caution AVANT** le scan — un « scanner pour rendre » nu fait accepter un montant qu'on n'a pas lu, ce qui vaut moins qu'un clic d'agent parce que ça en a l'air plus ; `POST /equipment/return` ne prend **que** le code (on accepte, on ne négocie pas — contester passe par le support) ; trois refus → trois gestes (`_unknown`/`_expired` = nouveau code, `_not_yours` = **bon écran**) ; un code de REMISE n'est pas un code de RETOUR ; `returned_via` **absent** sur un contrat rendu avant le scan → ne pas traduire par `staff`
- [ ] 🎯 **OBJECTIFS À ATTEINDRE** (v4.59.0, **toutes** les applications de CLIENT et d'AGENT) : `GET /me/challenges`, **une seule route**, le public vient du JETON ; `percent` affiché **tel quel** (déjà borné à 100 — une barre à 140 % a l'air d'un bug) ; `value` **et** `target` ensemble, jamais le pourcentage seul (« 3 sur 5 » dit qu'il en reste deux, « 60 % » ne dit rien) ; `money: true` → « 15 000 F sur 20 000 F », pas « 15 000 fois » ; `ends_at` **affiché** — sans échéance ce n'est pas un objectif ; `reached` affiché **avant** `paid_xof` (deux moments), et `pending` dit **en clair** « bonus en cours de versement » parce que le silence se lit comme un refus ; `challenge_reached` **routée vers l'écran des objectifs** et **non coupable** ; ⚠️ **ni budget, ni nombre de gagnants, ni places restantes** ne sont servis — et n'essayez pas de les récupérer : les afficher transforme un objectif en course aux places ; un objectif se gagne **une seule fois** ; une course compte **à son règlement**, pas à son acceptation ; un objectif périodique **change d'`id`** et remet son compteur à zéro chaque lundi
- [ ] 🆘 **BOUTON D'ALERTE** (v4.56.0, **toutes** les applications) : `POST /sos` appelé avec **ce qu'on a**, sans attendre un point GPS — aucun champ n'est obligatoire, pas même la position ; **réessai en boucle jusqu'à un `2xx`**, en tête de la file hors-ligne et sans attendre la fenêtre de synchronisation ; **aucune validation côté application** (pas de motif, pas de formulaire, pas de position exigée) ; bouton **laissé actif** après le premier appui (le double appui enrichit la même alerte) ; `GET /sos/me` appelé **au démarrage** (`200` + `alert: null` est le cas normal, pas un `404`) ; position poussée **toutes les 5–10 s** tant que l'alerte vit ; ⚠️ **AUCUN BOUTON D'APPEL (v4.58.0)** — ni vers les secours, ni vers nous : la route ne sert **aucun** numéro, et il ne faut en inventer aucun (ni 112, ni codé en dur). L'écran affiche `calls_back` : « le service client a été prévenu et va vous appeler » ; détections (`shake`/`crash`/`voice`) qui **proposent** via un compte à rebours de `countdown_seconds` avec **un seul** bouton « Annuler », GPS et batterie **préparés pendant** le rebours ; `confirmed: false` traité comme **plus** grave, jamais comme « envoyé par erreur » ; annulation **en un appui, sans seconde confirmation**, et dite comme « l'exploitation a été prévenue » — pas comme « effacé » ; **pas de verrou d'application** sur cet écran ; `vertical` + `ride_id`/`delivery_id` envoyés quand il y a une opération
- [ ] 🏷️ **Motif d'annulation** (v4.55.0, applications de CLIENT **et** de CHAUFFEUR) : liste **demandée** à `GET /rides/cancel-reasons`, jamais écrite en dur — elle dépend du rôle du jeton, et un code de l'autre rôle est refusé (`422`, `fields: ["reason_code"]`) ; codes **traduits chez vous** (la route sert des codes, pas des phrases) ; `reason_code` **s'ajoute** à `reason`, il ne le remplace pas ; `grave` lu dans la réponse plutôt que testé sur `code === "accident"` ; côté chauffeur, un motif grave ouvre un ticket critique → confirmation à part, « le support a été alerté », et **l'annulation aboutit même si le ticket échoue** (pas de réessai) ; `cancelled_reason_code` **absent** sur les courses d'avant la 4.55.0 → affichez `cancelled_reason` telle quelle, ne traduisez pas l'absence par `other`
- [ ] 📞 **Taux d'acceptation** (v4.55.0, application de CHAUFFEUR) : `rate` **absent** quand `called` vaut 0 → « — » ou « aucun appel reçu », **jamais 0 %** (qui se lit « il refuse tout ») ; `acceptance` **tout entier** peut manquer quand le suivi ne répond pas → masquez ou dites « indisponible », jamais zéro ; `declined` et `missed` **jamais additionnés** (un geste et une panne) ; ne recalculez pas le taux vous-même
- [ ] 📄 **PAPIERS D'UNE VOITURE DE SOCIÉTÉ** (v4.61.0, application de CHAUFFEUR) : `missing` et `missing_fleet` **jamais concaténés** — les concaténer referait le défaut qu'on vient de corriger (réclamer une carte grise à quelqu'un qui ne l'a pas) ; `missing_fleet` présenté comme une **information**, sans bouton « envoyer » ; `compliant: false` sur un manque de société **attribué à la société**, pas au chauffeur ; `by_fleet: true` **affiché** (sinon la pièce est redéposée et arbitrée deux fois) et **non remplaçable** ; `POST /driver/documents` sur une voiture de société reste **accessible** — les papiers sont souvent dans la boîte à gants
- [ ] 🏢 **VOITURE DE SOCIÉTÉ** (v4.60.0, application de CHAUFFEUR) : `offline_reason: "fleet"` traité (**quatrième valeur** — « Dira vous a mis hors ligne » devient faux une fois sur deux, une cause inconnue s'affiche **sans coupable**) ; véhicule `withdrawn` **grisé et non sélectionnable** (`PATCH /drivers/me/active-vehicle` serait refusé), et **jamais confondu avec `maintenance`** — le propriétaire reprend son bien, l'exploitation immobilise une voiture, l'interlocuteur n'est pas le même ; `vehicle_assigned` et `vehicle_taken_back` **routées vers l'écran des véhicules** et montrées **comme une notification à lire** (le rattachement ne demande pas son accord : c'est son seul contre-pouvoir) ; le message de reprise dit **« votre compte chauffeur reste actif »** — un propriétaire n'a **aucun** pouvoir sur un compte, et perdre sa voiture en passant hors ligne se lit sinon comme une exclusion de Dira
- [ ] 🧱 **Plafond de dette** (v4.55.0, application de CHAUFFEUR) : `max_debt_xof` **affiché depuis le relevé**, aucune valeur en dur — il est réglé **par pays** ; `max_debt_source` (`country` | `partner`) nommé dans le message de blocage, parce que l'interlocuteur change (l'exploitation, ou le propriétaire du véhicule) ; `max_debt_by` affiché quand il est là ; source absente traitée comme `country`
- [ ] 🔒 **Confidentialité** (v4.49.0) : **aucune** règle d'affichage en dur — ni par pays, ni par métier. `show_phone` lu **avant** d'écrire un numéro à l'écran ; `direct_call` sans `show_phone` = un bouton qui appelle et le numéro **nulle part** ; `in_app_alert` lu pour **cacher** (et non désactiver) le bouton du klaxon ; nom affiché **tel que servi**, jamais reconstruit depuis `first_name` + `last_name` ; un champ absent traité comme fermé, **sans réessai** ; véhicule et argent à encaisser toujours montrés ; tout fermé → l'écran mène à la **conversation**
- [ ] 🗑️ **Suppression de compte** (v4.47.0) : `DELETE /me` dans les applications de **CLIENT** — écran de conséquences **avant** la preuve d'identité (`password` ou `code`), **`erase_at` affiché**, « les courses et les commandes passées restent, anonymes » dit **avant** le bouton, `409 wallet_not_empty` renvoyé vers le solde. Les applications d'**agent** et de **marchand** n'affichent **pas** de bouton (`403 erasure_not_self_serve`) mais mettent « écrire au support ». **Toutes** traitent `403 account_closed` comme une fin de session **définitive** — pas comme une suspension
- [ ] 🔒 **Verrou de l'application** (v4.46.0) : `app_lock` relu à **chaque** réponse qui le porte, y compris le **rafraîchissement**, et appliqué à chaud ; code de secours toujours possible à côté de la biométrie ; au-delà de `max_attempts`, **déconnexion** (jamais blocage) ; **jamais de verrou sur un écran d'appel ni sur l'urgence**
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

### 4.61.1 — 9 octobre 2026

📱 **AUCUN CONTRAT NE CHANGE** — cette version n'ajoute que des **instructions**,
là où les deux précédentes se contentaient d'annoncer des champs. Rien à
recoder si vous avez déjà traité 4.60.0 et 4.61.0 ; tout à relire si vous vous
apprêtez à le faire.

**`VTC-DRIVER.md`** porte désormais :

- **Un tableau écran par écran** pour « mes véhicules » : ce que chaque `status`
  affiche, ce qu'il ne propose pas, et pourquoi `maintenance` et `withdrawn` ne
  se disent **pas** pareil — l'interlocuteur n'est pas le même (Dira dans un
  cas, le propriétaire du véhicule dans l'autre).
- **Les trois choses à dire, dans l'ordre**, sur l'écran d'un chauffeur dont on
  vient de reprendre la voiture : ce qui s'est passé, que ce n'est pas une
  sanction, et le geste suivant. Avec l'avertissement : **ne pas proposer « se
  remettre en ligne »** sans véhicule actif, le serveur refuse.
- **Le routage des deux notifications** (`data.type: "vehicle"`), l'obligation
  de **relire le profil** à leur arrivée, et le rappel qu'elles sont **non
  coupables** (catégorie `support`).
- **Une liste de recette** : six scénarios à essayer, dont « couper les
  notifications commerciales » et « reprendre une voiture NON active ».
- **Un tableau `missing` / `missing_fleet`** qui dit, ligne par ligne, qui agit,
  qui a un bouton, et ce qui compte dans `compliant` — avec l'interdiction
  explicite de **concaténer les deux listes**.
- **Les quatre cas d'une pièce `by_fleet`** : affichée, non remplaçable,
  `rejected` (c'est le propriétaire qui reprend la photo), `expiring` (à montrer
  quand même — c'est ce qui fait appeler son patron avant un contrôle).
- **Le dépôt par le chauffeur reste accepté** sur une voiture de société, et la
  spec dit comment le présenter : un geste discret, pas la liste par défaut.

**Toutes les specs** portent la nouvelle valeur de `error.reason` (`partner`) et
la règle qui va avec :

> ⚠️ **UNE VALEUR DE `reason` INCONNUE N'EST PAS UNE ERREUR — ET SURTOUT PAS UN
> BLANC.** La liste s'allonge (`courier` en 4.37.0, `partner` en 4.60.0) : une
> application qui traduit `reason` en nom d'application affiche « Ouvrez
> l'application  », phrase coupée. **Repli : `error.message`, déjà traduit.** Et
> `partner` **ne s'ouvre pas depuis un téléphone** — c'est une console web, donc
> ni bouton « ouvrir », ni lien vers un magasin d'applications.

**`FOOD-DELIVERY.md`** dit ce qui **n'arrivera pas** : la livraison n'a pas de
flottes privées, donc `missing_fleet` y est toujours absent et aucune pièce ne
porte `by_fleet`. Écrit pour que personne ne cherche un champ qui ne viendra
pas — et pour que la règle soit déjà connue le jour où il viendrait.

---

### 4.61.0 — 9 octobre 2026

📄 **LES PAPIERS D'UNE VOITURE DE SOCIÉTÉ SE DÉPOSENT PAR SON PROPRIÉTAIRE.**
On les réclamait à son conducteur — qui ne les a pas — et la relance
`documents_missing` le lui redisait tous les trois jours.

**Pour les applications mobiles, un seul document change encore :
`VTC-DRIVER.md`.** `GET /driver/documents` porte un champ de plus :

```
{ documents: [ { …, by_fleet: true } ], missing: [ … ], missing_fleet: [ … ], compliant }
```

> ⚠️⚠️ **`missing` A MAIGRI.** Carte grise, assurance, contrôle technique et
> photos d'un véhicule de société sont passés dans **`missing_fleet`**. Une
> application qui affiche `missing` comme une liste de choses à faire est
> désormais JUSTE — c'est tout l'objet du changement. Une application qui
> concaténerait les deux listes referait le défaut qu'on vient de corriger.

> ⚠️ **`missing_fleet` N'EST PAS UNE LISTE DE GESTES.** C'est une information :
> « votre société n'a pas encore envoyé l'assurance ». Pas de bouton
> « envoyer » à côté.

> ⚠️ **`compliant` RESTE `false`** quand il ne manque que ces pièces : la
> voiture roule sans papiers. Ne le présentez pas comme le défaut du
> chauffeur — dites qui doit agir.

> ⚠️ **`by_fleet: true`** marque une pièce déposée par le propriétaire. À
> AFFICHER (sinon on la redépose, et un opérateur regarde deux fois la même
> image), mais **sans** proposer de la remplacer.

> ⚠️ **ET LE DÉPÔT PAR LE CHAUFFEUR RESTE ACCEPTÉ**, délibérément : les papiers
> sont souvent dans la boîte à gants, et il ne doit pas être empêché de
> régulariser parce que son patron ne répond pas. `POST /driver/documents` ne
> change pas.

**LA LIVRAISON NE CHANGE PAS.** Il n'existe pas de flotte privée de motos :
`missing_fleet` y est toujours absent, et aucun livreur ne verra `by_fleet`.

---

### 4.60.0 — 9 octobre 2026

🏢 **LES PROPRIÉTAIRES DE FLOTTE ONT LEUR CONSOLE.** Une société qui possède des
voitures conduites par des chauffeurs Dira peut maintenant voir son parc,
rattacher une voiture, la reprendre, poser une limite de dette et lire ses
gains — depuis une application web à elle (`dira-partner`).

**Pour les applications mobiles, un seul document change : `VTC-DRIVER.md`.**
Rien à appeler de nouveau, mais **trois choses peuvent arriver à un chauffeur
sans qu'il ait rien fait** :

| Ce qui arrive | Ce que le téléphone reçoit |
|---|---|
| On lui rattache une voiture | `vehicle_assigned` (`data.type: "vehicle"`, `data.vehicle_id`) |
| On reprend la voiture | `vehicle_taken_back`, véhicule `status: "withdrawn"`, et **`offline_reason: "fleet"`** s'il la conduisait |
| Sa limite de dette change | rien de neuf : `max_debt_source: "partner"`, `max_debt_by` = le nom de la société |

> ⚠️ **`offline_reason` A UNE QUATRIÈME VALEUR : `fleet`.** Une application qui
> traduisait « toute cause autre que `driver`/`stale` » par « Dira vous a mis
> hors ligne » **mentira désormais une fois sur deux** — et enverra au support
> quelqu'un dont le différend est avec son propriétaire. Une cause inconnue
> s'affiche **sans coupable**.

> ⚠️ **`withdrawn` N'EST PAS `maintenance`.** Le premier est le propriétaire qui
> récupère son bien, le second l'exploitation qui immobilise une voiture. Et un
> véhicule `withdrawn` **ne se choisit plus** : n'offrez pas
> `PATCH /drivers/me/active-vehicle` dessus.

> ⚠️ **CE N'EST JAMAIS UNE SUSPENSION.** Un propriétaire n'a **aucun** pouvoir
> sur le compte d'un chauffeur — il ne peut pas le suspendre, c'était une
> décision de produit explicite. Perdre sa voiture et passer hors ligne au même
> instant se lit pourtant comme une exclusion de Dira : l'écran doit dire
> « votre compte reste actif ».

> ⚠️ **ET LE RATTACHEMENT NE DEMANDE PAS SON ACCORD** (la voiture n'est pas la
> sienne) : la notification est donc **son seul contre-pouvoir**. À montrer
> comme une notification à lire, pas comme une ligne d'historique.

**`403 wrong_app` a une valeur de plus : `partner`.** Elle ne concerne aucune
application mobile — aucune n'envoie `app: "partner"` — mais `error.reason` peut
désormais la porter, et une application qui ouvrirait une application d'après
cette valeur doit savoir qu'elle existe.

---

### 4.59.0 — 9 octobre 2026

🎯 **DES OBJECTIFS À ATTEINDRE, AVEC UN BONUS À GAGNER.** Pour les chauffeurs,
les livreurs **et** les clients. « Faites 20 courses cette semaine, gagnez
5 000 F. »

Les campagnes de notification existaient pour **informer** ; celles-ci
**récompensent**, et c'était le manque.

```
GET /me/challenges   → { items: [ { title, target, value, percent, reward_xof, ends_at, reached, pending } ] }
```

> ⚠️ **AU SOCLE**, et **une seule route pour tous les rôles** : le public vient
> du JETON. Pas de paramètre à passer — il déciderait de ce qu'on peut gagner.

---

### Ce que l'écran doit faire

⚠️ **`percent` EST DÉJÀ BORNÉ À 100.** Sept courses sur un objectif de cinq font
100 %, pas 140 % : une barre qui dépasse a l'air d'un bug, et chaque application
l'aurait bornée à sa façon.

⚠️ **`value` ET `target` ENSEMBLE, JAMAIS LE POURCENTAGE SEUL.** « 60 % » ne dit
pas quoi faire ; « 3 sur 5 » dit qu'il en reste deux. Même règle que le taux
d'acceptation (v4.55.0) : un chiffre dont on juge son avancement doit porter ce
qui le compose.

⚠️ **`reached` S'AFFICHE AVANT `paid_xof`** — ce sont deux moments. Et quand
`pending: true`, dites-le **en clair** (« bonus en cours de versement ») :
attendre l'argent pour annoncer la victoire ferait douter quelqu'un qui a compté
ses courses lui-même, et le silence se lit comme un refus.

⚠️ **`ends_at` EST CE QUI FAIT AGIR** : « 5 jours de travail » sans échéance n'est
pas un objectif.

---

### ⚠️ Ce que vous ne recevez PAS, et pourquoi

**Ni le budget, ni le nombre de gagnants, ni les places restantes.**

Ce que l'entreprise a provisionné n'est pas l'affaire de quelqu'un qui joue :
afficher « 240 000 F d'enveloppe » invite à calculer combien d'autres ont déjà
gagné, et transforme un objectif en **course aux places** — où la moitié des
gens abandonnent en se croyant trop tard.

N'essayez pas de les récupérer : aucune route ne les sert à une application.

---

### ⚠️ Trois mesures sont refusées exprès — et ce n'est pas un manque

Les **heures en ligne**, le **taux d'acceptation** et la **vitesse** sont tous
les trois mesurés par la plateforme. Aucun objectif ne peut porter dessus, et
c'est une décision :

> - **les heures** paieraient l'attente, pas le travail — et rempliraient le
>   vivier de gens qui ne veulent pas de course, dont les appels meurent sans
>   réponse ;
> - **une prime sur « ne jamais refuser »** achèterait les refus qu'on veut
>   qu'un chauffeur se permette : fatigue, départ mal éclairé, passager qui
>   l'inquiète ;
> - **payer la vitesse tue des gens**, et il n'existe pas de version prudente de
>   cette mesure.

Si une maquette en affiche un, ce n'est **pas** un oubli de l'API.

⚠️ **ET LA CIBLE EST BORNÉE PAR JOUR DE FENÊTRE** — 12 courses, 20 livraisons. Le
serveur refuse d'ÉCRIRE un objectif au-delà : une cible au volume sur une fenêtre
courte pousse à travailler fatigué. Vous ne verrez donc jamais « 100 courses en
trois jours ».

La mesure proposée en premier à l'exploitation est **« jours travaillés »** :
elle récompense la régularité, pas le volume.

---

### Les règles qui vous concernent sans être visibles

⚠️ **UN OBJECTIF SE GAGNE UNE SEULE FOIS**, même largement dépassé. Ce n'est pas
un tarif : « 3 courses = 5 000 F » répété serait une prime au volume.

⚠️ **UNE COURSE COMPTE À SON RÈGLEMENT, PAS À SON ACCEPTATION.** Sinon
l'objectif se gagnerait en acceptant puis en annulant vingt fois. Si votre écran
montre l'avancement en temps réel, attendez la fin de la course.

⚠️ **UNE COMMANDE MULTI-BOUTIQUES COMPTE POUR UNE**, même si elle donne trois
livraisons. Comptez par la route, pas par vos écrans.

⚠️ **UN OBJECTIF PÉRIODIQUE CHANGE D'`id` ET REMET SON COMPTEUR À ZÉRO.** « 20
courses cette semaine » est un objectif **neuf** chaque lundi : ne gardez pas
l'avancement de la semaine passée en cache.

⚠️ **ET LA SEMAINE COMMENCE LE LUNDI**, pas le jour du lancement. Si votre écran
affiche « cette semaine », comptez du lundi au dimanche comme le serveur le fait
— sinon vos deux nombres divergeront sans que personne ne comprenne pourquoi.


### 4.58.0 — 9 octobre 2026

☎️ **LE SERVICE CLIENT APPELLE, PAS LE TÉLÉPHONE DE LA PERSONNE.** Changement de
protocole du bouton d'alerte (v4.56.0), et il **retire** quelque chose de vos
écrans.

---

⚠️⚠️ **LES APPLICATIONS NE REÇOIVENT PLUS AUCUN NUMÉRO À COMPOSER.**
`GET /sos/settings` n'a plus de champ `numbers` : il a disparu, ce n'est pas un
oubli.

```jsonc
{ "button": true, "shake": true, "crash": true, "voice": false,
  "countdown_seconds": 10,
  "calls_back": true }       // ⚠️ CE QUE VOUS DEVEZ PROMETTRE
```

**RETIREZ LES BOUTONS D'APPEL DE VOS ÉCRANS D'URGENCE** — ni vers les secours,
ni vers nous. Et **n'en inventez aucun** : pas de 112, pas de numéro codé dans
l'application, pas de repli « au cas où », pas de liste tirée d'un site. Un
écran qui proposerait « appeler la police » enverrait quelqu'un composer un
numéro que nous ne lui avons pas donné.

⚠️ **CE QUE VOUS AFFICHEZ À LA PLACE** : « **Le service client a été prévenu et
va vous appeler.** » C'est `calls_back`.

⚠️ **ET PAS DE BOUTON « APPELER DIRA » NON PLUS.** Il ferait patienter quelqu'un
en danger dans une file d'attente téléphonique pendant qu'un opérateur essaie de
le joindre sur la même ligne.

---

### Le nouveau parcours, de bout en bout

```
la personne appuie  →  POST /sos  →  l'alerte sonne au service client
                                  →  un opérateur l'APPELLE
                                  →  s'il le faut, IL appelle les secours
```

⚠️ **POURQUOI C'EST MIEUX, ET PAS SEULEMENT DIFFÉRENT** — dites-le à votre équipe
produit, parce que la question viendra :

> - **Quelqu'un répond toujours.** Les secours d'un pays peuvent sonner dans le
>   vide : un relevé mené en Guinée en 2024 a composé les numéros officiels un
>   par un et en a trouvé plusieurs **hors service**. Un opérateur qui tombe sur
>   un numéro mort l'entend, raccroche et prend le suivant ; une personne en
>   panique, non.
> - **L'opérateur sait ce qu'il dit.** « Un chauffeur au carrefour X, voiture
>   grise immatriculée AB-1234-CD, course en cours » se transmet. Quelqu'un de
>   terrorisé ne décrit pas sa position.
> - **Le premier appel est souvent le bon** : téléphone tombé, dos-d'âne,
>   dispute déjà calmée. Appeler la police pour ça la ferait cesser de nous
>   écouter.

⚠️ **CE QUI NE CHANGE PAS, ET QUI COMPTE ENCORE PLUS MAINTENANT :**
>
> - **Le numéro de téléphone du compte doit être juste.** C'est par lui que
>   l'opérateur rappelle. Un compte sans numéro joignable est maintenant un
>   compte qu'on ne peut pas secourir : si votre application permet de le
>   changer, vérifiez-le.
> - **La note facultative sert vraiment** : l'opérateur la lit **avant**
>   d'appeler, et « un homme me suit » change ce qu'il dit au téléphone.
>   Gardez-la, et gardez-la secondaire.
> - **La position poussée toutes les 5–10 s** est ce que l'opérateur transmet
>   aux secours. C'est maintenant la donnée la plus utile de l'alerte.

---

🌍 **LES NUMÉROS DE SECOURS SONT AU CATALOGUE DE LA PLATEFORME**, au même titre
que la monnaie d'un pays — quatorze pays, police et pompiers partout, ambulance
quand les sources concordent. Ils s'affichent sur **l'écran de l'opérateur**, et
nulle part ailleurs.

Ils ne vous concernent pas directement, et c'est la raison de ce journal :
**n'essayez pas de les récupérer**. Aucune route ne les sert à une application.


### 4.57.0 — 8 octobre 2026

Trois ajouts aux applications d'**AGENT** (chauffeur, livreur). Les applications
de client et de marchand n'ont rien à changer.

---

📦 **CONCLURE LE RETOUR D'UN MATÉRIEL EN SCANNANT LE CONSTAT DU COMPTOIR.** Le
symétrique de la remise (v4.54.0) — **et pas son miroir**.

```
POST /equipment/return   { "code": "<ce que le scanner a rendu>" }
```

⚠️ **CE QUE CE SCAN PROUVE N'EST PAS CE QUE PROUVE CELUI DE LA REMISE.** À la
remise, la question était « l'article a-t-il vraiment été remis ? », parce que la
remise **démarre** l'échéancier. Au retour, elle s'inverse et se pose du côté du
**porteur** : « j'ai rendu le sac le 3, pourquoi me prélève-t-on encore le 20 ? ».

⚠️ **LE QR PORTE LE CONSTAT**, pas seulement une présence : le comptoir écrit
l'état et les dégâts, et le code vient avec. **Montrez l'état, les dégâts et ce
qui sera rendu de la caution AVANT le scan.** Un « scanner pour rendre » nu fait
accepter un montant que la personne n'a pas lu — ce qui vaut **moins** qu'un clic
d'agent, parce que ça en a l'air plus.

⚠️ **RIEN N'EST ÉCRIT TANT QUE PERSONNE N'A SCANNÉ** : un code qui expire ne
laisse aucune trace sur l'argent. C'est la différence entre « voici ce que nous
allons retenir » et « nous avons retenu ».

⚠️ **ON ACCEPTE, ON NE NÉGOCIE PAS.** La requête ne prend **que** le code — il n'y
a pas d'endroit où envoyer un constat. Contester passe par le **support**.

⚠️ **TROIS REFUS, TROIS GESTES** : `equipment_return_code_unknown` et `_expired`
→ « demandez un nouveau code » ; `equipment_return_not_yours` → **« regardez le
bon écran »** (deux porteurs au comptoir, ça arrive pour de bon).

⚠️ **UN CODE DE REMISE N'EST PAS UN CODE DE RETOUR** : scanner l'ancien échoue
proprement. N'en gardez aucun d'un geste à l'autre.

🆕 **`returned_via`** : `scan` ou `staff`. ⚠️ **Absent** sur un contrat rendu avant
que le scan existe — « on ne sait pas » n'est pas « sans preuve ».

---

📷 **UN VÉHICULE NON MOTORISÉ ATTEND SA PHOTO DE CÔTÉ** (livraison seulement).

C'est le **seul** endroit du dispositif de conformité où le non-motorisé demande
**plus**, pas moins — et la raison est qu'**un vélo n'a pas de plaque**. Pour une
moto, « AB-1234-CD » identifie l'engin ; un vélo n'a rien de tel. La photo est
alors la seule façon de dire à un client ce qu'il doit chercher dans la rue, et
la seule preuve que le véhicule déclaré existe. **L'absence de papiers n'est pas
une absence d'identité.**

⚠️ **UNE SEULE, ET C'EST LE CÔTÉ** : il porte la couleur, le cadre, le panier.
Ne demandez **pas** `vehicle_front` pour un vélo — la pièce est documentée
« plaque lisible », une attente qu'il ne peut pas satisfaire.

⚠️ **AUCUN PAPIER EN REVANCHE** : ni carte grise, ni assurance, ni contrôle
technique. Et un livreur **à pied** n'a aucune photo à envoyer : il n'y a rien à
photographier.

---

📄 **UNE RELANCE ARRIVE QUAND IL MANQUE DES PIÈCES — `documents_missing`.**

Six types de pièces ont été ajoutés en deux jours (v4.53.0 et celle-ci). Tous les
chauffeurs et livreurs déjà inscrits ont donc des pièces manquantes **sans que
personne ne leur ait demandé de les envoyer**. L'exploitation lance la collecte ;
chacun reçoit la liste de **ses** pièces, nommées en clair.

```jsonc
{ "key": "documents_missing",
  "title": "Dossier incomplet — 2 pièce(s)",
  "body":  "Il manque : Casier judiciaire, Photo de côté. Envoyez-les depuis Mon compte › Documents.",
  "data":  { "type": "documents_missing", "driver_id": "…" } }
```

⚠️ **ROUTEZ-LA VERS L'ÉCRAN DES DOCUMENTS**, pas vers l'accueil. Le message dit ce
qui manque ; il faut qu'un appui mène à l'endroit où on l'envoie.

⚠️ **CE N'EST PAS UNE MENACE DE SUSPENSION, et ne l'écrivez pas comme telle.**
Rien ne bloque le travail de quelqu'un dont le dossier est incomplet — le serveur
n'a jamais bloqué. Un écran qui annoncerait une suspension qui n'arrive pas use sa
propre crédibilité, et la prochaine alerte, celle qui compte, ne sera pas prise au
sérieux.

⚠️ **CATÉGORIE `support`, DONC NON COUPABLE** — quelqu'un qui la couperait ne
saurait jamais ce qu'on lui demande. **Et c'est pour cela qu'elle est rare** : au
plus une toutes les **72 h** par personne, même si l'exploitation relance plus
souvent. N'en faites donc pas une bannière permanente : une relance qui se répète
cesse d'être lue.


### 4.56.0 — 8 octobre 2026

🆘 **LE BOUTON D'ALERTE.** Un bouton, dans **toutes** les applications, pour
tout compte de la plateforme — passager, client, chauffeur, livreur, marchand.
**Au socle** (`…/api/v1/sos`, sans `/vtc` ni `/food`), parce que l'exploitation
doit voir les deux métiers dans la même file.

```
GET  /sos/settings      → détections, délai d'annulation, numéros du pays
POST /sos               → DÉCLENCHER
GET  /sos/me            → mon alerte en cours (200 avec `alert: null`)
POST /sos/{id}/position → où je suis MAINTENANT
POST /sos/{id}/cancel   → « fausse alerte »
```

---

⚠️⚠️ **UNE ALERTE NE SE PERD JAMAIS, ET TOUT LE RESTE EN DÉCOULE.** `POST /sos`
n'a **aucun champ obligatoire** — pas même la position. Partout ailleurs dans
cette API, refuser une requête incomplète est la bonne réponse ; ici c'est la
pire. Quelqu'un qui appuie sur ce bouton n'a pas le temps de corriger un
formulaire, et un `422` sur un appel au secours est indéfendable.

Ce que cela veut dire pour vous, concrètement :

> - **N'attendez pas un point GPS pour envoyer.** « On ne sait pas où il est »
>   est une alarme qu'un opérateur traite en premier, pas une requête à
>   compléter. Attendre un fix de dix secondes au fond d'un parking souterrain,
>   c'est perdre les dix secondes qui comptent. Envoyez, **puis** poussez la
>   position.
> - **Réessayez en boucle jusqu'à un `2xx`**, en tête de la file hors-ligne et
>   sans attendre la fenêtre de synchronisation : une alerte remise trois
>   minutes plus tard ne sert plus à personne.
> - **N'ajoutez aucune validation de votre côté** : pas de position obligatoire,
>   pas de motif à choisir, pas de formulaire à deux champs. Tout ce qui est
>   bancal est **corrigé** par le serveur (source inconnue → `button`,
>   coordonnées impossibles → position ignorée mais alerte gardée, note trop
>   longue → coupée proprement).

⚠️ **LE DOUBLE APPUI N'EST PAS UNE ERREUR, c'est le comportement de quelqu'un
qui panique.** Le serveur rend **la même** alerte et y ajoute la position — une
seule ouverte par personne. **Ne désactivez donc pas le bouton** après le
premier appui et n'affichez pas « déjà envoyé » comme une erreur : appuyer
encore *améliore* l'alerte et rassure.

⚠️ **APPELEZ `GET /sos/me` AU DÉMARRAGE, TOUJOURS.** Téléphone qui redémarre
après un choc, application tuée par le système, réseau qui revient : sans cela,
la personne ne sait plus si son alerte est partie — ou, bien pire, croit avoir
appelé alors que non. `200` **avec `alert: null`** est le cas NORMAL, pas un
`404`.

⚠️ **POUSSEZ LA POSITION TOUTES LES 5 À 10 SECONDES** tant que l'alerte vit,
plus souvent que votre cadence habituelle : un véhicule continue de rouler, et
l'opérateur a besoin de savoir où la personne **est**, pas où elle a appuyé.
C'est le seul moment du produit où la fraîcheur d'une position vaut la batterie
qu'elle coûte.

---

### ⚠️ LES DÉTECTIONS PROPOSENT, ELLES N'ENVOIENT PAS

`shake` (secouer), `crash` (choc violent) et `voice` (mot-clé) ouvrent un
**compte à rebours** de `countdown_seconds` — 10 s par défaut, réglé par pays —
avec **un seul** bouton : « Annuler ». À l'expiration, **l'alerte part** avec
`confirmed: false`.

⚠️ **POURQUOI PAS D'ENVOI DIRECT.** Un dos-d'âne, un téléphone qui tombe, un sac
qu'on pose : l'envoi direct remplirait la file de faux, l'opérateur apprendrait
à les ignorer, et la vraie alerte se noierait dedans. **Un faux positif traité
comme une vraie alerte coûte plus cher qu'un faux positif annulé.**

⚠️ **POURQUOI ELLE PART QUAND PERSONNE N'ANNULE.** C'est tout l'intérêt : après
un choc violent, **personne n'annule parce que personne ne peut**. Un compte à
rebours qui s'arrêterait sans rien envoyer serait un bouton de plus, pas une
détection.

⚠️⚠️ **`confirmed: false` EST PLUS GRAVE, PAS MOINS — et l'intuition dit
l'inverse.** Le serveur le traite comme tel : `grave: true`, et `trigger` vaut
`"choc détecté, PERSONNE N'A ANNULÉ"`. Ne le présentez jamais à la personne
comme « envoyé par erreur », et ne le rangez pas plus bas dans vos écrans.

⚠️ **PENDANT LE COMPTE À REBOURS, PRÉPAREZ TOUT** — acquisition GPS, batterie,
corps de la requête. À l'expiration, l'envoi doit partir en une milliseconde ;
un rebours qui finit sur « recherche du GPS… » a gaspillé dix secondes.

⚠️ **ET UN SEUL BOUTON PENDANT LE REBOURS : ANNULER.** Pas de champ de texte
obligatoire, pas de choix de motif, pas de liste de contacts. La note est
facultative et s'ajoute **après** l'envoi.

Ce que chacune coûte, et à qui elle sert :

| Détection | Défaut | Pour qui |
|---|---|---|
| `shake` | allumée | **la plus utile du lot** : pour le cas où on ne peut PAS regarder l'écran — poche, volant, quelqu'un à côté |
| `crash` | allumée | ceux qui conduisent. **La seule fonction du produit qui travaille quand on est inconscient** |
| `voice` | **éteinte** | elle écoute le micro **en permanence** : une permission, de la batterie, une surveillance. Ne s'allume qu'explicitement, et en l'ayant expliqué |

⚠️ **CÔTÉ CLIENT ET MARCHAND, SEULE `shake` A DU SENS** : personne n'est au
volant, et `crash` mesurerait un téléphone qui tombe du canapé ou une tablette
posée près d'une plaque de cuisson.

⚠️ **LE VOCAL SE FAIT SUR L'APPAREIL.** N'envoyez aucun flux audio à nos
serveurs — aucune route ne l'accepte.

---

### ⚠️ AUCUN NUMÉRO D'URGENCE N'EST PRÉCHARGÉ

`numbers` **peut être vide**, et c'est voulu. **N'affichez alors aucun bouton
d'appel.**

> ⚠️ **CETTE SECTION EST PÉRIMÉE — VOIR LA 4.58.0.** Les applications ne
> reçoivent plus **aucun** numéro : le téléphone de la personne en danger ne
> compose rien, et c'est le **service client** qui appelle. Le champ `numbers` a
> disparu de `GET /sos/settings`. Le paragraphe est conservé pour qui lit le
> journal dans l'ordre ; ne le câblez pas.

---

### L'écran d'alerte

> - **« L'exploitation a été prévenue »** dès le `201` — la seule chose que la
>   personne cherche à savoir. (4.58.0 : « …et va vous appeler ».)
> - ~~**Les boutons d'appel** de `numbers`~~ — **retirés en 4.58.0.**
> - **Annuler**, en un appui, **sans seconde confirmation** : un « êtes-vous
>   sûr ? » fait rester une fausse alerte dans la file, l'opérateur appelle pour
>   rien, et la prochaine vraie sera prise moins au sérieux.
> - **De quoi ajouter une note**, facultatif et secondaire.
>
> ⚠️ **ET RIEN D'AUTRE** : pas de menu, pas de navigation, pas de retour
> accidentel vers la course. Cet écran se tient devant quelqu'un dont les mains
> tremblent.

⚠️ **ANNULER NE SUPPRIME PAS L'ALERTE** : l'exploitation la voit encore quinze
minutes. Dites-le — « l'exploitation a été prévenue et vous rappellera peut-être
pour vérifier ». Une annulation peut être **contrainte**, et c'est le scénario
même que ce bouton existe pour couvrir ; promettre que « tout est effacé » serait
un mensonge.

⚠️ **PAS DE VERROU D'APPLICATION SUR CET ÉCRAN** (`app_lock`, v4.46.0) : un code
secret entre quelqu'un et son bouton d'alerte serait indéfendable.

⚠️ **LE BOUTON RESTE ATTEIGNABLE EN DEUX APPUIS**, depuis n'importe quel écran
d'une opération en cours. Enterré dans un menu « Aide › Sécurité », il n'existe
pas. **Sauf dans l'application MARCHAND**, où sa place est dans Compte ›
Sécurité : cette application se tient sur un comptoir à portée des clients, et
un bouton rouge affleurant se déclenche par accident plusieurs fois par jour.

⚠️ **LE SOS N'EST PAS LE SUPPORT, ET IL N'ANNULE RIEN.** Le support est une
conversation qu'on relit le lendemain ; le SOS est une alarme qu'un opérateur
prend dans la minute. Et on peut déclencher l'alerte **et rester dans le
véhicule** — c'est même le cas le plus fréquent : n'enchaînez pas sur une
annulation.

> ⚠️ **À distinguer de l'ANNULATION POUR MOTIF GRAVE** (v4.55.0). `accident` et
> `unsafe` sont des **motifs d'annulation** : ils ferment la course et ouvrent
> un ticket critique, qu'on regarde dans l'heure. Après un accident, les deux
> ont leur place — le SOS d'abord, le motif ensuite quand la course se termine.


### 4.55.0 — 8 octobre 2026

Trois trous du produit VTC, fermés ensemble parce qu'ils se rencontrent sur le
même écran : **un chauffeur qui ne reçoit plus d'appels et ne sait pas
pourquoi**.

---

🧱 **LE PLAFOND DE DETTE ÉTAIT LA MÊME CONSTANTE POUR TOUS LES PAYS** — 10 000,
dans la monnaie de chacun. En Guinée, où le franc guinéen vaut quinze fois
moins, cela coupait un chauffeur de Conakry **après une seule course en
espèces**. C'était un bug en production, pas un manque.

Le relevé (`GET /drivers/me/statement`) porte maintenant :

| Champ | Ce qu'il dit |
|---|---|
| `max_debt_xof` | le plafond **effectif** — celui qui s'applique vraiment |
| `max_debt_source` | `country` (l'exploitation du pays) ou `partner` (la flotte, plus serrée) |
| `max_debt_by` | qui l'a demandé, quand on le sait |

⚠️ **N'ÉCRIVEZ AUCUN PLAFOND EN DUR.** Il est réglé par pays, depuis la console,
et il a déjà changé.

⚠️ **DEUX BORNES, LA PLUS SERRÉE MORD.** Un partenaire de flotte peut poser une
limite **pour un chauffeur seul** : il répond des espèces encaissées avec son
véhicule. `max_debt_xof` est déjà le résultat des deux — rien à comparer chez
vous.

⚠️ **ET C'EST `max_debt_source` QUI REND LE MESSAGE UTILE.** « Vous ne recevez
plus d'appels » sans dire **quelle** borne a mordu envoie le chauffeur au
support, qui lit le plafond du pays et n'y comprend rien — alors que c'est son
partenaire qui a posé 5 000. Le geste est le même (régler), mais
**l'interlocuteur change**.

---

📞 **LE TAUX D'ACCEPTATION, CALCULÉ SUR DES APPELS DÉJÀ JOURNALISÉS.** C'était
un calcul manquant, pas une collecte manquante. `GET /drivers/me/stats` porte
un bloc `acceptance` :

```json
{ "acceptance": { "called": 10, "accepted": 6, "declined": 3, "missed": 1, "rate": 60 } }
```

⚠️ **PAS DE TAUX SANS APPEL.** `rate` est **absent** quand `called` vaut 0, et
ce n'est pas un zéro : un chauffeur qui n'a reçu aucun appel n'a **pas** un taux
de 0 %. « 0 % » se lit « il refuse tout » — l'inverse exact — et c'est le premier
écran d'un nouveau chauffeur qui attend sa première course. Affichez « — ». **Ne
recalculez pas** `accepted / called` : vous retomberiez sur la division par zéro
que le serveur vient d'éviter.

⚠️ **`acceptance` TOUT ENTIER PEUT MANQUER**, et ce n'est pas « zéro appel » :
c'est « on n'a pas pu demander ». Le bloc est **omis** quand le service de suivi
ne répond pas, plutôt que rendu à zéro — un zéro ferait passer la journée du
chauffeur pour mauvaise à cause d'une panne de notre côté.

⚠️ **`declined` ET `missed` RESTENT SÉPARÉS.** Refuser est un **geste** ; laisser
sonner est autre chose (téléphone dans une poche, réseau perdu). Les additionner
mélangerait un choix et une panne.

⚠️ **AFFICHEZ LE TAUX AVEC SES QUATRE NOMBRES.** « 60 % » ne se discute pas ;
« 6 acceptés sur 10 reçus » se discute — et c'est un chiffre dont on juge
quelqu'un.

---

🏷️ **LE MOTIF D'ANNULATION ÉTAIT UN TEXTE LIBRE, ET IL NE SE COMPTAIT PAS.**
« changé d'avis », « Changé d'avis », « chg avis », « il est pas venu » : quatre
façons d'écrire deux faits, et aucune manière de répondre à « combien de
passagers posés par un chauffeur cette semaine ? ».

```
GET /rides/cancel-reasons   → { "by": "driver", "items": [ { "code": "rider_no_show", "grave": false }, … ] }
POST /rides/{id}/cancel     { "reason_code": "wait_too_long", "reason": "…" }   // client
POST /rides/{id}/decline    { "reason_code": "rider_no_show",  "reason": "…" }   // chauffeur
```

⚠️ **DEMANDEZ LA LISTE, N'INVENTEZ PAS LES CODES.** Elle **dépend du rôle du
jeton**, et le serveur refuse un code de l'autre rôle (`422`, `fields:
["reason_code"]`, avec `allowed`). Les deux listes ne décrivent pas les mêmes
faits : proposer « le passager n'est pas venu » à un passager est absurde.

⚠️ **TRADUISEZ LES CODES CHEZ VOUS.** La route sert les codes et leur gravité,
**pas des phrases**.

⚠️ **LE CODE S'AJOUTE À LA PHRASE LIBRE, IL NE LA REMPLACE PAS.** Le code **se
compte**, la phrase **explique le cas**. Une taxonomie ne couvre jamais tout, et
forcer un choix fait cocher le premier élément de la liste — pire qu'un champ
vide, parce qu'on le croit. `other` existe pour ça.

⚠️ **DEUX MOTIFS APPELLENT QUELQU'UN TOUT DE SUITE** — `accident` et `unsafe`,
marqués `grave: true`. Ils ouvrent un **ticket de support critique** rattaché à
la course, dans la file que le support regarde déjà (une seconde file se
surveille toujours moins). Côté chauffeur : une confirmation **à part**, dites
que le support a été alerté, et proposez de l'appeler.

⚠️ **L'ANNULATION ABOUTIT MÊME SI LE TICKET ÉCHOUE** — n'attendez pas de
confirmation et **ne réessayez pas**. Le chauffeur est peut-être au bord de la
route : le bloquer dans un écran serait indéfendable.

⚠️ **LISEZ `grave` PLUTÔT QUE DE TESTER `code === "accident"`** : le jour où un
motif grave s'ajoute, votre application le traitera déjà correctement.

🔎 **`driver_asked` EST UN SIGNAL DE FRAUDE** — « le chauffeur m'a demandé
d'annuler ». Un chauffeur qui fait annuler son passager évite les frais
d'annulation **et** garde sa place dans le vivier. Proposez-le en clair côté
client : sans ce choix, le geste est indistinguable d'un renoncement.

⚠️ **ET LES COURSES ANNULÉES AVANT CETTE VERSION N'ONT QUE LA PHRASE.**
`cancelled_reason_code` est absent sur elles : affichez `cancelled_reason` telle
quelle, et **ne traduisez pas son absence par `other`** — c'est leur seule
explication.


### 4.54.0 — 8 octobre 2026

📷 **LA REMISE DU MATÉRIEL SE CONCLUT EN SCANNANT LE QR DU COMPTOIR.** C'est le
dernier geste qui manquait au module matériel (catalogue, contrat, remise,
acceptation, caution, échéancier sur les gains, défaut — tout cela existe depuis
la 4.11.0).

```
POST /equipment/handover   { "code": "<ce que le scanner a rendu>" }   → le contrat, ACTIF
```

⚠️ **POURQUOI CE GESTE EXISTE.** La remise était enregistrée par l'exploitation
seule : rien ne distinguait « l'article a été remis » de « quelqu'un a cliqué sur
Remettre ». Et la remise **démarre l'échéancier** — un clic de trop, et un
livreur rembourse pendant trois mois un sac qu'il n'a jamais eu. Le scan est le
geste que **seul le porteur** peut faire : il prouve qu'il était là, et que c'est
lui qui a conclu.

⚠️ **UN SEUL APPEL, PAS DEUX.** N'appelez **pas** `accept` avant de scanner : le
scan accepte tout seul. Physiquement, l'acceptation et la remise sont un seul
moment ; deux gestes obligeraient le porteur à chercher un autre écran pendant
que le comptoir attend. `accept` reste utile pour accepter **à l'avance**, chez
soi — et le scan marche aussi bien après.

⚠️ **ENVOYEZ CE QUE LE SCANNER VOUS A DONNÉ**, sans le découper : le QR porte un
lien profond, et le serveur accepte le code nu, l'URL entière, avec ou sans
tirets, en minuscules. Extraire le code vous-même ajoute un endroit où se
tromper.

⚠️ **TROIS REFUS, TROIS GESTES — n'affichez pas « code invalide » pour tout.**
`equipment_handover_code_unknown` et `_expired` veulent dire « demandez un
nouveau code au comptoir » ; `equipment_handover_not_yours` veut dire
**« regardez le bon écran »**, et ce cas arrive pour de bon : deux porteurs qui
attendent, deux écrans ouverts. L'envoyer réclamer un code ferait chercher le
comptoir pour rien.

⚠️ **PRÉVOYEZ UN CHAMP DE SAISIE À CÔTÉ DU SCANNER.** Le comptoir affiche aussi
le code en clair, seize caractères groupés par quatre : écran sale, soleil,
téléphone ancien — c'est un cas courant, et sans ce champ il ne reste rien à
faire. L'alphabet écarte `O`, `0`, `I`, `1`, `L` **précisément pour que ce code
se dicte**.

⚠️ **LE CODE VIT CINQ MINUTES**, un seul usage, et un nouveau révoque le
précédent. Un double appui est sans effet : le second appel échoue proprement, et
une remise déjà conclue rend simplement le contrat — vous pouvez laisser le
bouton actif.

🆕 **`handed_via` sur le contrat** : `scan` (le porteur a scanné) ou `staff`
(l'exploitation a enregistré seule). ⚠️ **Absent sur un contrat remis avant que
le scan existe — « on ne sait pas » n'est pas « sans preuve »**, ne traduisez pas
l'absence par `staff`. Affichez-le au moins quand il vaut `scan` : c'est la trace
que le porteur peut invoquer.

⚠️ **ET LE SCAN N'EST PAS OBLIGATOIRE** : l'exploitation peut toujours
enregistrer la remise elle-même, pour un comptoir sans réseau ou un téléphone
sans caméra. Le contrat porte alors `staff`, et c'est ce champ qu'on regarde
quand un échéancier est contesté.


### 4.53.0 — 8 octobre 2026

🪪 **CINQ PIÈCES DE CONFORMITÉ DE PLUS**, pour les chauffeurs VTC comme pour les
livreurs : le **casier judiciaire**, le **selfie**, et **trois photos du
véhicule**.

| Pièce | Rattachée à | `vehicle_id` | `expires_at` |
|---|---|---|---|
| `criminal_record` | la personne | refusé | ⚠️ **obligatoire** |
| `selfie` | la personne | refusé | facultatif |
| `vehicle_front` · `vehicle_rear` · `vehicle_side` | le véhicule | **requis** | facultatif |

⚠️ **`criminal_record` EXIGE UNE DATE, ET LUI SEUL.** Un casier est un
**instantané** : il dit ce qu'on savait le jour de sa délivrance, et rien du
lendemain. Sans date il vaudrait pour toujours — et un extrait de 2019 marqué
« valide » rendrait décoratif le contrôle le plus sensible de la plateforme. Le
dépôt est refusé (`422`) avec le champ nommé. **Demandez la date à l'écran** en
expliquant qu'il s'agit de la validité de l'extrait, sinon la personne la cherche
et redépose la même image.

⚠️ **`selfie` N'EST PAS LA PHOTO DE PROFIL.** Celle du profil est choisie
librement et sert à être reconnu ; le selfie est une pièce qu'un **humain
compare** à la pièce d'identité. Prenez-le dans l'application, caméra avant, et
**ne proposez pas la galerie** : une photo choisie ne prouve rien.

⚠️ **TROIS PHOTOS, TROIS PIÈCES — TROIS `POST`.** Une pièce porte **une** image et
un dépôt **remplace** celle du même type : envoyer les trois sous `vehicle_front`
n'en laisse qu'une, sans message et sans trace. C'est aussi ce qui permet à
`missing` de nommer **quel angle** manque (`vehicle_rear:<vehicle_id>`) — « il
manque une photo » n'indique pas laquelle reprendre.

⚠️ **LE CASIER ET LE SELFIE SONT DUS PAR TOUT LE MONDE**, même sans véhicule
motorisé — contrairement au permis, qui suit la motorisation. Transporter des
gens ou leur argent est ce qui crée l'obligation : un livreur à vélo manipule des
espèces et entre dans des cours d'immeubles. À l'inverse, **un véhicule non
motorisé n'attend rien**, photos comprises : ne réclamez pas la photo d'un vélo.

⚠️ **ET IL FAUT S'Y PRÉPARER : TOUT AGENT DÉJÀ INSCRIT DEVIENT
`compliant: false`** jusqu'à avoir déposé ces pièces. Rien ne l'empêche de
travailler — le serveur n'a jamais bloqué, `compliant` est informatif — mais
**votre bannière va s'allumer pour tout le monde** le jour du déploiement.
Prévoyez un écran qui liste ce qui manque et permette de tout envoyer en une
fois, pas un refus.

✅ **ET LA CONFORMITÉ DES COURSES EXISTE** — `GET`/`POST /driver/documents`.
⚠️ `VTC-DRIVER.md` annonçait « ❌ aucune route » alors que le partage annoncé
avait eu lieu : la spec a continué de décrire un trou, c'est-à-dire qu'une équipe
a pu laisser l'écran `ch_docs` de côté pour rien. Corrigé, avec la table complète
et les états.


### 4.52.0 — 8 octobre 2026

🧾 **LES REÇUS ET LES RELEVÉS, EN PDF — avec la DISTANCE RÉELLEMENT PARCOURUE.**
Quatre routes, deux par métier :

```
GET /rides/{id}/receipt.pdf     GET /rides/statement.pdf?from=&to=
GET /orders/{id}/receipt.pdf    GET /orders/statement.pdf?from=&to=
```

`application/pdf` · `Content-Disposition: attachment` · `Cache-Control:
no-store` · nom de fichier déjà daté (`dira-course-2026-10-08-718293.pdf`).

⚠️ **NE LES OUVREZ PAS DANS UNE WEBVIEW**, et ne les gardez pas dans un cache
disque applicatif. Ces documents portent un nom, une adresse, un trajet et un
montant : sur un téléphone partagé, un reçu mis en cache se relit après la
déconnexion. Laissez le système l'enregistrer ou le partager — c'est ce que la
personne veut en faire.

⚠️ **LA DISTANCE IMPRIMÉE EST CELLE DU TRAJET RÉEL, ET LE DOCUMENT NOMME SA
SOURCE.** `tracked` = mesurée sur les positions de l'agent, recalées sur la
route ; `planned` = estimée, parce que le suivi n'a rien enregistré. **Le second
cas est COURANT, pas rare** : il suffit que le téléphone ait perdu le réseau.
C'est pourquoi la source est écrite plutôt que sous-entendue — « 11,4 km »
présenté comme mesuré alors qu'il vient d'une estimation est la phrase d'un reçu
qu'on ne peut plus défendre devant une réclamation. L'estimation du devis figure
**à côté**, jamais à la place : l'écart entre les deux est ce qu'un litige
examine.

⚠️ **L'APPROCHE DU CHAUFFEUR EST SÉPARÉE** (courses). Les additionner ferait lire
« 10,3 km » pour une course de 8,2 km, avec un prix qui ne colle plus avec la
distance affichée juste au-dessus.

⚠️ **LE DÉTAIL S'ADDITIONNE JUSQU'AU TOTAL, OU RIEN N'EST SERVI** (500). C'est le
choix le plus important de ces documents : un reçu dont les lignes ne tombent pas
juste PROUVE une erreur, il porte notre nom, et c'est le client qui le découvre —
souvent devant son comptable. Mieux vaut un incident qu'on corrige qu'une
réclamation qu'on ne peut pas gagner. Côté commandes, les plats sont donc
détaillés **seulement** s'ils font le sous-total ; sinon un montant global est
servi, parce qu'un reçu moins détaillé vaut mieux qu'un reçu faux.

⚠️ **LES RELEVÉS REFUSENT AU LIEU DE TRONQUER.** `from` et `to` sont
**obligatoires, sans défaut** — un défaut implicite produirait une somme sur une
autre période que celle demandée — et une date illisible est **refusée** (`422`),
pas ignorée. Au delà de **500 opérations** ou **366 jours** :
`409 statement_too_large` avec `meta.max_rows`. Proposez de **découper la
période** : c'est la seule suite utile. Un relevé qui s'arrêterait en silence à la
cinq-centième ligne produirait une somme fausse présentée comme vraie.

⚠️ **ET DEUX RELEVÉS, PAS UN**, même dans l'application unifiée. C'est le seul
endroit où elle ne peut pas réunir les deux métiers : un relevé mêlé exigerait un
document calculé par un service qui voit les deux, et il n'existe pas. Proposez
deux exports plutôt qu'un bouton qui promettrait un seul fichier.

**Trois champs enfin servis, utiles en eux-mêmes :**

⚠️ **`cancel_fee_xof`, `cancel_fee_pct`, `cancel_fee_step` sur une course
annulée.** Le bloc `cancellation` est un DEVIS — ce qu'une annulation
*coûterait* — et il disparaît une fois la course annulée : le montant était bien
prélevé du solde, et **aucune application ne pouvait le montrer**. C'est
précisément la somme dont on demande des comptes. Servi au passager **et au
chauffeur** : ce qui est retenu LUI REVIENT, moins la commission, et le lui cacher
ferait d'une compensation une retenue invisible.

⚠️ **`delivery.actual_distance_m` et `delivery.distance_source` au CLIENT**
(livraison). Le chiffre existait, mesuré sur les positions du livreur, et
s'arrêtait à la console d'exploitation — le client n'avait pas accès à la distance
réelle de SA livraison. Lisez toujours les deux ensemble.

⚠️ **`country` sur une commande.** C'est lui qui DIT LA MONNAIE des montants : une
application qui formate avec la monnaie du COMPTE écrit le bon nombre derrière le
mauvais symbole dès qu'on commande à l'étranger — « 35 000 F CFA » pour une
commande payée 35 000 FG. Les courses le servent depuis la 4.27.0 ; le besoin
était identique.


### 4.51.0 — 8 octobre 2026

🎟️ **LES CODES PROMO — ce qu'on TAPE.** Jusqu'ici, une remise ne pouvait
qu'arriver toute seule : une promotion s'applique à qui remplit ses conditions.
Un **code** se saisit — il vient d'une affiche, d'un influenceur, du parrainage
d'un proche — et c'est un objet différent : il est **unique pour toute la
plateforme**, il a une **enveloppe**, et on veut savoir ce qu'il a **rapporté**.

**Un seul champ, dans les deux métiers** : `POST /rides/quote { …, "code" }` et
`POST /orders { …, "code" }`. Le prix servi — `fare_xof`, `total` — est **déjà
remisé** : ne soustrayez **rien**.

⚠️ **SUR UNE COURSE, LA REMISE REVIENT PAR CLASSE.** 20 % d'une moto et 20 % d'un
van ne font pas le même nombre de francs, et un code peut être plafonné. Affichez
`promo_code_xof` **ligne par ligne** — une remise unique en tête de liste serait
fausse sur trois classes sur quatre. Le code, lui, est validé **une fois** : il
ne peut pas être « accepté pour la moto et refusé pour le van » dans le même
écran.

⚠️ **JAMAIS DEUX REMISES SUR UNE MÊME OPÉRATION.** `promo_title` et `promo_code`
ne sont jamais servis ensemble. C'est la **meilleure des deux** qui gagne, pour
le client — et quand c'est l'offre automatique, la course revient avec
**`promo_code_ignored: true`**.

⚠️ **`promo_code_ignored` DOIT ÊTRE DIT.** Un code accepté qui ne change pas le
prix se lit comme un code cassé : on le ressaisit, on vérifie les majuscules,
puis on appelle le support — qui n'en saura pas plus. La seule phrase vraie est
**« gardez-le : une meilleure offre s'applique déjà »**, et le code n'a rien
consommé : il servira une autre fois.

⚠️ **LES DEUX MÉTIERS NE REFUSENT PAS AU MÊME MOMENT.** Sur une course, le refus
arrive **au devis** : on corrige avant de commander. Sur une commande, il arrive
**à la création**, et la commande n'est **pas** créée au prix plein — le client
aurait payé sans remise en croyant en avoir une, et l'aurait découvert sur son
solde. Conséquence pour l'application : **gardez le panier à l'écran**, et
proposez de réessayer ou de commander sans code.

⚠️ **NEUF REFUS NOMMÉS, ET N'AFFICHEZ PAS « CODE INVALIDE » POUR TOUT** :
`promo_code_invalid` · `_unknown` · `_expired` · `_wrong_country` (`meta.valid_in`)
· `_wrong_service` (`meta.valid_for`) · `_amount_too_low` (`meta.min_amount_xof`)
· `_exhausted` · `_already_used` (`meta.max_uses_per_user`) · `_own_referral`.
Chacun a une suite différente — corriger une faute, renoncer, attendre un panier
plus gros, aller dans l'autre onglet. `message` est déjà traduit : affichez-le.
`promo_code_unavailable` (livraison) est une **panne de notre côté** : proposez
de commander sans code.

🎁 **LE PARRAINAGE — `GET /me/referral`.** Un appel qui dit tout : le code à
partager, ce que gagne celui qui arrive, ce que gagne celui qui invite, et
combien de personnes l'ont déjà utilisé.

⚠️ **`enabled` EST EXPLICITE : NE LE DÉDUISEZ PAS DE L'ABSENCE DE CODE.** Le
parrainage se règle **pays par pays** et il est **éteint par défaut** — tant
qu'une direction ne l'a pas décidé et budgété, il ne distribue rien. À faux,
**cachez l'entrée** « inviter un ami » : un bouton qui ne copie rien, ou un code
partagé de bonne foi qui répond « invalide » aux proches de quelqu'un, coûtent
plus cher que l'absence du bouton.

⚠️ **DEUX MONTANTS, DEUX BÉNÉFICIAIRES.** `invitee_xof` est une **remise** pour
le filleul ; `sponsor_xof` un **crédit** sur le solde du parrain. « Gagnez
1 500 F » fait attendre 1 500 F à tout le monde et produit deux déceptions.

⚠️ **LE PARRAIN EST PAYÉ QUAND LA REMISE EST CONSOMMÉE** — course terminée,
commande livrée —, jamais à la saisie du code. Écrivez-le sur l'écran : sinon
quelqu'un qui voit son filleul commander et son solde ne pas bouger croira que
le parrainage ne marche pas.

⚠️ **ET SON PROPRE CODE EST REFUSÉ, NOMMÉMENT** (`promo_code_own_referral`).
C'est le premier geste de tout le monde : on reçoit son code, on l'essaie.
Accueillez-le comme une explication — « ce code est celui que vous donnez à vos
proches » — pas comme un échec.

**Côté agents, rien ne change dans les montants, et c'est le message à faire
passer** : la remise sort de la **commission de la plateforme**, plafonnée à
elle. `driver_xof` est exactement ce qu'il aurait été sans le code, et
`store_amount` ne bouge pas. ⚠️ **Mais `cash_to_collect` et
`cash_to_collect_xof` PEUVENT ÊTRE PLUS BAS que la somme des plats** : ce sont
ces champs qui font foi. Un livreur qui additionne le panier réclame une somme
que le client n'a pas vue, et c'est lui qui porte la discussion sur le pas de la
porte.


### 4.50.0 — 8 octobre 2026

💸 **UN BARÈME D'ANNULATION, réglé par pays et par véhicule.** Jusqu'ici :
annulation à tout moment, remboursement intégral, aucun barème. C'est tenable
tant qu'un chauffeur ne s'est pas déplacé ; passé ce moment, il a brûlé du
carburant, refusé les appels qui tombaient pendant qu'il venait, et parfois
attendu dix minutes devant un portail — pour rien.

⚠️ **CE BARÈME NE PUNIT PAS, IL COMPENSE** : ce qui est retenu va au
**chauffeur**, moins la commission de la course. À dire dans l'application :
c'est ce qui change la façon dont les frais sont reçus.

**Trois échelons** : gratuit tant qu'aucun chauffeur n'a répondu (toujours, et
non réglable) · gratuit pendant le **délai de grâce** qui suit l'acceptation ·
puis une part du tarif, **plus haute** quand le chauffeur attend sur place (et
là, sans grâce : il est là).

⚠️ **`cancellation` VOYAGE AVEC LA COURSE** — `fee_xof`, `pct`, `step`, `why`,
`grace_left_s` — et **l'écran de confirmation doit le dire**. Retenir de
l'argent à quelqu'un qui n'a pas pu le lire avant est indéfendable, et c'est la
réclamation qu'on ne peut pas gagner. Jamais de bouton « Annuler » nu quand
`fee_xof > 0`.

⚠️ **`grace_left_s` ÉVITE LA RÉCLAMATION** : annoncer « gratuit » sans dire
jusqu'à quand fait découvrir les frais après le geste. Rendez le compte à
rebours depuis ce nombre, et **relisez-le à chaque rafraîchissement** — le
chauffeur peut arriver avant la fin de la grâce, et la grâce tombe alors d'un
coup.

⚠️ **AFFICHEZ `why` QUAND C'EST GRATUIT.** « C'est gratuit » sans dire pourquoi
laisse croire à une faveur, et la prochaine fois que ce sera payant, personne
ne comprendra ce qui a changé.

⚠️ **COMMENT C'EST PERÇU** : sur une course déjà payée, le remboursement est
**amputé** des frais, en un seul mouvement. En **espèces**, personne n'a rien
versé : les frais deviennent une **dette** réglée à la prochaine recharge. Un
écran qui promettrait « remboursement intégral » dans le second cas annoncerait
un mouvement qui n'existe pas.

🧾 **CÔTÉ CHAUFFEUR** : le dédommagement apparaît au relevé sous `cancel_fee`,
**pas** sous `earning` — on doit distinguer ce qu'une course a rapporté de ce
qu'une annulation a rendu. ⚠️ Et aucun montant ne s'affiche **avant** :
annoncer « cette annulation vous rapporterait 800 F » serait une incitation à
attendre plutôt qu'à partir. ⚠️ Une annulation **du chauffeur** ne fait rien
payer au passager.

### 4.49.0 — 8 octobre 2026

🔒 **QUI VOIT QUOI DE QUI — RÉGLÉ PAR PAYS ET PAR MÉTIER, depuis la console.**

Ce qu'un client et l'agent qui le sert voient l'un de l'autre était décidé dans
le code, et il y avait **quatre réponses différentes** à la même question, dans
quatre fichiers, aucune réglable. Ce n'était pas une incohérence — quatre
arbitrages pris séparément, chacun défendable — mais il manquait un endroit
pour les lire et la possibilité de les reprendre.

**Ce qui se règle** : le nom (quatre niveaux : complet · prénom · initiales ·
masqué), le prénom, le nom de famille, la photo, le genre, la note — et les
**trois canaux de contact**.

⚠️ **LE SERVEUR N'ENVOIE PAS CE QU'ON N'A PAS LE DROIT DE MONTRER.** Un champ
absent n'est pas une panne : c'est un champ que l'exploitation n'a pas ouvert
dans ce pays. Affichez votre propre libellé et **ne réessayez pas**.

⚠️ **TROIS PERMISSIONS DE CONTACT, ET CE NE SONT PAS LES MÊMES** :
`show_phone` (afficher le numéro), `direct_call` (proposer un bouton d'appel),
`in_app_alert` (faire sonner l'application de l'autre).

⚠️ **`phone` PRÉSENT NE VEUT PAS DIRE « AFFICHABLE ».** Le numéro arrive dès
qu'il peut être affiché **ou composé** — un bouton d'appel en a besoin pour
composer. **Lisez `show_phone` avant de l'écrire à l'écran** : un `phone`
affiché parce qu'il était là annulerait le réglage dans l'écran même qui le
lit. `direct_call: true` avec `show_phone: false` est le cas le plus courant :
un bouton « Appeler », et le numéro **nulle part**. Nous savons qu'il reste
dans le journal d'appels du téléphone — c'est de la friction, pas du secret, et
c'est assumé : ne compensez pas en l'affichant.

⚠️ **N'ÉCRIVEZ AUCUNE RÈGLE EN DUR** — ni « si le pays est le Togo », ni « si
c'est une course alors pas de téléphone ». Ces décisions changent **sans
redéploiement de votre application**, et une règle recopiée chez vous
contredirait le serveur sans que personne ne sache laquelle croire.

⚠️ **NE RECONSTRUISEZ JAMAIS UN NOM** à partir de `first_name` et `last_name` :
ils ont leurs propres interrupteurs, et les concaténer rendrait un nom complet
là où l'exploitation n'a ouvert que des initiales. Le nom arrive **déjà
masqué**.

⚠️ **CE QUI N'EST JAMAIS FERMÉ**, et qu'il faut donc toujours montrer : le
**véhicule** (une plaque et un type ne nomment personne — c'est ce qu'on guette
dans la rue, et ce qui reste quand tout le reste est fermé) et l'**argent à
encaisser** d'une livraison en espèces (ce n'est pas une donnée personnelle,
c'est ce que le livreur doit réclamer). S'ils manquent, c'est une panne.

🔔 **LE KLAXON DES COURSES SE RÈGLE AU MÊME ENDROIT.** Une alerte sonore fait
sonner le téléphone de quelqu'un : c'est un canal de contact. Ouvert par
défaut ; fermé, `POST /rides/{id}/honk` répond `403 honk_not_available`.
⚠️ **Cachez le bouton, ne le désactivez pas** : lisez
`rider.contact.in_app_alert`. Un bouton qui échoue toujours fait chercher une
panne, et le refus arrive après que le chauffeur a cru se signaler.

🆕 **LA CARTE DU PASSAGER côté chauffeur** (`rider`), qui n'existait pas.
**Absente par défaut** : un chauffeur enchaîne vingt courses et la plateforme ne
lui donne pas vingt identités. Le champ existe pour qu'un marché qui l'exige
puisse ouvrir un prénom ou un appel — pas pour qu'on construise un écran autour
de lui.

⚠️ **TOUT FERMÉ N'EST PAS UNE IMPASSE** : la **conversation** reste le canal, et
elle existe précisément pour que ces deux-là se parlent sans rien s'échanger. Un
écran sans nom ni numéro doit mener à elle, pas à un cul-de-sac.

**Les défauts reproduisent exactement ce qui s'échangeait avant** — rien ne
change tant que l'exploitation n'a rien touché.

### 4.48.0 — 8 octobre 2026

🛵 **LE MOTO-TAXI EST UN MODE DE VÉHICULE À PART, OUVERT DANS TOUS LES PAYS.**
`GET /classes` sert désormais `moto` **en tête** du catalogue : un passager, pas
de bagage, et il passe où une voiture attend. Il était rangé dans `eco`, avec
les citadines — une erreur de prix autant que de produit.

⚠️ **`seats: 1`, ET IL FAUT LE LIRE.** Ne proposez ni « 2 passagers », ni
bagage, ni siège enfant sur un mode à une place. Lisez `seats` plutôt que de le
déduire de la `key` : le jour où un tricycle arrive, il en portera trois.

⚠️ **PAS DE COURSE PARTAGÉE**, et vous le savez **sans règle spéciale** :
`modes` ne porte pas la clé `pool`. C'est déjà la règle générale — une clé
absente = mode non vendu par ce véhicule. N'écrivez pas « si key == moto
alors… » : c'est le serveur qui décide, mode par mode et pays par pays.

⚠️ **`icon_url` EST `null` AU DÉPART**, le temps que l'exploitation pose son
image. Dessinez la silhouette `map_icon` (`moto`) — pas une icône de voiture, et
surtout pas une image en dur de votre côté : elle resterait là le jour où la
vraie arrive.

⚠️ **AUCUNE VOITURE NE RÉPOND À UN APPEL DE MOTO, NI L'INVERSE.** Le reste du
catalogue est une hiérarchie — une berline prend une course éco, le passager
monte dans mieux que ce qu'il a payé — mais la moto en est **dehors** : le prix
est figé au devis, donc une citadine qui répondrait à un appel de moto roulerait
au tarif d'une moto, et le passager qui voulait **passer** dans
l'embouteillage recevrait exactement l'inverse. Côté application : ne promettez
pas « une voiture si aucune moto n'est libre », et côté chauffeur, une moto se
déclare `class_key: "moto"` avec `seats: 1`.

⚠️ **CE N'EST PLUS L'ORDRE DE LA LISTE QUI DÉCIDE DE QUI SONNE**, mais ce que
chaque mode DÉCLARE inclure. Ne déduisez rien du rang d'affichage :
l'exploitation peut remonter un mode pour le mettre en avant sans changer qui
est appelé.

### 4.47.0 — 7 octobre 2026

🗑️ **SUPPRIMER SON COMPTE — `DELETE /me`, en DEUX TEMPS, et les opérations
passées RESTENT.**

1. **Tout de suite** : le compte est **fermé**. Les sessions tombent sur **tous**
   les appareils, les notifications cessent, et la connexion répond ensuite
   `403 account_closed` — par mot de passe **comme par code**.
2. **À `erase_at`** (trente jours, réglable au déploiement) : l'**identité**
   part. Nom → « Compte supprimé », téléphone brouillé puis **libéré**, e-mail,
   photo, état civil, préférences et **carnet d'adresses** effacés, boîte de
   notifications purgée, et les verticales prévenues pour jeter les messages de
   conversation, les fils de support et les pièces de conformité.

⚠️ **ON EFFACE LA PERSONNE, PAS L'OPÉRATION.** Une course et une commande sont
des **écritures comptables** — prix, commission, date, déjà déclarés dans une
facturation par pays : les effacer trouerait le journal en partie double. Elles
ne **désignent plus personne**, et c'est le socle commun qui rend cela tenable :
aucune verticale ne stocke de nom ni de téléphone (elles les demandent au
compte au moment d'afficher), donc anonymiser le compte anonymise **tout**
l'historique d'un seul coup, partout, console et support compris. Un écran mobile
qui promet « tout sera effacé » promet ce qui ne peut pas l'être.

⚠️ **IL FAUT PROUVER QUI ON EST** — `password`, ou `code` pour un compte né par
code (demandé par `POST /auth/otp`, **consommé** ici). Irréversible depuis
l'application : sans preuve, un téléphone déverrouillé posé sur une table
suffirait.

⚠️ **PORTEFEUILLE VIDE EXIGÉ** : `409 wallet_not_empty`, et `meta.reason` dit
`money` · `tokens` · `debt`. On ne détruit pas de l'argent en silence — et une
suppression n'efface pas une dette.

⚠️ **CLIENTS SEULEMENT** : `403 erasure_not_self_serve` pour un chauffeur, un
livreur, un marchand ou un compte de direction. Ils portent des versements et
parfois une dette qui vivent dans la verticale ; le support solde d'abord, le
grand livre sous les yeux.

⚠️ **`account_closed` N'EST PAS `account_suspended`.** Une suspension se lève ;
une fermeture ne se lève que par le support, et seulement pendant le délai de
grâce. Le message dit « ce compte a été supprimé » et propose de se réinscrire —
pas « mot de passe oublié ».

⚠️ **LE NUMÉRO N'EST RENDU QU'À `erase_at`.** Se réinscrire avant répond
`403 account_closed`, pas `phone_taken` : « ce numéro est déjà enregistré »
aurait fait croire que la suppression n'avait pas marché.

### 4.46.0 — 7 octobre 2026

📱 **COMBIEN D'APPAREILS UN COMPTE PEUT TENIR — réglable par PAYS, trois par
défaut.** Les comptes ordinaires (client, marchand, staff) n'avaient jusqu'ici
**aucune borne** : un compte pouvait accumuler vingt sessions vivantes, chacune
avec un jeton de rafraîchissement de trente jours, sans que rien ne le montre.
Au-delà de `max_devices` (`GET /countries`, réglé depuis la console), la session
**la plus silencieuse** est déconnectée.

⚠️ **« LA PLUS SILENCIEUSE », PAS « LA PREMIÈRE OUVERTE ».** Une session est
datée de son dernier rafraîchissement : le téléphone du quotidien se redate
seul, celui du tiroir part le premier. L'appareil évincé l'apprend à son
prochain rafraîchissement — un `401` ordinaire, à traiter comme une session
expirée.

⚠️ **ENVOYEZ `device_id`, MÊME CÔTÉ CLIENT.** Il ne va pas dans le jeton et ne
soumet à aucune règle d'agent ; il sert à reconnaître **le même téléphone qui
revient**, pour qu'une réinstallation ne pousse pas un autre appareil dehors.

⚠️ **LES CHAUFFEURS ET LIVREURS GARDENT UN SEUL APPAREIL**, et cela ne se règle
pas : deux téléphones en ligne pour un véhicule, ce sont deux flux de positions
et un appel qui part vers le mauvais. Leur refus reste `session_superseded`,
avec le libellé de l'appareil qui a pris la place.

🔒 **LE VERROU DE L'APPLICATION — biométrie ou code secret, réglé par PAYS
depuis la console.** Ajout rétrocompatible : `app_lock` voyage avec le jeton
(connexion, inscription, vérification de code) **et au rafraîchissement**, et
`GET /countries` le porte aussi pour l'application qui change de pays sans se
reconnecter. `{mode: off|optional|required, biometrics, pin_length,
grace_seconds, max_attempts}`.

⚠️ **C'EST UNE POLITIQUE, PAS UN CONTRÔLE D'ACCÈS.** Le verrou vit dans le
téléphone : le serveur ne peut ni le poser, ni vérifier qu'il y est. Ce qu'il
protège, c'est un téléphone laissé sur une table — pas une donnée. Ce qui
protège la donnée reste le jeton en stockage sécurisé et sa durée de vie.

⚠️ **TROIS RÈGLES À NE PAS MANQUER**, détaillées dans chaque spec : le **code
secret reste toujours possible** à côté de la biométrie (un capteur cassé ne
doit enfermer personne dehors) ; au-delà de `max_attempts` on **déconnecte**, on
ne bloque pas (un téléphone volé qui se bloque garde un jeton valide trente
jours) ; et **ni l'écran d'appel ni l'urgence ne se verrouillent** — un appel
dure trente secondes, derrière un code c'est un appel manqué.

🆕 **S'INSCRIRE ET SE CONNECTER PAR CODE — le téléphone, et rien d'autre.**
Ajout rétrocompatible : `POST /auth/otp` envoie six chiffres,
`POST /auth/otp/verify` les consomme et rend **exactement** le corps de
`POST /auth/login`, augmenté de **`created`** — le compte vient de naître, ou
non. Rien ne change pour `POST /auth/register` ni `POST /auth/login`, qui
restent la porte des **agents** et des **marchands**.

⚠️ **LA PASSERELLE N'EST PAS ENCORE CÂBLÉE.** Tant que la réponse porte
`channel: "echo"`, **rien n'est envoyé** : le code est rendu en clair dans
**`dev_code`**, pour que les applications se câblent dès maintenant. C'est une
faille assumée et temporaire — quiconque connaît un numéro entre dans le
compte —, et **`dev_code` disparaîtra sans préavis** : l'écran doit fonctionner
à l'identique sans lui.

⚠️ **RÉSERVÉE AUX CLIENTS, et c'est une règle de sécurité.** Six chiffres
remplacent un mot de passe : l'ouvrir aux chauffeurs, aux livreurs, aux
marchands ou à la direction ferait du numéro de téléphone — qui s'affiche sur
une plaque et se donne à un passager — le seul secret protégeant un compte qui
débite des portefeuilles. Un compte qui n'est pas client reçoit
`403 otp_not_available` et **aucun code**.

⚠️ **LA DEMANDE DE CODE NE DIT JAMAIS SI LE NUMÉRO EST CONNU** — ce serait un
annuaire. C'est `created` qui autorise l'écran « comment vous appelez-vous ? »,
une fois la personne authentifiée ; le nom est **facultatif**, et le numéro fait
office de nom d'affichage tant qu'il n'est pas donné.

⚠️ **UN COMPTE NÉ PAR CODE N'A PAS DE MOT DE PASSE**, et `POST /auth/login` lui
répond `401 invalid_credentials` — jamais un code qui dirait « ce compte se
connecte autrement », ce qui révélerait quels numéros existent. Ne lui proposez
ni mot de passe, ni « mot de passe oublié ». *(Au passage : cette connexion
répondait `500` avant d'être corrigée — un compte sans empreinte faisait échouer
la vérification.)*

Cadence : un code par minute et par numéro (`429 otp_too_soon`), cinq par heure
(`429 otp_too_many_requests`), le code vit **5 minutes** et meurt après **5
essais** (`401 otp_too_many_attempts`). Un échec de remise
(`503 otp_delivery_failed`) ne consomme **rien**. Détail et table des refus :
`DIRA-CLIENT.md` §6 bis.

### 4.45.2 — 7 octobre 2026

🔁 **LE PRINCIPE DU MODE PARTAGÉ REMIS EN PREMIER : on cherche le CO-PASSAGER
*avant* le chauffeur.** Correction de rédaction, aucun changement d'API.

⚠️ **C'était une faute d'ordre introduite par la 4.45.1** : en ajoutant
l'explication détaillée, les deux temps — la seule chose à retenir — s'étaient
retrouvés **après six sous-sections**. Ils ouvrent désormais la section, avec le
schéma, les champs, et la raison de cet ordre.

**Pourquoi dans cet ordre, et pas l'inverse** — c'est maintenant écrit : appeler
un chauffeur dès le premier passager aurait semblé plus rapide et ne tenait pas.
Un chauffeur accepte en vingt secondes ; il aurait fallu ensuite soit **lui
imposer un arrêt qu'il n'a pas accepté**, soit **faire partir le premier passager
seul au prix du partage**. Le groupe est donc figé **avant** que le téléphone
d'un chauffeur ne sonne : ce qu'il accepte est ce qu'il roulera. ⚠️ Conséquence
directe, dite aussi : **on ne rejoint plus un groupe après l'acceptation**.

- **`VTC-CLIENT`** : la section ouvre sur « on cherche le co-passager avant le
  chauffeur », et l'avertissement de l'écran d'attente va avec — ⚠️ **ne dites
  pas « nous cherchons un chauffeur » pendant l'étape 1**, dites « nous cherchons
  quelqu'un qui fait le même trajet » et décomptez `pool_until`.
- **`VTC-DRIVER`** : la section ouvre sur ce que cet ordre **protège** — quand
  son téléphone sonne, le groupe est déjà formé et figé. Il n'accepte jamais
  « une course à laquelle on ajoutera quelqu'un ».
- **`DIRA-CLIENT`** : le renvoi dit le principe au lieu de le nommer.

### 4.45.1 — 7 octobre 2026

📖 **LE PRINCIPE DE FONCTIONNEMENT DE LA COURSE PARTAGÉE, EN ENTIER.** Aucun
changement d'API : les deux specs VTC expliquent désormais **comment ça marche**,
et pas seulement quels champs lire.

**`VTC-CLIENT` §4 quater** — le marché à trois (ce que chacun gagne et à quoi il
renonce), **les cinq conditions qui décident que deux trajets se partagent**
(avec un exemple de carte montrant une paire acceptée et deux refusées), la
**chronologie minute par minute des deux passagers côte à côte**, pourquoi la
voiture semble venir d'ailleurs, l'ordre de route, les **quatre façons dont ça se
termine**, et les **six questions que le passager va poser** avec leur réponse.

⚠️ **Ce qui manquait vraiment** : rien n'expliquait **ce qui décide** qu'un
appariement est possible. Les rayons étaient servis dans les réglages sans dire
à quoi ils servent — et une application ne doit surtout pas les appliquer
elle-même : le serveur ne lui donne jamais les courses en attente (ce serait
donner l'adresse d'inconnus). Ils sont là **pour qu'elle puisse expliquer**.

**`VTC-DRIVER` §4 ter** — l'économie du mode (un déplacement, deux courses :
140 % au lieu de 100 %), comment un groupe arrive jusqu'à lui, pourquoi
`pickup` n'est le départ d'**aucun** des deux passagers, ce qu'il reçoit en
acceptant, le tableau « vous faites ceci → sur quelle course », et ce qui se
passe si un passager annule **avant** puis **après** son acceptation.

⚠️ **UNE LIMITE CONNUE, DÉSORMAIS ÉCRITE : `eta_at` EST OPTIMISTE POUR LE SECOND
PASSAGER.** Il est calculé en ligne **directe** depuis la position du chauffeur
vers l'arrêt suivant de **chaque** course : il ne passe **pas** par la prise en
charge de l'autre. Tant que le second passager n'est pas à bord, le chiffre
annoncé est donc plus court que la réalité.

- **Côté passager** : ne l'affichez pas comme une promesse quand `pool_size > 1`
  et que vous n'êtes pas encore `in_transit` — dites « il récupère d'abord
  l'autre passager ». Dès la montée à bord, `eta_at` redevient exact.
- **Côté chauffeur** : le second passager peut vous croire en retard alors que
  vous êtes dans les temps. C'est de là que ça vient, pas de votre conduite.

### 4.45.0 — 7 octobre 2026

📏 **LE TRAJET MINIMUM D'UNE COURSE PARTAGÉE PASSE À 3 km** par défaut
(`settings.modes.pool.min_distance_m`, 5 km jusqu'à la v4.44.0).

⚠️ **À 5 km, le mode ne se proposait presque jamais en ville** : la plupart des
courses urbaines d'ici sont plus courtes, et un mode qui n'apparaît pas ne se
vend pas. Trois kilomètres laissent encore de la place à une remise qui vaut le
détour.

⚠️ **C'est un DÉFAUT, pas une valeur en dur.** Lisez-le dans
`GET /settings/modes?near= → pool.min_distance_m` et comparez-le au
`distance_m` du devis ordinaire pour savoir si vous pouvez **proposer** le
mode : un pays peut régler autre chose, et une application qui écrirait « à
partir de 3 km » en dur mentirait chez le voisin. Le refus `pool_too_short`
porte les deux nombres **dans sa phrase**, déjà traduite.

🇹🇬 **La course partagée est ouverte au Togo** — la politique du pays l'était
déjà, mais aucun véhicule ne la proposait, donc rien ne se vendait. Les trois
classes togolaises l'offrent désormais. ⚠️ Rappel : **deux conditions, pas
une** — le pays ouvre le mode (`settings.modes.pool.enabled`) **et** chaque
voiture dit si elle le sert (`classes[].modes.pool`). Une clé `pool` présente
dans `modes` veut dire que les deux sont vraies : vous n'avez pas à recouper.

### 4.44.0 — 6 octobre 2026

💸 **LE PRIX D'UNE COURSE PARTAGÉE EST UNE *PART* DU TARIF ORDINAIRE** —
`settings.modes.pool.fare_pct`, **70 % par défaut**, réglé par **pays**. Le
devis et la course portent **`share_pct`**.

⚠️ **`share_pct: 70` VEUT DIRE « IL PAIE 70 % », donc 30 % de moins.** C'est
une **part**, pas une remise : le nombre servi est celui qu'on paie. Pour
afficher l'économie, montrez `100 − share_pct`. Dire l'inverse aurait rendu
« 0 » ambigu — gratuit, ou plein tarif ?

⚠️ **AFFICHEZ-LA, au devis ET sur le reçu.** « 2 000 F » à côté de « 2 850 F »
ne se comprend pas tout seul : c'est « −30 % » qui donne une raison d'accepter
un détour et un inconnu à bord. Et des mois plus tard, c'est la seule chose qui
explique un montant inférieur au tarif — sans elle, le client lit une erreur de
facturation, et l'exploitation une sous-facturation.

⚠️ **`modes.pool` NE PORTE PLUS DE GRILLE** — c'est un objet **vide**, dont la
seule présence dit « cette voiture partage ». Jusqu'à la v4.43.0 il servait
`base_xof`, `per_km_xof` et `per_min_xof` : ils ne facturent plus et ne sont
plus servis. Une application qui les lirait encore n'y trouverait **rien**.

**Pourquoi ce changement.** Chaque véhicule avait sa propre grille de partage :
**deux endroits où lire un prix pour le même trajet**, dont le second pouvait
être saisi **plus cher** que l'ordinaire sans que rien ne l'interdise — et le
mode ne prenait alors jamais, sans que personne ne comprenne pourquoi. Une part
ne peut pas être plus chère que ce dont elle est une part, et elle suit toute
seule chaque changement de la grille ordinaire, **y compris par véhicule** :
70 % d'un van est une remise de van.

- ⚠️ **La part s'applique au sous-total, une fois**, puis le prix est arrondi
  au multiple de 50 comme partout, et la commission se calcule sur **ce**
  montant. Remiser la prise en charge, le kilomètre et la minute séparément
  aurait tronqué trois fois et le total ne serait pas retombé sur la part
  annoncée.
- ⚠️ **La part est FIGÉE au devis** : un pays qui la change à midi ne change
  pas un devis déjà affiché.
- **La commission reste propre au mode**, réglable par véhicule : la plateforme
  a apparié deux trajets, et le chauffeur en sert deux pour un seul
  déplacement. Laissée à zéro, celle de la course ordinaire s'applique.
- ⚠️ **Pas de majoration de zone, pas de promotion** : la remise **est** la
  promotion de ce mode.

### 4.43.0 — 6 octobre 2026

🆕 **UNE SEULE APPLICATION CLIENTE POUR LES DEUX MÉTIERS** —
[`DIRA-CLIENT.md`](DIRA-CLIENT.md), premier document. Un compte, un solde, une
boîte, un historique, et deux métiers à l'intérieur. Deux endpoints **combinés**
le rendent possible, servis par `dira-analytics` — le service qui détient le
modèle de langage **et** interroge déjà les deux métiers.

💬 **L'ASSISTANT UNIFIÉ — `POST /analytics/ai/chat`.** « J'ai faim » et
« emmène-moi à l'aéroport » arrivent par le **même** champ. Le serveur tranche
de quel métier il s'agit, délègue à l'assistant de ce métier-là, et rend sa
réponse **verbatim** : `vertical` dit qui a répondu, `plan` garde la forme de la
spec du métier, `routed_by` dit comment on a tranché (`app` · `lexical` ·
`model`).

- ⚠️ **`vertical` ABSENT = ON N'A PAS SU TRANCHER**, et `choices` donne les
  métiers possibles : **posez la question**. On ne devine pas — envoyer
  « commande-moi quelque chose » au hasard ferait livrer un repas à quelqu'un
  qui attend un taxi, et il le découvrirait au prix.
- ⚠️ **ENVOYEZ `vertical` DÈS QUE VOUS LE SAVEZ** (on arrive depuis un onglet) :
  c'est gratuit, exact, et cela évite un appel de modèle. **Omettez-le** depuis
  un accueil où les deux métiers cohabitent.
- ⚠️ **UN PLAN NE COMMANDE RIEN** : il se confirme par la route du métier
  (`POST /orders`, `POST /rides { quote_id }`), comme s'il avait été composé à
  la main. `unresolved` se **dit**, il ne se tait pas.
- ⚠️ **PAS DE VOCAL COMBINÉ** : utilisez `POST /ai/voice` du métier visé. Un
  vocal n'est **jamais** exécuté directement — la transcription revient à
  l'application, qui l'affiche et laisse corriger.
- Refus : `ai_disabled` et `assistant_unavailable` (masquez la boîte),
  `ai_quota_reached` et `ai_budget_reached` (proposez la composition à la main).

📜 **LE FIL D'ACTIVITÉ — `GET /analytics/activity`.** Les courses et les
commandes dans une seule liste, la plus récente d'abord, découpée en
**semaines**.

- ⚠️ **LE PAS DE PAGINATION EST LA SEMAINE, jamais la ligne.** Redonnez
  `next_before`. Une semaine n'est **jamais** coupée entre deux pages : son
  en-tête « 3 courses · 12 000 F » mentirait sur la moitié servie.
- ⚠️ **LES SEMAINES SONT DÉCOUPÉES DANS LE FUSEAU DU PAYS** (`timezone`), pas en
  UTC. Le Gabon et le Tchad sont à UTC+1 : une course prise **lundi 00 h 30** à
  Libreville a eu lieu dimanche 23 h 30 en UTC, et un découpage en UTC la
  rangerait dans la semaine **précédente**.
- ⚠️ **`iso_year` N'EST PAS TOUJOURS L'ANNÉE DE LA DATE** : le 29 décembre 2025
  est en **semaine 1 de 2026**.
- ⚠️ **LES SEMAINES VIDES SONT SAUTÉES** : entre deux commandes espacées d'un
  mois, vous recevez deux semaines, pas cinq. Lisez `start` et `iso_week`, pas
  l'index dans la liste.
- ⚠️ **`sources` DIT CE QUI A RÉPONDU.** Un métier muet ne vide pas l'historique
  de l'autre — mais **dites-le** : un fil où la moitié manque sans le dire se lit
  comme un fil complet, et le client croit avoir perdu ses commandes. Un jeton
  **refusé**, lui, refuse le fil **entier**.
- ⚠️ **C'EST UN FIL, PAS L'OBJET.** Une ligne porte des **faits** (`pickup`,
  `first_item`, `mode`) et le `status` du métier **verbatim** ; le détail se lit
  chez le métier, seule source de vérité. Fabriquer « Course vers l'aéroport »
  côté serveur aurait demandé de traduire et de choisir une longueur — des
  décisions d'écran prises loin de l'écran.

⏳ **`?before=` SUR `GET /rides` ET `GET /orders`** (RFC 3339, **strictement**
avant) — la fenêtre de date qui rend le fil combiné possible. Sans elle, il
repartait du haut à chaque page et relisait tout ce qu'il avait déjà lu : le
coût d'une page croissait avec la profondeur. ⚠️ **Une date illisible est
REFUSÉE, pas ignorée** : l'ignorer servirait silencieusement l'historique entier
là où l'appelant demandait une tranche. Additif — une application qui ne
l'envoie pas ne voit aucun changement.

⚠️ **LES DEUX PIÈGES PROPRES À UNE APPLICATION UNIFIÉE**, écrits dans
`DIRA-CLIENT` parce qu'aucune des deux specs métier ne pouvait les dire :

- **DEUX SOCKETS, DEUX SERVICES, DEUX CYCLES DE VIE** — celui des commandes est
  global, celui du suivi est **par opération**. Ne les factorisez pas sous
  prétexte que ce sont deux WebSockets : une couche unique finit par fermer le
  mauvais. ⚠️ Et **les courses n'ont pas de socket global** : attendre la
  symétrie des deux métiers, c'est attendre une trame qui n'arrive jamais.
- **UNE SEULE BOÎTE, DEUX VOCABULAIRES** — `GET /notifications` rend les deux.
  ⚠️ **Routez sur `data.type`, pas sur la clé du gabarit** : la clé est du texte,
  renommable par l'exploitation. Une clé **inconnue** s'affiche quand même, sans
  rien ouvrir.

### 4.42.0 — 6 octobre 2026

🤝 **LA COURSE PARTAGÉE.** Deux passagers dont les départs **et** les arrivées
sont proches montent dans la même voiture et paient **chacun son trajet**,
moins cher. Le chauffeur fait un déplacement et sert deux courses. Un mode de
plus, à côté du compteur et de la location — `VTC-CLIENT` §4 quater,
`VTC-DRIVER` §4 ter.

⚠️ **LA RECHERCHE SE FAIT EN DEUX TEMPS, et c'est tout ce qu'il faut retenir.**
D'abord un **co-passager** (`dispatch_state: "pooling"`, 5 min par défaut,
**aucun chauffeur appelé**) ; ensuite **une voiture pour les deux**
(`dispatch_state: "calling"`), sur un cercle centré au **milieu** des deux
départs. Côté passager : ne dites pas « nous cherchons un chauffeur » pendant
la première étape — ce serait faux, et il s'étonnerait que ça dure cinq
minutes.

- **`pool_until`** est un **instant**, à décompter localement — comme `eta_at`.
- **`dispatch_reason`** dit pourquoi la recherche s'est arrêtée, et **change ce
  que l'écran propose** : `pool_no_match` (relancer **ou** prendre une course
  ordinaire), `pool_partner_left` (relancer — le prix ne change pas),
  `pool_call_failed` (relancer). Absent sur une course ordinaire : il n'y a
  qu'une façon d'y échouer.
- ⚠️ **ON NE BASCULE JAMAIS D'OFFICE** en course ordinaire : elle n'a pas le
  même prix. Changer de mode = annuler (remboursé, personne ne l'a prise) et
  repasser par un devis.
- **Chacun paie SA distance**, à la grille remisée, **figée au devis**. Il
  n'existe aucun prix commun : ne divisez rien, n'affichez pas de montant « à
  deux ». Ni majoration de zone ni promotion sur ce mode — la remise est déjà
  dans la grille.
- **Côté chauffeur : il reçoit DEUX courses et un ORDRE de passage**
  (`pool_plan`) — les deux prises en charge d'abord, **puis** les deux dépôts.
  Déposer le premier avant d'aller chercher le second ferait deux courses à la
  suite, pas une course partagée. Chaque arrêt porte son `ride_id`.
- ⚠️ **`pool_plan` ne va qu'au CHAUFFEUR.** Un passager ne reçoit ni le nom ni
  l'adresse de l'autre : ils se rencontrent dans la voiture. Prévenez-le que la
  voiture fera un détour qu'il ne pourra pas expliquer point par point.
- **Trois pushs** : `ride_pool_matched` (à celui qui attendait — on a trouvé
  quelqu'un), `ride_pool_no_match`, `ride_pool_partner_left`.
- **Refus du devis** : `pool_off`, `pool_too_short`, `pool_direct_only` (pas
  d'arrêt intermédiaire), `pool_no_class`. ⚠️ **Les chiffres sont DANS la
  phrase** — « Ce trajet fait 2 100 m : la course partagée commence à
  3 000 m » —, pas dans un `meta` : l'enveloppe ne rend que `code`, `message`,
  `fields` et `reason`, et c'est la règle depuis le début.
- **Réglé par pays** (`GET /settings/modes → pool`) : 2,5 km entre les départs,
  2,5 km entre les arrivées, 3 km de trajet minimum (⚠️ 5 km jusqu'à la
  v4.44.0 — voir la 4.45.0), 5 min de recherche, 2
  passagers. ⚠️ **Allumé par défaut**, contrairement aux deux autres modes : il
  n'ajoute qu'une option au passager, ne retire rien, et ne se déclenche que
  s'il la choisit.
- ⚠️ **Pas d'entrée `tracking.pool`** : une course partagée est une course
  commandée, elle utilise la cadence `normal`.

📝 **CORRECTION, et elle touche la LOCATION aussi.** `VTC-CLIENT` et
`VTC-DRIVER` annonçaient depuis la v4.29.0 que `422 rental_out_of_range`
portait un `meta` avec `distance_km` et `max_km`. **Il n'arrive pas** — les deux
documents disaient par ailleurs, et depuis toujours, que l'enveloppe ne rend
que `code`, `message`, `fields` et `reason`. Les nombres n'ont jamais voyagé
qu'à l'intérieur de la phrase traduite, qui les porte bel et bien. Aucun
changement de serveur : c'est la rédaction qui était fausse.

📍 **`GET /settings/modes` SUIT ENFIN LE LIEU — `?near=lng,lat`.** Depuis la
v4.27.0 un devis est calculé dans le pays du **départ**, et `GET /classes?near=`
rend le catalogue de ce pays-là. Les réglages, non : un passager togolais à
Dakar lisait les modes du **Togo** avec les prix du **Sénégal**, deux pays dans
un seul écran.

⚠️ **Passez `near` sur les DEUX routes, avec le point de départ.** C'était
invisible jusqu'ici parce que les prix venaient du bon pays ; avec le partage,
cela devenait faux à l'affichage — « à partir de 3 km » là où le pays d'accueil
en demande trois. Un `near` illisible est **ignoré**, pas refusé.

### 4.41.0 — 30 septembre 2026

🗺️ **LE FOND PAR DÉFAUT D'UN PAYS SE LIT DANS `GET /countries`.** Chaque pays du
catalogue public porte `basemap` (`dira` | `google`).

⚠️ **Pourquoi c'était nécessaire, et c'est le piège** : le bloc `maps` de
l'authentification voyage avec le jeton — il dit le fond du pays où l'on s'est
**connecté**, une fois. L'application rouvre sans se reconnecter, et surtout le
pays peut changer sans reconnexion (un passager togolais qui ouvre l'application
à Dakar). Une application qui ne lirait que `maps` garderait le fond de la
veille, dans le mauvais pays.

- La règle du fond effectif, écrite dans les cinq documents : **le choix de la
  personne, sinon le `basemap` du pays où elle OPÈRE, sinon `dira`**.
- **Tant que la personne n'a rien choisi, suivez le pays.** Dès qu'elle choisit,
  son choix prime et survit au changement de pays.
- ⚠️ **Le fond change → la carte doit RENAÎTRE.** Les SDK lisent leur style à la
  création ; changer la valeur sans reconstruire la vue ne se voit pas. C'est le
  défaut que la console a eu le 30 septembre : le logo Google posé sur les
  tuiles de l'autre fond.

### 4.40.0 — 29 septembre 2026

🗺️ **LE SERVEUR NE SERT PLUS DE CLÉ GOOGLE. `maps` NE PORTE PLUS QUE
`basemap`.** Il a servi une clé par pays et par plateforme du 22 au 29
septembre, pour pouvoir la faire tourner sans republier les applications.
L'intention était juste ; le montage était incompatible avec la façon dont vous
êtes faits. **Une clé Google se restreint par ce qui l'utilise** — nom de paquet
+ empreinte SHA-1, bundle, référent HTTP —, donc elle appartient à
l'application, et vous en avez déjà une. La clé du serveur arrivait trop tard,
pour quelqu'un qui n'en avait pas besoin, et elle demandait à l'exploitation de
ranger trois secrets par pays dans un écran d'administration.

Il reste **la seule question que le serveur tranche mieux que vous** : dans ce
pays, quel fond montre-t-on d'abord ?

- `"maps": { "basemap": "dira" | "google" }` — un champ, servi à la connexion,
  à l'inscription et au rafraîchissement.
- ⚠️ **`google_key` et `google_map_type` ont disparu.** Si vous les lisiez,
  cessez.
- ⚠️ **Le CHOIX ne se conditionne plus à rien : proposez toujours les deux
  fonds**, et affichez Google avec **votre** clé. C'était la règle inverse
  jusqu'ici — c'est le seul point où votre code doit changer.
- ⚠️ **`platform` est acceptée et IGNORÉE** sur les trois routes d'authentification.
  **Ne la retirez pas** : le serveur refuse les champs inconnus, et ce champ
  reste déclaré exprès pour que rien ne casse d'un déploiement à l'autre.
- Côté exploitation, la console n'a plus qu'une bascule par pays — plus de clé
  à coller, plus de plateforme à choisir.

### 4.39.1 — 29 septembre 2026

📋 **SECONDE RÉPONSE AU RELEVÉ CHAUFFEUR DU 29 SEPTEMBRE** (C4, C5, E6).

- **C4 — `results[].message` de `/rides/sync` et `/deliveries/sync` parle la
  langue de la requête.** Vous aviez raison : la traduction couvrait
  l'enveloppe, pas les lignes de résultat — « ride not found » restait en
  anglais au pire moment. Corrigé dans les deux verticales ; un test le garde.
  ⚠️ Trouvé en le vérifiant : `retryable` **manquait** sur une ligne refusée
  quand il valait `false` (les faux étaient tus). Une ligne `rejected` le
  porte désormais **toujours** — c'est lui qui décide « retirez-le » ou
  « gardez-le », et un champ absent ne décide rien.
- **C5 — confirmé par écrit, et éprouvable :** un point `backfill: true` daté
  de plus d'une heure est **accepté et rangé à son `ts` d'origine** ; le
  parcours n'a aucune limite d'âge (seule la position courante expire). Et le
  feu vert du Gabon couvre **aussi** les rattrapages — roulez, coupez le
  réseau, reprenez. Écrit dans `VTC-DRIVER.md` §4 quater, *Les positions*.
- **E6 — `maps` : la question est tranchée autrement, voir 4.40.0.** La réponse
  du matin (« attendez que le bloc `maps` porte `google_key` ») ne vaut plus :
  le serveur ne sert plus de clé du tout. Vous n'attendez donc rien — vous
  affichez Google avec la vôtre quand la personne le demande, et `basemap` dit
  seulement quel fond montrer d'abord.

### 4.39.0 — 29 septembre 2026

🎙️ **LES VOCAUX DANS LES LANGUES D'ICI.** Jusque-là, un vocal en wolof
partait chez un modèle à qui personne n'avait dit que c'était du wolof, et
revenait en charabia français. Douze langues sont désormais **nommées** aux
modèles — français, anglais, éwé, kabiyè, wolof, peul, sérère, malinké,
soussou, arabe tchadien, ngambay, fang —, et l'exploitation règle **par
pays** les langues attendues, qui transcrit, et les mots d'ici.

- `POST /ai/voice` accepte `language=<code>` (facultatif) et rend
  `{ text, language }`. **N'envoyez rien** quand la personne n'a pas choisi :
  le serveur prend la première langue du pays. ⚠️ **Ce n'est pas
  `Accept-Language`** — la langue de l'interface n'est pas la langue parlée.
  Codes et refus (`422 unknown_language`, `503 language_not_served`) dans la
  section *Le VOCAL* de `FOOD-CLIENT.md` et `VTC-CLIENT.md`.
- ⚠️ **Le texte revient dans la langue parlée**, mots français compris. Ne
  le traduisez pas : montrez-le, laissez corriger, envoyez.
- Rien ne change pour un vocal en français : même route, même délai
  (« transcription : 1-3 s »).

### 4.38.0 — 29 septembre 2026

📋 **RÉPONSE AU RELEVÉ DE L'APPLICATION CHAUFFEUR DU 29 SEPTEMBRE** (points C1
à E). Rien n'a été cassé depuis le 26 ; tout ce qui est écrit ici est vérifié.

- **C3 — la resynchronisation s'éprouve pour de vrai, au GABON.** C'est la
  réponse que vous attendiez en premier : un pays réservé aux essais, où créer
  des courses est **permis**. Comptes, feu vert et garde-fous dans la section
  *Le Gabon est le pays d'essai* ci-dessus, et une note en tête de
  `VTC-DRIVER.md` §4 quater et de `FOOD-DELIVERY.md` §6. Et un repère : le
  serveur y a déjà rejoué dix-huit courses libres depuis trois téléphones
  simultanés — toutes `applied`, et le même lot renvoyé n'a rien créé.
- **C1 — `session_superseded` parle français, partout.** Le refus naît dans le
  socle mais sort des courses et de la livraison, qui n'en avaient pas la
  phrase. ⚠️ En mesurant, ce n'était pas un cas isolé : **une centaine de
  refus** — `insufficient_funds`, `store_closed`, `debt_over_limit` — partaient
  en anglais dans trois services sur quatre. Tous traduits ; un test le garde
  désormais dans chaque dépôt, et le socle embarque les phrases des refus
  qu'il prononce au nom de tous. Vous n'aurez plus à porter votre propre
  phrase pour un refus du serveur — mais gardez la vôtre pour celui-ci, le
  contrat le recommande toujours.
- **D2 — validé, et ÉCRIT :** une issue de `/rides/sync` inconnue se **garde**
  (comme un `rejected` + `retryable: true`), et tout nouvel `outcome` terminal
  arrivera avec **préavis d'une version** dans ce document. Même règle pour
  `/deliveries/sync`.
- **D1, D3, D4, D5 — validés, sans réserve.** D5 vérifié dans le code : la
  majoration de zone n'est calculée que si `mode` est vide ; compteur et
  location la reçoivent à `nil`.
- **C2 — acté :** `nav_icon_url` n'est pas produit et ne le sera pas. Écrit
  dans `VTC-DRIVER.md` §2. Le champ reste au contrat pour la carte du
  passager — divergence assumée, pas oubli.

⚠️ Deux choses au passage. Le relevé déposé dans `docs/frontend/` était devenu
**documentation de l'assistant** (le dossier s'embarquait en entier) ; les
contrats sont désormais nommés un par un, et un relevé n'est qu'un relevé. Et
ne testez **jamais** sur le Togo ni le Sénégal : une course d'essai y notifie
six personnes.

### 4.37.0 — 28 septembre 2026

🚕🛵 **UN CHAUFFEUR N'EST JAMAIS LIVREUR — deux mots pour les deux applications
d'agent.** L'application de **livraison envoie désormais `app: "courier"`** à la
connexion et à l'inscription, là où elle envoyait `driver` comme celle des
courses. Le socle **refuse** l'entrée à un compte qui frappe à la porte de
l'autre : `403 wrong_app`, avec `error.reason` nommant l'application à ouvrir.

**Ce que les applications ont à faire :**

| Application | Ce qui change |
|---|---|
| **Dira Livreur** (`FOOD-DELIVERY.md`) | ⚠️ **`app: "courier"`** sur `POST /auth/login` **et** `POST /auth/register`, au lieu de `"driver"`. `role: "driver"` à l'inscription **ne change pas**. |
| **Dira Chauffeur** (`VTC-DRIVER.md`) | rien à la connexion — mais **afficher** le refus `wrong_app` avec `reason: "courier"` : « ce compte est un compte livreur ; ouvrez l'application Dira Livreur ». |
| **Les trois autres** | ⚠️ `error.reason` a **cinq** valeurs et non plus quatre : `courier` s'ajoute. Un `switch` à quatre branches retombe sur son défaut — c'est-à-dire sur « erreur de connexion », le message que cette règle existe pour éviter. |

⚠️ **JAMAIS « erreur de connexion », JAMAIS « identifiants invalides ».** Le mot
de passe était juste. Le refus DIT où aller : affichez-le.

⚠️ **POURQUOI — et il faut le dire aux équipes, sinon elles contournent.** Une
personne qui ouvrait les deux applications sur un seul téléphone **se
déconnectait elle-même, en boucle**. Un agent ne tient qu'une session et le
registre d'appareils est clé par **compte** ; deux applications sur le même
téléphone sont deux installations, donc deux `device_id`, et chacune chassait
l'autre — avec un écran qui annonçait « vous vous êtes connecté sur un autre
appareil » **en nommant son propre téléphone**. S'y ajoutaient deux flux de
positions pour un seul véhicule, et deux viviers d'appel pour une seule
personne. **Qui veut faire les deux métiers ouvre deux comptes**, avec deux
numéros — c'est la seule configuration que la plateforme sait tenir.

**La bascule ne ferme la porte à personne.** Un compte qui n'appartient encore à
aucune application n'est **rien refusé** : les comptes déjà en place se rangent
tout seuls à leur première ouverture, du côté de l'application qui porte déjà
leur profil. ⚠️ Mais **n'invitez personne à « essayer l'autre application pour
voir »** pendant cette période : un livreur en place qui ouvrirait celle des
chauffeurs avant d'avoir rouvert la sienne réserverait `driver`, et se verrait
ensuite refuser l'entrée chez lui. C'est réparable — le support peut **libérer**
l'appartenance d'un compte, pour de vrais changements de métier — mais c'est une
journée de travail perdue pour quelqu'un.

**Ce qui NE change pas : le RÔLE.** Un livreur a toujours le rôle `driver` au
socle — même portefeuille de jetons, même règle d'appareil unique, mêmes pièces
de conformité. La frontière nouvelle est le **métier**, pas le rôle : il n'y a
pas de rôle `courier`, et `POST /auth/register` avec `role: "courier"` répond
`422`.

⚠️ **Ceci corrige ce que la 4.26.0 annonçait.** Elle écrivait que `driver`
couvrait « le chauffeur VTC **et** le livreur, même rôle au socle », et que deux
applications d'une même famille ne se distinguaient pas entre elles. C'était
vrai, et c'est précisément ce qui produisait la boucle de déconnexion. Les deux
applications d'agent se distinguent désormais ; les deux applications de
**client**, elles, continuent de partager `client`.

Écrit au long dans `VTC-DRIVER.md` §7 et `FOOD-DELIVERY.md` §2, chacun pour son
public.

### 4.36.0 — 28 septembre 2026

🗺️ **LE FOND DE CARTE SE RÈGLE PAR PAYS — Dira ou Google.** Le pays décide de
celui qu'on voit **d'abord** ; la personne choisit ensuite celui qui lui va, et
son choix reste chez elle. Écrit au long dans les cinq documents, section *Le
fond de carte*.

Ce que les applications ont à faire :

> ⚠️ **CE QUI SUIT A ÉTÉ SIMPLIFIÉ PAR LA 4.40.0** : le serveur ne sert plus de
> clé, `maps` ne porte plus que `basemap`, et le choix se propose **toujours**.
> Lisez la section *Le fond de carte* d'un des cinq documents, pas cette liste.

- **Lire `maps`** dans la réponse (connexion, inscription, rafraîchissement).
- ⚠️ **`maps` absent = notre fond, rien d'autre à faire.** Une application pas
  encore à jour garde une carte qui marche.
- **Garder le choix de la personne en local**, ⚠️ **même quand il devient
  impossible** : quelqu'un qui change de pays a changé de pays, pas d'avis.
- ⚠️ **Le logo Google est obligatoire** dès que ses tuiles s'affichent — 16 dp,
  10 dp de dégagement, jamais recouvert, le vôtre compris. C'est une clause du
  contrat d'utilisation, pas une politesse.
- ⚠️ **Jamais la clé dans un journal ni dans un rapport de plantage** : ces
  fichiers partent chez un tiers et se gardent des mois.

### 4.35.0 — 28 septembre 2026

📱 **UN SEUL APPAREIL À LA FOIS — pour les chauffeurs et les livreurs, et pour
eux seuls.** Un compte de chauffeur ouvert sur deux téléphones poussait **deux
flux de positions pour une seule voiture** : le vivier d'appel la voyait à deux
endroits, l'appel partait vers le téléphone resté à la maison, et la course
mourait d'un « personne n'a répondu » que rien n'expliquait. Le passager, lui,
regardait une carte où sa voiture sautait d'un quartier à l'autre. **La dernière
connexion gagne désormais ; la précédente est fermée, immédiatement, et
partout** — y compris son socket de positions.

Écrit au long dans `VTC-DRIVER.md` §7 et `FOOD-DELIVERY.md` §2, chacun pour son
public.

- **`device_id` et `device_name` à `POST /auth/login` et `POST /auth/register`.**
  ⚠️ **Le serveur ne peut pas deviner l'appareil** : ni l'adresse IP, ni le
  modèle du téléphone, ni le jeton FCM ne désignent une **installation**.
  `device_id` est une chaîne opaque tirée **une fois** ; `device_name` est le
  libellé lisible, et il ne sert qu'à une chose — que l'écran puisse dire « vous
  vous êtes connecté sur **Itel A70** » au lieu de « erreur de connexion ».
- ⚠️ **RANGEZ `device_id` À CÔTÉ DU JETON DE RAFRAÎCHISSEMENT, MÊME DURÉE DE
  VIE.** Tiré à chaque lancement, ou rangé dans un cache que le système a le
  droit de vider, **l'application se chasse elle-même à chaque ouverture** :
  « vous avez été déconnecté » en boucle, sur un seul téléphone. Et si la lecture
  échoue, **n'en tirez pas un neuf — n'envoyez pas le champ** : sans lui le
  serveur ne touche à rien.
- **Un refus NOMMÉ, partout : `401 session_superseded`**, sur
  `POST /auth/refresh` comme sur n'importe quel appel authentifié, avec
  **`error.reason` = le libellé de l'appareil qui a pris la place**. Le socket du
  suivi, lui, se ferme en **4409 `session_superseded`**.
- ⚠️ **NE RAFRAÎCHISSEZ PAS, NE VOUS RECONNECTEZ PAS AUTOMATIQUEMENT.** Un `401`
  déclenche d'ordinaire un rafraîchissement puis un rejeu ; ici le
  rafraîchissement répond le **même** code. Et une reconnexion automatique
  chasserait à son tour l'autre téléphone, qui chasserait celui-ci : les deux
  appareils se renvoient la balle indéfiniment et aucun ne travaille. **Testez
  `error.code` avant de rafraîchir**, et ne traitez pas `4409` comme `4401`.
- ⚠️ **NE VIDEZ JAMAIS LA FILE HORS LIGNE sur ce refus.** Ce sont des courses
  réellement conduites, que le serveur n'a jamais vues. Tant que l'ancien jeton
  d'accès vit, `POST /rides/sync` et `POST /deliveries/sync` **répondent
  encore** — ce sont les seules routes qu'un appareil chassé atteint, parce que
  raconter le passé ne crée aucun second flux de positions. Au-delà, il faut se
  reconnecter **sur ce téléphone** pour la rendre, et les deux routes sont
  idempotentes : rien n'est compté deux fois.
- ⚠️ **CHASSÉ EN PLEINE COURSE : rien n'est annulé.** La course appartient au
  COMPTE, pas à l'appareil, et se reprend sur le nouveau téléphone à l'état exact
  où elle en était. Mais plus personne n'émet de position : « course hors radar »
  côté exploitation après **2 min**, mise hors ligne par le serveur après
  **5 min**. En revenant, poussez le trou avec `backfill: true` — c'est
  `actual_distance_m` qui en dépend. **La plateforme ne refuse jamais la
  connexion parce qu'une course est en cours** : un chauffeur dont le téléphone
  meurt en pleine course doit pouvoir reprendre ailleurs à l'instant même.
- ⚠️ **UNE RÉINSTALLATION N'EST PAS UN SECOND TÉLÉPHONE.** L'identifiant part
  avec le jeton, un neuf est tiré, il chasse un appareil qui n'existe plus :
  aucune conséquence. Envoyez le **même `device_name`** — c'est ce qui permet au
  support de distinguer « nouvelle installation » de « quelqu'un d'autre a mon
  compte ». Et ne tirez pas d'identifiant neuf à une mise à jour, à une rotation
  de jeton FCM ou à un redémarrage.
- **Une notification prévient le compte** (catégorie `security`, non coupable) :
  « Votre compte vient d'être ouvert sur *X*. L'appareil précédent a été
  déconnecté. » ⚠️ Elle est adressée au **compte**, donc elle arrive **aussi sur
  le nouveau téléphone** — d'où une rédaction en forme d'annonce, pas de
  reproche. Ne la transformez pas en écran d'erreur sur l'appareil qui vient de
  se connecter.
- ⚠️ **CLIENTS, PASSAGERS ET MARCHANDS NE SONT PAS CONCERNÉS**, et c'est écrit
  dans leurs trois documents : un client garde sa tablette **et** son téléphone,
  une enseigne garde sa caisse, sa cuisine et le téléphone du gérant. N'envoyez
  pas `device_id` pour eux ; ils ne recevront jamais `session_superseded`.
- ⚠️ **`device_id` omis ne vérifie rien**, comme `app` avant lui : une version
  pas encore mise à jour continue de fonctionner — mais le second téléphone y
  entre aussi. **La règle ne mord que lorsque TOUTES les versions en circulation
  envoient le champ.** C'est la raison de l'envoyer dès celle-ci.

### 4.34.0 — 28 septembre 2026

🧵 **LE FIL D'UNE REQUÊTE — `X-Request-ID`.** Un geste dans une application
traverse jusqu'à quatre services ; jusqu'ici, l'identifiant qu'un service posait
sur une requête **mourait à la première frontière** : le suivi, le socle et les
cartes en tiraient chacun un neuf, et les quatre moitiés de la même histoire ne
se retrouvaient jamais. Le même voyage maintenant de bout en bout.

Ce que cela change pour une application — écrit dans les cinq documents,
section *Le fil d'une requête* :

- **Chaque réponse porte un `X-Request-ID`**, y compris un refus, y compris un
  `500`. Lisez-le, gardez-le avec l'erreur que vous enregistrez, citez-le dans
  un ticket : il mène à ce qui s'est passé dans les quatre journaux d'un coup.
  Un `5xx` est d'ailleurs rangé côté serveur **avec ce fil dedans**.
- **Vous pouvez envoyer le vôtre**, il est repris tel quel. ⚠️ **C'est le seul
  moyen de retrouver un appel dont la réponse n'est jamais arrivée** — délai
  dépassé, tunnel, réseau coupé entre la question et la réponse : il n'y a rien
  à lire, et ce sont exactement ceux-là qu'on cherche. Son absence de nos
  journaux répond aussi : la requête ne nous a jamais atteints.
- **64 caractères au plus**, dans `a-z A-Z 0-9 - _ .` ⚠️ Hors de ces règles il
  est **refusé, pas tronqué** (tronqué, deux fils différents se confondraient) —
  on tire le nôtre, et la réponse dit lequel a été retenu.
- ⚠️ **Jamais de donnée personnelle dedans** : ce mot est recopié dans les
  journaux de six services, et une base de journaux ne se purge pas comme un
  compte se supprime.
- ⚠️ **Un par requête** — le même partout regrouperait tout et n'identifierait
  rien. Sur une **reprise du même appel**, en revanche, le garder est utile.
- ⚠️ **Ce n'est pas une référence métier** : le fil nomme un APPEL, pas une
  course ni une commande. Le confondre avec `client_ref` ferait refuser des
  courses bien réelles.

### 4.33.0 — 26 septembre 2026

🛠️ **CE QUE L'ÉQUIPE DE L'APP CHAUFFEUR A RELEVÉ** — relevé au curl sur la
recette, reproduit ici, corrigé (`VTC-DRIVER.md` §4 ter et §4 quater).

- ⚠️ **`tariff_version` est maintenant sur `GET /drivers/me`.** Elle ne
  voyageait que dans le `meta` de l'appel : un chauffeur qui ne reçoit **aucun
  appel** de la journée — celui qui ne fait que des compteurs, justement le plus
  concerné — n'apprenait jamais que sa grille avait changé.
- ⚠️ **`403 vehicle_not_yours` au démarrage d'un compteur.** Un `vehicle_id`
  inventé passait : sans véhicule pas de classe, sans classe pas de grille, et
  **deux heures de compteur facturées 0 F**.
- ⚠️ **La cadence `normal` passe à 30 s / 40 m**, et le contrat explique enfin
  pourquoi : **les deux déclencheurs ne gouvernent pas la même chose**. En
  mouvement c'est la **distance** (40 m à 30 km/h = cinq secondes) ; à l'arrêt
  c'est l'**intervalle**, un simple battement de cœur. Et il a une **borne dure
  de 60 s** : au-delà, le vivier d'appel oublie le véhicule et **rien ne le dit
  au chauffeur**.
- **`speed_limit_kmh`** dans `GET /settings/dispatch` : alertez au-delà. ⚠️ Pas
  une sanction, `0` = aucune alerte, et **n'inventez pas de seuil**.
- **`tiers` et `surge` sont décrits.** ⚠️ **`multiplier` est en MILLIÈMES**
  (1400 = ×1,4), à appliquer **avant** l'arrondi ; les majorations ne touchent
  **ni le compteur ni la location**.
- **`auto_closed`** sur un élément de `POST /rides/sync` : un compteur
  abandonné hors ligne, c'est au téléphone de le fermer — nous ne pouvons
  fermer que ce que nous connaissons.

### 4.32.2 — 26 septembre 2026

📴 **PRÉCISIONS SUR LE HORS-LIGNE** (`VTC-DRIVER.md` §4 quater).

- ⚠️ **N'exigez rien avant d'envoyer une resynchronisation — surtout pas d'être
  en ligne.** Les barrières du départ (en ligne, non suspendu, dette sous le
  plafond) disent qui a le droit de **commencer** à travailler ; elles ne
  s'appliquent pas à une course déjà faite. Le serveur ne les vérifie plus, et
  l'application ne doit pas les vérifier à sa place : c'est **en rentrant chez
  lui, hors ligne**, qu'un chauffeur vide sa file. Le seul refus qu'une course
  bien formée peut recevoir est **`403 vehicle_not_yours`** — « est-ce sa
  voiture ? » ne dépend pas du temps.
- Le champ **`geo`** est nommé explicitement : `POST /rides/free` au départ,
  `PATCH /rides/{id}/status` à l'arrêt. Facultatif — un téléphone sans fix ne
  doit pas empêcher de travailler — mais sans lui la facture dit « de nulle
  part à nulle part ».

### 4.32.1 — 26 septembre 2026

📴 **RECTIFICATION — le livreur peut rejouer ses gestes, pas seulement ses
positions** (`FOOD-DELIVERY.md` §6).

La 4.32.0 écrivait, il y a une heure, que « changer l'état d'une livraison
demande le réseau ». **C'est faux depuis `POST /deliveries/sync`** : collecter
et terminer se rejouent comme les gestes d'une course VTC.

⚠️ **Une différence à connaître avec `POST /rides/sync`** : l'idempotence se
lit dans **l'état**, pas dans une clé. Ces deux gestes portent sur un objet qui
existe déjà ; `client_ref` ne sert qu'à relier la réponse à la ligne de votre
file. Conséquence pratique : **« déjà fait » est une réussite**
(`outcome: duplicate`), pas un refus — mais **une collecte hors séquence reste
un échec**.

⚠️ **Ce qui reste impossible hors ligne** : **accepter** une course. Elle est
proposée à plusieurs livreurs à la fois, et l'accepter de mémoire reviendrait à
promettre une course qu'un autre a déjà prise.

### 4.32.0 — 26 septembre 2026

📴 **HORS LIGNE — une course ne s'arrête pas parce que le réseau s'arrête**
(`VTC-DRIVER.md` §4 quater, `VTC-CLIENT.md` §6, `FOOD-DELIVERY.md` §6).

Le chauffeur conduit, le passager descend, il paie en espèces : tout cela a
lieu sans nous. Ce qui manquait, c'est le **récit** — et de quoi le reprendre.

**1. La grille en poche** — `GET /tariffs/snapshot`

Un **seul** document : monnaie, arrondi, grille ordinaire et grilles des modes
par véhicule, zones de majoration, politique des modes, cadences de suivi. Vous
ne choisissez pas le moment où le réseau tombe, et ce que vous avez en poche
doit être complet et cohérent.

- ⚠️ `version` empreinte le **contenu** → revalidez avec `ETag` /
  `If-None-Match` et recevez `304`. Une application qui retélécharge sans
  raison à chaque retour de réseau finit par ne plus télécharger du tout.
- La même version voyage dans le **`meta` de chaque appel**
  (`tariff_version`) : c'est le seul moment où l'on est sûr que le téléphone
  écoute.
- ⚠️ **Ce que le téléphone calcule est une ESTIMATION.** Affichez le mot, et
  n'imprimez pas de reçu définitif hors ligne.

**2. La file locale et la resynchronisation** — `POST /rides/sync`

- ⚠️ **`client_ref` est tirée au moment du GESTE**, pas de l'envoi. Tirée à
  l'envoi, elle change à chaque tentative et l'idempotence ne sert plus à
  rien — c'est l'erreur qui fait les courses en triple.
- ⚠️ **`200` même avec des refus**, un résultat **par élément**. Un lot traité
  en bloc serait rejoué indéfiniment sur son seul élément fautif, et **rien ne
  passerait plus jamais**.
- `retryable` sépare nos pannes des refus définitifs. ⚠️ Un refus définitif se
  **montre** au chauffeur : retirer en silence un travail réel est exactement
  ce qu'il ne faut pas faire.
- Réessais **exponentiels avec ±30 % de hasard** : quand une antenne revient,
  tous les téléphones du quartier réessaient à la même seconde.

**3. Les positions rattrapées** — `backfill: true` + le `ts` d'origine

⚠️ **Un rattrapage sans `ts` est refusé** : daté de maintenant, il prétendrait
dire où l'on est. Une position ancienne ne remplace jamais la courante, n'est
pas rediffusée aux passagers, et **échappe à la limite de cadence**. Le
parcours est remis en ordre du temps avant d'être mesuré — c'est lui qui donne
la distance, donc le prix.

**4. Les cadences** : 5 s (course commandée) · 20 s (compteur) · 30 s
(location). ⚠️ **Espacer n'est pas éteindre** : une course qu'on ne suit plus
est une course qu'on ne sait plus facturer.

**5. Deux minutes de silence, et l'exploitation le voit.** ⚠️ Une alerte, pas
une sanction : la console n'annule rien, la course continue.

**6. Le passager** : ⚠️ **ne dites jamais qu'une course est perdue** parce
qu'une position manque. La voiture **saute** à sa vraie position après une
coupure — c'est la vérité de la route, pas un défaut.

**7. Les points d'un compteur ou d'une location sont relevés par le téléphone
et NOMMÉS par le serveur** (géocodage inverse) : un géocodeur demande du
réseau, celui-là même qui manque.

### 4.31.0 — 26 septembre 2026

💰 **CHAQUE MODE DE COURSE SE TARIFE SUR LA VOITURE** — et les réglages du
pays ne portent plus d'argent du tout (`VTC-CLIENT.md` §4 quater,
`VTC-DRIVER.md` §4 ter).

Un van immobilisé cinq heures n'est pas une eco ; un compteur de rue n'a
demandé ni recherche ni attente. Le prix **et la commission** dépendent donc
du couple (mode × voiture), pas du seul mode.

**Ce qui déménage**

```
GET /classes → items[].modes = {
      "free":   { base_xof, per_km_xof, per_min_xof, min_fare_xof },
      "rental": { tiers: [ { hours, price_xof, included_km } ], extra_per_km_xof } }
```

- ⚠️ **`GET /settings/modes` NE PORTE PLUS AUCUN PRIX.** Il lui reste la
  POLITIQUE — `free.enabled`, `free.scannable`, `free.max_hours`,
  `rental.enabled`, `rental.max_radius_km`, `rental.alert_km_before` — c'est-à-
  dire ce qui ne dépend **pas** du véhicule. Une application qui lirait encore
  `rental.tiers` n'y trouverait **plus rien**, et n'aurait aucune durée à
  proposer.
- ⚠️ **UNE CLÉ PRÉSENTE DANS `modes` = UN MODE VENDU PAR CETTE VOITURE, DANS
  CE PAYS.** Le serveur croise les deux conditions à la source : `modes` ne
  contient jamais un mode que le pays a fermé. Plus de recoupement à faire
  écran par écran — le premier qui l'oublierait proposerait une location
  refusée à la commande, au pire moment.
- ⚠️ **IL N'Y A PLUS DE GRILLE COMMUNE À RECOUPER.** La v4.30.0 mettait un
  `class_key` par ligne, les lignes sans `class_key` valant pour les autres, et
  laissait l'emboîtement à l'application ; une seule qui inversait l'ordre
  affichait un van au prix d'une eco. Chaque voiture porte maintenant ses
  plages **entières** : prenez-les telles quelles.
- Une voiture **sans** `modes.rental` ne se loue pas, **sans** `modes.free` ne
  fait pas le compteur — retirez-la de l'écran correspondant. Elle reste
  commandable normalement.
- Côté chauffeur : le bouton du compteur dépend de **la voiture conduite ce
  jour-là**, et le tarif du compteur est lisible dans le catalogue.

Rien à changer dans `POST /rides/rental/quote` : `class_key` et `hours` y
étaient déjà, et c'est le serveur qui y applique la bonne grille.

### 4.30.0 — 26 septembre 2026

💰 **LA LOCATION SE TARIFE PAR MODE DE VÉHICULE** (`VTC-CLIENT.md`
§4 quater).

Une journée de van ne se vend pas au prix d'une journée d'eco : ce n'est ni
le même véhicule, ni le même carburant, ni le même chauffeur qu'on
immobilise.

- ⚠️ **`class_key` devient OBLIGATOIRE** sur `POST /rides/rental/quote`, et
  **il sert deux fois** : il choisit le prix **et** il décide qui sera
  appelé. Sans lui, une location faisait sonner tous les modes, et un van
  pouvait répondre à une course vendue au tarif eco.
- Chaque ligne de `rental.tiers` porte un `class_key`. ⚠️ **Une ligne SANS
  `class_key` vaut pour tous les modes qui n'ont pas la leur** — c'est ce qui
  permet de vendre une grille unique sans la recopier trois fois.
- ⚠️ **LE MODE EXACT L'EMPORTE TOUJOURS** sur la ligne commune. Appliquer la
  commune en premier afficherait un van au prix d'une eco.
- ⚠️ **NE PROPOSEZ QUE LES DURÉES DU MODE CHOISI** : les siennes, plus les
  communes. Un passager qui choisit « 24 h » sur une eco qui ne se loue pas à
  la journée se verrait refuser après coup — pire qu'une durée jamais
  proposée. L'exemple complet est dans la spec.

Côté chauffeur : le forfait affiché sur l'appel dépend de **son** mode.

### 4.29.0 — 26 septembre 2026

🚕 **DEUX MODES DE COURSE EN PLUS**, et chacun s'allume **par pays**
(`GET /settings/modes`).

⚠️ **LISEZ CE RÉGLAGE AVANT DE MONTRER QUOI QUE CE SOIT.** Un mode éteint doit
**disparaître de l'écran**, pas y rester et répondre `409` — un bouton qui
échoue se lit comme une panne. Et n'écrivez jamais les durées d'une location
en dur : c'est ce réglage qui dit ce que le pays vend.

**1. La course LIBRE — le taxi qu'on hèle** (`VTC-DRIVER.md` §4 ter,
`VTC-CLIENT.md` §4 quater).

Le chauffeur lance un compteur sans que personne n'ait commandé. **Le prix
naît à la FIN**, de ce qui a été réellement roulé. Un client qui monte scanne
le code affiché pour suivre la course et recevoir la facture.

- ⚠️ **`fare_xof` vaut 0 pendant toute la course.** N'affichez pas « 0 F » :
  dites « compteur en cours », et montrez le montant à `completed`.
- ⚠️ **Le chauffeur doit pousser ses positions sous `mission_id` =
  l'identifiant de la course**, dès le démarrage — c'est ce tampon qui donne
  la distance, donc le prix. Sans lui, la course est facturée à la durée
  seule.
- ⚠️ **Deux temps côté client : montrer, puis rattacher.** `GET` rend le
  chauffeur et la voiture ; `POST …/join` engage la facture. Un scan qui
  rattache d'un coup fait des réclamations.
- ⚠️ **Rattacher ne change pas le moyen de paiement** : la course reste en
  espèces. Le client obtient la facture, pas une autre façon de payer.
- Le code fait **6 caractères sans O/0, I/1 ni S/5** — il se recopie à la
  main quand la caméra refuse. Acceptez la saisie manuelle.
- ⚠️ **`auto_closed: true`** : la plateforme a fermé un compteur oublié. La
  course est **terminée et facturée**, jamais annulée — elle a eu lieu.

**2. La LOCATION — un chauffeur retenu pour une durée** (mêmes sections).

Le passager achète **du temps**, au forfait de la plage vendue par son pays.

- ⚠️ **Pas de destination à demander** : un seul point, le départ. Un écran
  qui réclamerait une arrivée empêcherait de commander ce que le passager
  veut justement.
- ⚠️ **La durée doit être une plage VENDUE** (`tiers`). « 3 h » entre 1 h et
  5 h est refusé : interpoler inventerait un prix que personne n'a décidé.
- ⚠️ **DITES LES TROIS GARDE-FOUS AVANT DE RÉSERVER** — kilomètres compris,
  prix du km au-delà, rayon autour du départ. « Cinq heures » ne veut pas
  dire « cinq heures de route », et un passager qui découvre ces bornes à la
  facture n'a pas acheté ce qu'on lui avait promis.
- ⚠️ **`PUT /rides/{id}/rental-stops`, PAS `PATCH /rides/{id}/stops`.**
  L'autre route recalcule le prix ; ici le prix ne change **jamais** avec le
  trajet. **Les deux côtés peuvent l'appeler** — le passager donne ses
  destinations au fur et à mesure, le chauffeur note celles qu'on lui a dites
  de vive voix.
- ⚠️ **`rental_ends_at` court à la MONTÉE À BORD**, pas à la commande :
  absent tant que le passager n'est pas monté, donc pas de compte à rebours
  avant.
- ⚠️ **Un écran d'appel DISTINCT côté chauffeur** (`meta.mode`,
  `rental_hours`, `no_destination`). Il a cinq secondes pour décider et
  bloque des **heures** : accepter une location en croyant prendre une course
  de quinze minutes se répare en annulant, ce qui pénalise tout le monde.
- `422 rental_out_of_range` porte `distance_km` **et** `max_km` : affichez
  les deux. Un « trop loin » sans chiffres laisse deviner de combien.

### 4.28.0 — 26 septembre 2026

**La carte ne clignote plus, et le décompte suit les étapes.**

**1. L'accueil du socket porte la dernière position connue**
(`VTC-CLIENT.md` §6, `FOOD-CLIENT.md` §6). `hello` arrivait nu : il fallait
attendre la trame suivante — jusqu'à plusieurs secondes — pour savoir où
était le véhicule. À chaque reconnexion (réseau retrouvé, application revenue
au premier plan, jeton rafraîchi), la carte se vidait puis se remplissait.
Elle porte maintenant les mêmes champs qu'une trame `position` : **dessinez
dès l'accueil**.

⚠️ `ts` est l'horodatage du **dernier point reçu**, pas de la connexion — il
peut dater de quelques dizaines de secondes. ⚠️ Un `hello` **nu** reste
possible (véhicule pas encore attribué, téléphone silencieux) : ne le
dessinez pas comme s'il portait des coordonnées.

**2. La reconnexion automatique devient une EXIGENCE**, plus une bonne
pratique. Tant qu'une course ou une livraison est vivante, l'application
rouvre le socket toute seule : back-off 1 s → 30 s, et **immédiatement** au
retour au premier plan. Un socket tombé sans reconnexion laisse un véhicule
figé sur la carte, et rien à l'écran ne dit que l'information a cessé
d'arriver. Le tableau des cas est dans chaque spec.

**3. Le décompte porte sur le PROCHAIN arrêt** (`VTC-CLIENT.md` §5) :
`eta_stop_index` + `eta_at` sur `GET /rides/{id}`.

⚠️ La seule durée servie était `duration_s`, celle du devis — le trajet
entier. On ne pouvait donc décompter que l'arrivée **finale** : pendant
l'approche, le passager qui attend sur le trottoir n'avait aucun chiffre, et
un trajet à plusieurs arrêts sautait les étapes intermédiaires.

`eta_at` est un **instant**, à décompter localement — pas une durée à
réinterroger. `eta_stop_index` vaut `0` pendant l'approche (le départ), puis
l'arrêt suivant celui atteint. ⚠️ **Les deux champs absents veulent dire « on
ne sait pas »** (chauffeur silencieux, position de plus de 2 min, moteur
d'itinéraire muet, chauffeur à l'arrêt) : n'affichez alors **aucun** chiffre,
surtout pas le dernier connu.

Côté chauffeur (`VTC-DRIVER.md` §4) : ce sont **ses positions** qui font ce
décompte — émettre dès l'acceptation, et prévenir le chauffeur quand
l'émission s'arrête.

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
3. ⚠️ **`GET /classes?near=lng,lat`** — le catalogue des modes doit suivre
   le lieu comme le prix. Sans `near`, vous afficheriez les modes de chez
   soi avec les prix d'ici, et un mode servi là-bas mais absent chez soi
   n'aurait aucune ligne où s'afficher.

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

**Un vocal peut être dans une langue d'ici (v4.39.0).** Douze langues sont
nommées aux modèles — français, anglais, et dix langues des cinq pays :
éwé, kabiyè, wolof, peul, sérère, malinké, soussou, arabe tchadien, ngambay,
fang. L'application peut déclarer la langue parlée (`language=wo`) ; sinon
le serveur prend la **première langue du pays**, réglée par l'exploitation.
Le texte revient **dans la langue parlée**, mots français compris.

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
