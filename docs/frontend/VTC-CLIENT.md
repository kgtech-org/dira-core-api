# App CLIENT — COURSES (VTC) — contrat d'API

> **Version 4.15.2** · 22 septembre 2026
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
{ "items": [ { "key": "eco", "name": "Éco", "note": "Citadine · 1-4 passagers", "seats": 4,
              "icon_url": "https://files.dira.llc/class/…/eco.png",
              "map_icon_url": "https://files.dira.llc/class/…/eco-map.png",
              "map_icon": "voiture",
              "waiting_free_min": 5, "waiting_per_min_xof": 100,
              "bill_actual_time": true, "time_tolerance_min": 10, "per_min_xof": 50 } ] }
```

**Ce qui peut s'ajouter au prix en route (v4.14.0)** — quatre nombres et un
drapeau, par mode, à montrer **avant** que le passager ne choisisse :
`waiting_free_min` minutes d'attente offertes au départ puis
`waiting_per_min_xof` la minute ; et, si `bill_actual_time` est vrai, les
minutes roulées au-delà de la durée prévue du devis, tolérance
`time_tolerance_min` déduite, à `per_min_xof` chacune. Une ligne suffit :
« 5 min d'attente offertes puis 100 F/min · au-delà de 10 min de retard sur
la durée prévue, 50 F/min ». Ces termes sont **recopiés sur la course au
devis** : ce que le passager a lu en commandant est ce qui s'applique.

**Les modes se règlent depuis la console (v4.5.0)** : leur nom et leurs
icônes — des **images envoyées par l'exploitation** — peuvent changer, et
un mode peut s'ajouter. **N'écrivez ni le nom ni l'icône en dur** :
affichez `icon_url` devant `name`, tels que servis. `map_icon_url` est
l'image du marqueur pour une application qui dessine des cartes ; à
défaut (`null`), `map_icon` nomme une silhouette de repli (`voiture` ·
`berline` · `suv` · `van` · `moto` · `tricycle` · `velo` · `pieton`) —
sans image ni silhouette connue, dessinez `voiture`. Mettez les images en
cache par URL : elles changent d'URL quand elles changent. La `key`
reste l'identifiant technique (devis, course) — jamais un libellé.

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

## 3 bis. 🗣️ Commander EN PARLANT — l'assistant du passager (v4.15.0)

```
POST /ai/chat   { "message": "…", "origin": [lng, lat] }   → { reply, plan? }
```

« Je veux aller de Tokoin à l'aéroport en van » rend une **phrase** et,
quand il y a de quoi composer, un **plan vérifié** :

```jsonc
{ "reply": "Je vous propose un van, il y en a un tout près.",
  "plan": {
    "stops": [ { "kind": "pickup", "label": "Tokoin, Lomé", "geo": [1.21, 6.15] },
               { "kind": "dest",   "label": "Aéroport de Lomé", "geo": [1.2545, 6.1656] } ],
    "quotes": [ { "id": "6aa2…", "class_key": "van", "fare_xof": 4200, "expires_at": "…" },
                { "id": "6aa3…", "class_key": "eco", "fare_xof": 2500, "expires_at": "…" } ],
    "unresolved": [], "when": "" } }
