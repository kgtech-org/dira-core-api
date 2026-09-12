# App CLIENT — COURSES (VTC) — contrat d'API

> **Version 3.4.0** · 12 septembre 2026
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
> connecte pas deux fois. Conventions communes (erreurs, pagination, montants,
> dates) : voir [`FOOD-CLIENT.md` §1](FOOD-CLIENT.md).

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

## 5. La course, statut par statut

```
searching → accepted → approach → onboard → completed
         ↘ cancelled (par le passager, le chauffeur, ou l'échec de la recherche)
```

| Statut | Ce que voit le passager |
|---|---|
| `searching` | « nous cherchons un chauffeur » — `driver_id` est vide |
| `accepted` | un chauffeur a pris la course |
| `approach` | il roule vers le point de départ |
| `onboard` | le passager est à bord |
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

`409 invalid_transition` sur une course `onboard` : le bouton doit disparaître
à ce statut, pas échouer.

---

## 6. Suivre le chauffeur

Le suivi temps réel passe par **`dira-tracking`**, sur son propre hôte — pas
par cette API.

```
WS wss://tracking-staging.dira.llc/track/subscribe/{ride_id}
```

L'identifiant de course sert d'identifiant de mission. Reconnexion + repli REST
comme pour la livraison : voir [`FOOD-CLIENT.md` §5](FOOD-CLIENT.md).

---

## 7. Parler au chauffeur — sans échanger de numéros

```
GET  /rides/{id}/messages          → { items, unread }
POST /rides/{id}/messages          { "body": "je suis au portail bleu" }
POST /rides/{id}/messages/read
```

**Exactement la même mécanique que la conversation de commande** — même
modèle, mêmes règles, même paquet côté serveur.

| Refus | Quand |
|---|---|
| `409 no_driver_yet` | personne n'a encore pris la course — **lisible, pas écrivable** |
| `409 conversation_closed` | plus de **deux heures** après l'arrivée |
| `403 forbidden` | cette conversation n'est pas la vôtre |

> La saisie se désactive sur les deux premiers ; l'historique **reste lisible**.
> Deux heures après l'arrivée : « j'ai oublié mon téléphone sur la banquette »
> se dit dans la minute qui suit, pas un mois plus tard.

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
| `GET /agents/{id}/ratings` | les avis d'un chauffeur |

- **Inscription** : `{ phone (E.164, avec le +), name, password, role: "client", email?, first_name?, last_name? }`. Sans `+`, `422` avec `fields: ["phone"]` ; `phone_taken` (409) → proposer la connexion ; `account_suspended` (403) → le dire tel quel.
- **Un `422` nomme ses champs** (`fields`, `reason`) — voir la liste de contrôle du `README`.
- `GET /wallet` répond toujours `200` à un client : le Dira Cash s'ouvre à la première lecture.

---

## 9. ⚠️ Ce que la maquette demande et que l'API ne sert PAS

Écrit ici pour être découvert **maintenant**, pas à l'intégration.

### ❌ Noter une course

Aucune route. Le dépôt d'une note existe pour la livraison
(`/food/orders/{id}/rating`) et **pas encore pour les courses**. L'écran
d'évaluation de fin de course n'a rien derrière.

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