```

> ⚠️ **`reply` est une phrase à lire ; seuls `plan.quotes` commandent.**
> Chaque lieu a été **géocodé et borné à la ville desservie** par le
> serveur, chaque mode validé, et **le prix vient du devis** — jamais du
> modèle, qui n'a pas le droit d'annoncer un montant. Pour commander, c'est
> `POST /rides { "quote_id": … }` (§4), exactement comme un devis composé à
> la main. **L'assistant ne commande jamais.**

Ce qu'il faut envoyer et afficher :

- **`origin`, la position du téléphone**, à chaque appel si vous l'avez :
  elle sert de départ quand le passager n'en nomme pas (« je vais à
  l'aéroport »). Sans elle et sans départ nommé, la réponse vient **sans
  plan** — demandez la position, ne devinez pas.
- **`plan.stops` sur la carte** avant les prix : le passager doit voir
  *où* on l'emmène avant de voir *combien*. Un lieu mal compris se corrige
  d'un appui, sur la carte, pas dans une conversation.
- **`plan.quotes` en liste de modes**, dans l'ordre servi — celui que le
  passager a nommé est déjà en tête. Les devis **expirent** (`expires_at`,
  2 min) : passé ce délai, redemandez (§3), n'envoyez pas un `quote_id`
  périmé.
- **`plan.unresolved`** nomme ce que la carte ne connaît pas (« chez Tantie
  Adjo ») : dites-le et proposez la recherche d'adresse. Un **arrêt**
  introuvable ne fait pas échouer la course ; une **destination**
  introuvable, si — il n'y a alors pas de plan.
- **`plan.when`** (« dans 20 minutes ») est rendu **tel quel** :
  l'assistant ne programme pas. Si c'est une course à l'avance, envoyez le
  passager sur l'écran de programmation (§4 bis).
- `plan` **absent** = ce n'était pas une demande de course (un bonjour, une
  question de prix). Affichez la phrase, rien d'autre.

`502`/`503 assistant_unavailable` : l'assistant est indisponible — dites-le
et **laissez la composition manuelle**, qui ne dépend d'aucun modèle.

### 🎙️ Le VOCAL — 60 s, transcrit, MONTRÉ, puis envoyé

```
POST /ai/voice                multipart : file=<audio>        → { "text": "…" }
```

⚠️ **La transcription n'est PAS exécutée.** Elle revient à vous ; vous
l'**affichez dans le champ de saisie**, corrigible, et c'est le client qui
l'envoie à `/ai/chat`. Les accents d'ici ne pardonnent pas : partir à
« l'aéroport » quand quelqu'un a dit « la gare » est pire que pas
d'assistant du tout.

**Enregistrez en opus ou AAC, MONO, 16-24 kbit/s** — 60 s pèsent alors
~150 Ko. Le WAV est refusé : une minute fait 5 Mo, soit **des minutes**
d'envoi sur un réseau lent, et c'est l'envoi, pas l'IA, qui fait attendre.
Limite **2 Mio**, ~60 s.

| Étape | à montrer | 3G lent | 4G |
|---|---|---|---|
| envoi de l'audio | la barre d'envoi | 10-16 s | 1-2 s |
| transcription | « transcription… » | 1-3 s | 1-3 s |
| réponse | « l'assistant écrit… » | 2-10 s | 2-10 s |

Une seule requête en vol. Pas de réessai automatique en boucle : un bouton,
une fois. `413 audio_too_large` = l'application n'a pas compressé ;
`503 assistant_unavailable` = la voix n'est pas servie — **gardez le clavier
disponible**, tout marche sans elle.

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
| `wallet` | le solde est **vérifié** (`402` s'il ne couvre pas) — il n'est **débité qu'à l'acceptation** d'un chauffeur (v4.4.0) | **remboursé** sur le portefeuille si un chauffeur avait accepté ; rien à rendre avant |
| `online` | `payment_url` est rendue — **rien n'est encaissé** | remboursé **si** le paiement a été confirmé |

> ⚠️ **`402 insufficient_funds`** sur `wallet` : le solde ne couvre pas la
> course. Ce n'est pas une panne — routez vers la recharge du portefeuille
> (`POST /wallet/purchase`, **au socle**), pas vers un message d'erreur.
>
> **Le solde part à l'acceptation, pas à la commande (v4.4.0).** Une
> recherche sans preneur ne fait pas partir puis revenir l'argent, et un
> passager qui annule pendant la recherche n'a rien à se faire rendre. Le
> solde est **vérifié** à la commande pour que personne ne roule pour une
> course impayable. Si le solde a fondu entre la commande et l'acceptation
> (dépensé ailleurs), la course est **annulée** — `status: cancelled`,
> `cancelled_reason: "payment_failed"`, push `ride_cancelled` — et le
> passager doit recharger avant de recommander. Affichez le solde sur
> l'écran de recherche : c'est lui qui paiera quand un chauffeur dira oui.
>
> **Le pays peut débiter à une autre étape (v4.12.0).** La course rend
> **`charge_at`** : `accept` (le défaut ci-dessus), `request` (débité à la
> commande — remboursé si annulée), `start` (à la montée à bord) ou
> `complete` (déposé). Affichez « débité à … ». Après `accept`, la course
> ne s'annule plus pour un solde insuffisant : à la montée ou à l'arrivée,
> l'impayé devient la **dette** du passager (`GET /wallet` → `debt_xof`),
> remboursée d'office sur sa prochaine recharge.

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
searching → accepted → picking_up → [arrived] → in_transit → completed
                                                       ↘ cancelled   (tout état avant completed)
```

| Statut | Ce que ça veut dire | Course de livraison | Course VTC |
|---|---|---|---|
| `searching` | on cherche quelqu'un | la course attend un livreur — proposée dès que le repas est **prêt** | on appelle des chauffeurs |
| `accepted` | quelqu'un a pris l'opération | un livreur l'a acceptée, il part vers le restaurant | un chauffeur l'a prise |
| `picking_up` | il est au point de départ | la **première collecte** est faite, il en reste | il **roule vers le passager** |
| `arrived` | il est arrivé et attend (v4.14.0) | — (une course de livraison passe directement à `in_transit`) | il est **au point de départ**, le passager n'est pas encore monté ; l'attente offerte court, puis se facture à la minute |
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
| `searching` | « nous cherchons un chauffeur » — `driver_id` est vide ; `dispatch_state` dit si l'on appelle encore (v4.1.0, ci-dessous) |
| `accepted` | un chauffeur a pris la course — `driver` dit qui (v3.8.0) |
| `picking_up` | il roule vers le point de départ |
| `arrived` | il est là et attend (v4.14.0) — `arrived_at`, et le compteur d'attente : `waiting_free_min` minutes offertes, puis `waiting_per_min_xof` la minute |
| `in_transit` | le passager est à bord — `waiting_minutes` / `waiting_fee_xof` disent ce que l'attente a coûté, déjà compris dans `fare_xof` |
| `completed` | terminée |
| `cancelled` | `cancelled_by` dit qui, `cancelled_reason` pourquoi |

```
GET /rides/{id}
GET /rides?cursor=…     # l'historique de VOS courses, page par page
```

L'historique rend **les plus récentes d'abord** — par date de création, puis identifiant (v4.12.1) ; `?cursor=` est l'identifiant de la dernière course reçue et rend la page suivante, plus ancienne.

### ⚠️ Le chauffeur est là — `arrived` et l'attente facturée (v4.14.0)

Quand le chauffeur signale son arrivée au point de départ, la course passe
à **`arrived`** : vous recevez le push **`ride_driver_arrived`** (« [driver]
vous attend · [vehicle]. [free] min d'attente offertes, puis [rate]/min ») et
la trame `status: arrived` sur le socket. **Affichez-le fort, avec un
compteur** depuis `arrived_at` : les minutes offertes (`waiting_free_min`,
5 par défaut) en vert, puis le montant qui monte — `waiting_per_min_xof`
par minute **entamée**. Les deux nombres sont sur la course dès le devis,
figés : ce que le passager a vu en commandant est ce qu'il paie.

À la montée à bord (`in_transit`), la course porte `waiting_minutes` et
`waiting_fee_xof`, **déjà ajoutés à `fare_xof`**, et une ligne
`fare_adjustments` (`reason: "waiting"`, `delta_xof`, `movement`). Le
push `ride_fare_adjusted` dit ce qui a bougé : débité du solde Dira
(`charged`), porté à la dette si le solde ne suivait pas (`owed` —
`debt_xof` sur `GET /wallet`, remboursée d'office à la prochaine recharge),
à régler au chauffeur en espèces (`cash`), ou compris dans le débit à
venir quand le pays débite à la montée ou à l'arrivée (`deferred`). Aucune
attente facturée = aucun de ces champs.

Une course `arrived` s'annule encore (`POST /rides/{id}/cancel`) ; le
chauffeur est là, dites-le avant de confirmer.

### ⚠️ Le temps réel — 5 km en une heure ne coûtent pas 5 km en dix minutes (v4.14.0)

Le devis tarife la **durée prévue** (`duration_s`, celle du réseau routier).
Quand le mode le règle (`bill_actual_time` sur la course), la durée
**réellement roulée** — de la montée à bord (`in_transit_at`) à l'arrivée —
compte aussi : chaque minute **entamée** au-delà de la durée prévue plus
`time_tolerance_min` coûte `per_min_xof`. À `completed`, la course porte
`actual_duration_s`, `extra_minutes` et `time_fee_xof`, **déjà compris dans
`fare_xof`**, avec une ligne `fare_adjustments` (`reason: "duration"`) et le
même push `ride_fare_adjusted` (`event: duration`) ; les mouvements
d'argent sont ceux de l'attente (`charged` · `owed` · `cash` · `deferred`).

**Pendant la course, affichez la durée prévue et le temps écoulé** depuis
`in_transit_at`, et dès que la tolérance est dépassée, le supplément qui
monte — le passager ne doit pas découvrir le prix à l'arrivée. Sur le reçu,
une ligne « Temps supplémentaire : 12 min · 600 F » sous le prix du devis.
`bill_actual_time` faux = rien de tout cela, la course coûte son devis
(plus l'attente au départ, s'il y en a eu).

### Quand personne ne répond — `dispatch_state` et la relance (v4.1.0)

La plateforme appelle les chauffeurs autour du départ selon un réglage
d'exploitation (tous en même temps dans un rayon, ou un par un du plus
proche) et **s'arrête** au bout d'un nombre de chauffeurs ou de minutes.
Une course `searching` porte alors **`dispatch_state`** :

| `dispatch_state` | Ce que ça veut dire | Ce que montre l'écran |
|---|---|---|
| `calling` | on appelle des chauffeurs | « nous cherchons un chauffeur », animation |
| `exhausted` | la recherche s'est arrêtée **sans preneur** — la course n'est **pas** annulée | « aucun chauffeur disponible » + deux boutons : **Relancer** et **Annuler** |

`dispatch_at` date le dernier passage. Le passager l'apprend par **push**
`ride_search_exhausted` (`data.type: "ride_status"`, `ride_id`,
`status: "searching"`, `dispatch_state: "exhausted"`) — ouvrir la course,
`GET /rides/{id}`, afficher les deux gestes.

```
POST /rides/{id}/relaunch      → 200 Ride, dispatch_state: calling
POST /rides/{id}/cancel        → remboursé sur le solde si la course était payée
```

| Refus | Quand |
|---|---|
| `409 search_running` | un appel court déjà — masquer « Relancer » tant que `dispatch_state` vaut `calling` |
| `409 not_searching` | la course a été prise, terminée ou annulée entre-temps — relire |
| `503 dispatch_unavailable` | la recherche est momentanément indisponible — proposer de réessayer |

> ⚠️ **N'annulez jamais d'office** une course épuisée : le passager a payé et
> attend peut-être encore devant sa porte ; l'exploitation peut aussi lui
> attribuer un chauffeur à la main. Laissez-lui les deux gestes.

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
vehicle: { class_key, brand, model, license_plate, color, description, photo_url } }`
— qui vient, dans quelle voiture, avec quelle note. **`description`
(v4.10.0)** est GÉNÉRÉE — *marque modèle couleur*, « Toyota Avensis rouge » :
c'est ce que le passager guette, à afficher avec la **plaque** ;
`photo_url` est la photo de couverture du véhicule, quand le chauffeur en a
déposé une. `id` est le **profil** du chauffeur,
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


Sur la carte : le départ et chaque étape sont `stop`, l'arrivée `client` ; le chauffeur est dessiné par son mode (`map_icon_url` de `GET /classes`).

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

### Changer le trajet EN COURS DE ROUTE — un arrêt de plus, un de moins (v4.4.0)

Le passager peut ajouter un arrêt, en retirer un, ou changer la
destination **pendant qu'un chauffeur est assigné** (`accepted`,
`picking_up`, `arrived`, `in_transit`). Il envoie le **nouveau trajet en entier** :

```
PATCH /api/v1/vtc/rides/{id}/stops
{ "stops": [
  { "kind": "pickup", "label": "Almadies",  "geo": [1.2255, 6.1319] },   ← figé
  { "kind": "stop",   "label": "Pharmacie", "geo": [1.2300, 6.1400] },   ← nouveau
  { "kind": "dest",   "label": "Aéroport",  "geo": [1.2545, 6.1656] }
]}
→ 200 Ride  — nouveau `stops`, `distance_m`, `duration_s`, `fare_xof`,
              et `fare_adjustments: [{ from_xof, to_xof, delta_xof, movement, by, at }]`
```

**Ce qui est figé.** Les arrêts **déjà atteints** ne changent plus : le
départ dès qu'un chauffeur roule vers lui, puis chaque arrêt marqué
atteint (`reached_at`). Renvoyez-les **tels quels, en tête** — `422` sinon.
Grisez-les dans l'éditeur : on ne réécrit pas ce qui a eu lieu. Même règle
de forme qu'au devis : 2 à 5 arrêts, `pickup` en premier, `dest` en
dernier, tous dans la ville desservie.

**Le prix suit le trajet.** Il est recalculé sur la grille du jour avec la
**même majoration qu'au devis** — une majoration montée ou retombée
entre-temps ne change pas une course qui roule. Montrez le nouveau prix
**avant** de confirmer : demandez un devis avec le nouveau trajet
(`POST /rides/quote`) pour l'afficher, puis envoyez le `PATCH`. (Le devis
peut différer de quelques francs si une majoration s'est ajoutée depuis :
c'est le `PATCH` qui fait foi.)

**L'argent suit le prix, tout de suite** — `fare_adjustments[].movement` :

| Paiement | `delta_xof > 0` (plus cher) | `delta_xof < 0` (moins cher) |
|---|---|---|
| `wallet` | la différence est **débitée du solde Dira** → `charged` | rendue sur le solde → `refunded` |
| `online` | **débitée du solde Dira** aussi (l'opérateur ne se rappelle pas à chaud) → `charged` | rendue sur le solde → `refunded` |
| `cash` | rien ne bouge : le chauffeur encaisse le nouveau prix → `cash` | idem |

> ⚠️ **`402 insufficient_funds` = le changement est REFUSÉ**, la course reste
> exactement telle qu'elle était. Ce n'est pas une panne : routez vers la
> recharge (`POST /wallet/purchase`, au socle), et proposez de réessayer.
> `409 payment_pending` : une course mobile money pas encore confirmée ne
> change pas de trajet tant que le paiement n'est pas arrivé.

**Les deux sont prévenus.** Le chauffeur reçoit `ride_stops_changed` et
relit la course ; le passager reçoit `ride_fare_adjusted`
(`data.type: "ride_status"`, `event: "stops_changed"`, `fare_xof`,
`delta_xof`) avec ce qui a bougé — relisez `GET /rides/{id}` pour
redessiner le trajet, comme pour tout signal.

`409 ride_not_reroutable` : pas de chauffeur (encore `searching`) ou
course finie — pendant la recherche, annulez et recommandez.

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
`accepted`, `picking_up`, `arrived`, `in_transit`, `completed`, `cancelled`. Elle ne
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
| `ride_driver_arrived` | il est là et attend — les minutes offertes et le tarif ensuite dans le texte (v4.14.0) | `status: arrived`, `arrived_at`, `waiting_free_min`, `waiting_per_min_xof` |
| `ride_cancelled` | annulée par le chauffeur ou la plateforme (`reason`) | `status: cancelled` |
| `ride_search_exhausted` | personne n'a pris la course — relancer ou annuler (§5, v4.1.0) | `status: searching`, `dispatch_state: exhausted` |
| `ride_fare_adjusted` | le trajet a changé en route, le prix aussi — ce qui a été débité ou rendu (§5, v4.4.0) | `event: stops_changed`, `fare_xof`, `delta_xof` |
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

## 7 ter. Le SUPPORT — réclamations et objets perdus (v4.9.0)

> Jusqu'ici l'application n'avait **aucune porte** vers le support : les
> courses ne servaient pas de tickets, et le seul recours était d'appeler.
> Désormais une réclamation se dépose **depuis l'application**, se suit dans
> un fil, et le cas de l'**objet oublié dans la voiture** a son propre
> parcours — parce que c'est dans les minutes qui suivent qu'un sac se
> retrouve, pas quand le support ouvre sa file le lendemain.

```
POST /tickets                       { category, message, ride_id?, priority?, lost_item? }   → 201 ticket
GET  /tickets                       → { items, next_cursor }   mes tickets, du plus récent au plus ancien
GET  /tickets/{id}                  → le ticket et son fil
POST /tickets/{id}/messages         { "body": "…" }   → 201 ticket
```

**Les catégories**, dans l'ordre où l'écran les propose :

| `category` | Quand |
|---|---|
| `ride` | quelque chose s'est mal passé **pendant** une course — trajet, retard, prix : `ride_id` obligatoire |
| `lost_item` | **objet oublié** dans la voiture — parcours à part, ci-dessous |
| `payment` | débit, remboursement, mobile money |
| `tokens` | (réservé aux chauffeurs et marchands — ne pas proposer au passager) |
| `account` | connexion, profil, téléphone |
| `behaviour` | conduite ou comportement du chauffeur : `ride_id` recommandé |
| `other` | le reste |

- `ride_id` rattache la course. **Seul un passager qui l'a vécue** peut s'y
  référer (`403 forbidden` sinon) ; le serveur fige un libellé lisible
  (`ref_label` : « Lomé Centre → Aéroport · 19/09 14:02 ») pour que le fil
  reste compréhensible même des mois plus tard. Depuis l'écran d'une course
  terminée, proposez « Signaler un problème » et « J'ai oublié quelque chose »
  avec `ride_id` déjà rempli.
- `priority` est facultative (`normal` par défaut) : ne la demandez pas au
  passager, c'est le support qui la règle.
- ⚠️ **`order_id` n'existe pas ici** — c'est le mot de la livraison. L'envoyer
  répond `422 fields: ["order_id"], reason: "wrong_vertical"`.

Le ticket rendu :

```json
{ "id": "…", "reference": "TCK-000123", "user_id": "…", "role": "client",
  "category": "lost_item", "priority": "high", "status": "open",
  "ride_id": "…", "ref_label": "Lomé Centre → Aéroport · 19/09 14:02",
  "counterpart_id": "…",                       ← le chauffeur de la course (objet perdu seulement)
  "lost_item": { "item": "Sac à dos noir", "details": "avec un ordinateur",
                 "found": null, "answered_at": null, "note": "" },
  "messages": [ { "author_id": "…", "author_role": "client", "body": "…", "at": "…" } ],
  "created_at": "…", "updated_at": "…" }
```

**Afficher la référence** (`TCK-000123`) : c'est ce que le passager dira au
téléphone. `status` ∈ `open` (déposé) · `in_progress` (pris en charge) ·
`waiting` (on attend le passager) · `resolved` · `closed`. `author_role` dit
qui parle dans le fil — `client`, `admin` (le support), `driver` (sur un objet
perdu) : dessinez trois bulles différentes, jamais un nom.

### 🎒 L'OBJET PERDU — `lost_item`

```
POST /tickets  { "category": "lost_item", "ride_id": "…",
                 "message": "Je l'ai laissé sur la banquette arrière",
                 "lost_item": { "item": "Sac à dos noir", "details": "avec un ordinateur portable" } }
```

Ce qui se passe **côté serveur**, et que l'écran doit dire :

1. `ride_id` **et** `lost_item.item` sont obligatoires (`422` qui les nomme).
   Seul le passager de la course peut le déclarer, et la course doit avoir eu
   un chauffeur — `409 no_driver_yet` sinon (une course annulée en recherche).
2. La priorité monte à **`high`** d'office.
3. **Le chauffeur de cette course est prévenu à l'instant** (notification
   `lost_item_reported`) et devient partie au ticket : il lit le fil, y
   répond, et dit s'il a trouvé l'objet.
4. Le passager reçoit sa réponse : **`lost_item_found`** (« Bonne nouvelle :
   votre Sac à dos noir a été retrouvé. Le support vous contacte pour la
   restitution ») ou **`lost_item_not_found`**. Données
   `{ type: "lost_item", ticket_id, ride_id }` : ouvrir le ticket dessus.
5. Dans le ticket, `lost_item.found` a **trois états** : `null` — le
   chauffeur n'a pas encore regardé ; `true` — retrouvé, `lost_item.note` dit
   où, le statut passe `in_progress` et le support organise la restitution ;
   `false` — pas trouvé, le ticket **reste ouvert**, le support poursuit.
   Affichez les trois différemment : « pas encore regardé » n'est pas « pas
   trouvé ».

⚠️ **La restitution passe par le support**, jamais par un échange de numéros
dans le fil : c'est ce qui protège les deux côtés.

### Le fil, et ce qui réveille l'application

- `POST /tickets/{id}/messages` : le passager écrit ; le support et, sur un
  objet perdu, le chauffeur reçoivent **`ticket_reply`** (« Nouveau message
  sur votre demande TCK-000123 »). Quand c'est le support ou le chauffeur qui
  écrit, c'est le passager qui la reçoit. Données `{ type: "ticket",
  ticket_id, ride_id? }`.
- Quand le support clôt (`resolved` · `closed`) : **`ticket_resolved`**, une
  fois.
- Ces notifications sont dans la catégorie `support`, **non coupable** dans
  les préférences : on a posé une question, on reçoit la réponse.
- Pas de socket : sur l'écran d'un ticket ouvert, relisez `GET /tickets/{id}`
  à l'ouverture et sur chaque notification reçue.

---

## 8. Ce que le SOCLE sert (sans `/vtc`)

| | |
|---|---|
| `POST /auth/register` · `/auth/login` · `/auth/refresh` · `/auth/logout` | la session |
| `GET · PATCH /me` · `PATCH /me/preferences` | le profil |
| `POST /uploads?kind=avatar` | la photo de profil — **v3.0.0**, une seule porte pour toute la plateforme |
| `POST /bug-reports` (**LIVRAISON**, `…/api/v1/food/bug-reports`) | signaler un bug de l'application — pas une réclamation : celles-ci sont §7 ter, sur `/vtc/tickets` |
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
