# App CHAUFFEUR — COURSES (VTC) — contrat d'API

> **Version 4.32.1** · 26 septembre 2026
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

> **`GET /rides?limit=&cursor=`** — VOS courses, **les plus récentes
> d'abord** (date de création puis identifiant, v4.12.1) ; `?cursor=` est
> l'identifiant de la dernière course reçue et rend la page suivante, plus
> ancienne.

> ⚠️ **`online` et `status` sont DEUX AXES, et ils doivent le rester.**
> `status` est ce que l'administration a décidé (`pending`, `active`,
> `suspended`) ; `online` est ce que le chauffeur choisit maintenant. Les
> confondre laisserait un chauffeur suspendu lever sa propre suspension en se
> remettant en ligne.

> ⚠️ **HORS LIGNE VEUT DIRE AUCUN APPEL — y compris par le véhicule
> (v4.26.0).** L'attribution juge deux choses : le COMPTE et le VÉHICULE.
> Le compte était bien filtré ; le véhicule, lui, ne regardait que son état
> mécanique (`maintenance`), jamais la disponibilité de son propriétaire.
> Quand une vague ne nommait que des véhicules, un chauffeur hors ligne
> pouvait donc encore sonner. Le véhicule interroge désormais son chauffeur :
> hors ligne ou suspendu, il est écarté aussi de cet axe. Si un chauffeur
> hors ligne reçoit encore une course, c'est un bug à signaler, plus une
> tolérance.


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
`key`, `name`, `icon_url` (image), `map_icon_url`, `nav_icon_url` /
`map_icon`. **Affichez
`icon_url` + `name` tels que servis** — les modes se règlent depuis la
console, et un mode peut s'ajouter (v4.5.0). Le `class` d'un appel (§3)
est la `key`.

> ### 🧭 ⚠️ LES DEUX PINS D'UN VÉHICULE, ET LEUR ORIENTATION (v4.20.0)
>
> **Un véhicule a DEUX images de marqueur, pas une.** Elles viennent de votre mode de véhicule — `GET /classes` :
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
> ⚠️ **DANS LES DEUX, LE NEZ POINTE VERS LE HAUT DE L'IMAGE** — le capot (ou
> la roue avant) vers le bord supérieur, quel que soit le véhicule et quelle
> que soit la vue. C'est la convention des images envoyées depuis la console,
> et c'est sur elle que reposent les rotations.
>
> Ne confondez pas les deux angles : **90° et 45° décrivent la CAMÉRA**
> (à la verticale, ou penchée), jamais l'orientation du véhicule dans
> l'image. Celle-ci ne change pas d'une vue à l'autre — c'est précisément ce
> qui permet à votre code de tourner l'une ou l'autre sans rien savoir de
> laquelle il s'agit.
>
> **Appliquez donc `heading` TEL QUEL** comme rotation du marqueur : un cap
> de 0° (plein nord) laisse l'image droite, 90° la tourne d'un quart de tour
> vers la droite. Pas de correction, pas d'offset de −90° — si vous avez
> besoin d'en ajouter un, c'est l'image qui est mal orientée, pas le code :
> signalez-le à l'exploitation plutôt que de le compenser chez vous.
>
> Pourquoi cette règle est écrite : une correction appliquée dans UNE
> application et pas dans les autres fait rouler les véhicules de côté sur
> un seul écran, et personne ne sait lequel a raison.
>
> ⚠️ **Le cap ne tourne pas à l'arrêt.** Un `heading` déduit (`heading_source`
> ≠ `gps`) sur un véhicule immobile pivote dans le vide : gardez la dernière
> orientation plutôt que de l'animer.
>
> ⚠️ **La règle vaut pour les VÉHICULES, pas pour les marqueurs de lieu.** Un
> pin de client, de marchand ou d'étape ne tourne jamais : il désigne un
> endroit, pas une direction.

Sur la carte d'une course : le départ et chaque étape sont `stop`, l'arrivée `client`.

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

**Sur VOS cartes**, les trois points d'une course viennent tous du genre
`client` :

| Le point | Le pin |
|---|---|
| le **départ** (`kind: "pickup"`) — le passager, qui attend | `map_icon_url`, le pin **principal** |
| les **étapes** (`kind: "stop"`) | `numbered`, dans l'ordre de `stops[]` — c'est ce qui vous dit où aller d'abord sans lire les adresses |
| l'**arrivée** (`kind: "dest"`) | `dest_icon_url`, le pin de **destination** |

⚠️ **Ne confondez pas le passager et l'arrivée.** Le pin principal marque
**quelqu'un** à qui vous allez parler ; celui de destination marque un
**lieu** où personne ne vous attend. Les dessiner pareil vous ferait chercher
un client là où il n'y a qu'une adresse.

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

> ⏱️ **VOS POSITIONS FONT LE DÉCOMPTE DU PASSAGER — v4.28.0.** Depuis cette
> version, son application affiche « il arrive dans 4 min », puis « prochain
> arrêt dans 7 min » à chaque étape. Ce chiffre est calculé **depuis votre
> dernière position connue** vers le prochain point de la course.
>
> Deux conséquences pour vous :
>
> - **Émettez dès l'acceptation**, pas seulement une fois le passager à bord.
>   Sans position, le passager n'a aucun chiffre et croit que rien n'avance.
> - **Une position de plus de 2 minutes ne compte plus** : la plateforme se
>   tait plutôt que d'annoncer une arrivée sur la foi d'un point périmé. Si
>   votre application détecte que l'émission s'est arrêtée (veille, réseau,
>   permission retirée), **dites-le au chauffeur** — c'est la première cause
>   d'un passager qui appelle pour demander où vous êtes.
>
> La même course porte `eta_at` pour vous aussi, si vous voulez l'afficher :
> c'est un INSTANT, à décompter localement, et il concerne
> `eta_stop_index`, l'arrêt vers lequel vous roulez.

> 🧳 **Votre passager peut venir d'un autre pays — v4.27.0.** Depuis cette
> version, une course se fait dans le pays où elle SE FAIT, et non dans
> celui où le passager s'est inscrit : un Togolais de passage chez vous
> commande normalement.
>
> **Rien ne change pour vous** : la course est une course de votre ville,
> à votre tarif, avec votre commission — c'est le pays de la COURSE qui
> règle tout, et c'est le vôtre.
>
> ⚠️ **Sauf son NUMÉRO, qui peut porter un autre indicatif** (`+228` chez
> vous au Sénégal). Composez-le **tel que l'API le rend**, en E.164 avec
> son `+` : un numéro raccourci ou complété de votre indicatif n'appelle
> personne. Prévenez aussi que l'appel peut être facturé comme un appel
> vers l'étranger.

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
| `402 insufficient_tokens` | **mode JETONS** (v4.12.0) : accepter coûte `token_cost` jetons pris sur votre portefeuille Dira (`GET /wallet` → `balance`), et il n'y en a pas assez — la course reste à prendre. Rechargez (`POST /wallet/purchase`) |

> **Le pays décide de la façon dont la plateforme se paie (v4.12.0).** En
> mode **commission** (le défaut), rien à l'acceptation et la commission au
> relevé (§6). En mode **jetons**, la course rend **`token_cost`** : ce que
> l'acceptation vous coûte, en jetons, et **il n'y a pas de commission** —
> le prix vous revient en entier (`driver_xof = fare_xof`). Affichez le
> coût sur l'appel, comme le prix. Le pays règle aussi **quand le passager
> au portefeuille est débité** (`charge_at` sur la course) ; ce n'est jamais
> votre affaire : la plateforme vous règle la course de la même façon.

---

## 4. Conduire

```
PATCH /rides/{id}/status                   { "status": "picking_up" | "arrived" | "in_transit" | "completed" }
POST  /rides/{id}/stops/{index}/reached
POST  /rides/{id}/decline                  { "reason": "…" }
GET   /rides/{id}
GET   /rides?cursor=…                      # l'historique de VOS courses
```

```
accepted → picking_up → arrived → in_transit → completed
        ↘ cancelled
```

### ⚠️ Signaler l'arrivée — `arrived`, et l'attente facturée (v4.14.0)

Arrivé au point de départ, **envoyez `{ "status": "arrived" }` avant de
laisser monter** : c'est ce qui alerte le passager (« votre chauffeur est
là ») et ce qui fait courir l'attente. La course rend alors `arrived_at`,
`waiting_free_min` (les minutes offertes — 5 par défaut, réglées par mode
dans la console et **figées sur la course au devis**) et
`waiting_per_min_xof` (le prix de chaque minute entamée au-delà).

**Affichez un compteur** depuis `arrived_at` : vert pendant les minutes
offertes, puis le montant qui monte — le passager voit le même. Au passage
à `in_transit`, la course porte `waiting_minutes` (les minutes facturées,
entamées) et `waiting_fee_xof`, ajoutés à `fare_xof` et à votre part
(`driver_xof`) selon la commission habituelle ; l'historique
`fare_adjustments` en garde la ligne (`reason: "waiting"`). Pour une
course en espèces, **c'est vous qui encaissez** le nouveau prix ; pour le
solde Dira, la plateforme débite — et porte à la dette du passager ce que
son solde ne couvre pas, sans bloquer la montée à bord.

Passer de `picking_up` à `in_transit` sans `arrived` reste accepté (une
application antérieure ne casse pas) — mais alors **aucune attente n'est
facturée** : la plateforme ne devine pas quand vous êtes arrivé. Une course
`arrived` peut encore être annulée, par vous (`POST /rides/{id}/decline`)
ou par le passager.

#### 📍 Votre APPROCHE est enregistrée toute seule (v4.24.0)

**Vous n'avez rien à faire.** Au moment où vous envoyez `arrived`, la
plateforme fige ce que vous avez roulé **pour venir** et l'écrit sur la
course :

```
approach_duration_s   le temps que vous avez mis, depuis l'ACCEPTATION
approach_distance_m   les kilomètres de l'approche
approach_polyline     votre parcours réel (polyline Google [lat, lng])
approach_source       "tracked" (le suivi avait vos positions) · "planned" (il n'avait rien)
```

⚠️ **C'est la moitié de la course que personne ne mesurait**, et c'est la
vôtre : vous la roulez sans être payé pour. Enregistrée, elle vous défend —
« il a mis vingt minutes » se vérifie, et un embouteillage se voit sur le
tracé.

⚠️ **La durée court depuis l'ACCEPTATION**, pas depuis `picking_up` : c'est à
l'acceptation que le passager a commencé à attendre, et c'est cette
attente-là qu'une réclamation met en cause.

⚠️ **Émettez vos positions pendant l'approche**, sous `mission_id` — voir
« Émettre sa position ». Sans elles, `approach_source` vaut `planned` : la
durée reste juste (deux horodatages suffisent), mais **le tracé qui vous
défendrait n'existe pas**.

⚠️ **Conséquence à connaître :** `actual_distance_m`, la distance de la
course, **ne compte plus vos kilomètres d'approche** — elle commence là où
le passager monte. Une course de 8 km après 6 km d'approche affichait 14 km
avant la v4.24.0.

### 🔔 KLAXONNER — « je suis là » (v4.24.0)

Vous êtes sur place, vous ne voyez personne.

```
POST /rides/{id}/honk
→ 200 { …la course…, "honked_at", "honk_count" }
→ 409 not_arrived     — vous n'avez pas encore envoyé `arrived`
→ 409 honk_too_soon   — trop tôt (meta.cooldown_s)
```

Le passager reçoit une notification courte et sonore qui dit **qui** l'attend
et **dans quoi** — c'est ce qu'il cherche des yeux en sortant. La course ne
change pas d'état : elle reste `arrived`, et vous pouvez klaxonner plusieurs
fois.

> ⚠️ **GRISEZ LE BOUTON PENDANT 60 SECONDES APRÈS CHAQUE COUP**, avec un
> compte à rebours visible.
>
> Un bouton qui sonne fort et qu'on peut presser dix fois devient du
> harcèlement en dix secondes — et c'est le passager, celui qui descend les
> escaliers avec ses sacs, qui le prend. Le service refuse de toute façon un
> second klaxon trop rapproché (`409 honk_too_soon`), mais **ne comptez pas
> là-dessus pour l'affichage** : un bouton qui répond « non » est un bouton
> cassé aux yeux de celui qui appuie.
>
> Le décompte repart de `honked_at`, pas d'un minuteur local : l'application
> redémarrée ne doit pas offrir un klaxon de plus.

⚠️ **Klaxonner n'est pas « dépêchez-vous ».** C'est pour cela que la route
refuse avant `arrived` : personne ne descend attendre sous le soleil une
voiture qui est encore à dix minutes.

`honk_count` est visible de l'exploitation : un passager qui se plaint
d'avoir été harcelé doit pouvoir être cru — ou non.

### Le temps réel — les minutes roulées au-delà du prévu (v4.14.0)

Quand le mode le règle (`bill_actual_time` sur la course), la durée
réellement roulée — de `in_transit` à `completed`, la plateforme la mesure —
compte : chaque minute entamée au-delà de la durée prévue du devis
(`duration_s`) plus `time_tolerance_min` coûte `per_min_xof`. À l'arrivée,
la course porte `actual_duration_s`, `extra_minutes`, `time_fee_xof`,
déjà compris dans `fare_xof` et `driver_xof` (commission habituelle), et
une ligne `fare_adjustments` (`reason: "duration"`). **Affichez le temps
écoulé et, passé la tolérance, le supplément qui monte** : le passager
voit la même chose. En espèces, c'est ce nouveau prix que vous encaissez.

Les termes (`waiting_free_min`, `waiting_per_min_xof`, `bill_actual_time`,
`time_tolerance_min`, `per_min_xof`) sont sur la course dès l'acceptation,
figés au devis du passager — ni la grille du jour ni un autre mode ne
s'appliquent en route.

> **v4.0.0 — le vocabulaire commun.** `approach` est devenu **`picking_up`**
> (« je roule vers le passager »), `onboard` est devenu **`in_transit`**
> (« il est à bord ») : ce sont les mots d'une course de livraison aussi, et
> le `PATCH` ne prend plus les anciens (`422`).

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

> ⚠️ **Une course `in_transit` ne s'annule plus.** Le passager est dans la
> voiture ; l'interrompre demanderait de décider où on le dépose. Le bouton
> doit disparaître à ce statut, pas échouer.

`stop_index` avance à chaque arrêt atteint. Les arrêts sont **dans l'ordre
choisi par le passager** : rien n'est réordonné, contrairement aux collectes
d'une livraison.

**Ce que vous voyez et que le passager ne voit pas** : `commission_xof` et
`driver_xof`. C'est votre part, et elle n'est servie qu'à vous.

### 🎁 Une course en PROMOTION (v4.16.0)

Le passager a parfois payé moins que le prix normal. La course porte alors
`promo_title` et `promo_discount_xof`, et **`fare_xof` est déjà le prix
remisé** — c'est ce montant que vous encaissez en espèces, pas le prix plein.

> ⚠️ **Par défaut, la remise ne vous coûte RIEN.** Elle sort de la commission
> de la plateforme : `driver_xof` reste ce que la course vaut. Une promotion
> est une dépense de croissance de la plateforme, pas une baisse de votre
> revenu décidée sans vous.
>
> Il existe des opérations où les chauffeurs sont partie prenante, et où les
> deux parts baissent ensemble. Dans ce cas c'est **`driver_xof` qui le dit** —
> il est plus bas. Affichez toujours `driver_xof` tel qu'il est servi : c'est
> la seule ligne qui compte pour vous, promotion ou non.

`fare_xof = commission_xof + driver_xof` reste vrai dans tous les cas. Si la
somme ne retombe pas chez vous, c'est un bogue — signalez-le.

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
   `accepted`, `picking_up`, `arrived` ou `in_transit` qui est la vôtre — c'est ce qui
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

## 4 ter. 🚕 LES AUTRES MODES DE COURSE (v4.31.0)

**Deux modes en plus du mode ordinaire. Le PAYS les ouvre, et chaque VOITURE
dit lesquels elle sert — les deux conditions, pas l'une ou l'autre.**

```
GET /settings/modes → { free:   { enabled, scannable, max_hours },
                        rental: { enabled, max_radius_km, alert_km_before } }

GET /classes        → items[].modes = {
       "free":   { base_xof, per_km_xof, per_min_xof, min_fare_xof },
       "rental": { tiers: [ { hours, price_xof, included_km } ], extra_per_km_xof } }
```

⚠️ **LISEZ-LES AVANT DE MONTRER LE BOUTON.** Un mode éteint doit **disparaître
de l'écran**, pas y rester et répondre `409`. Un chauffeur qui appuie sur un
bouton qui échoue croit à une panne.

⚠️ **LE BOUTON DU COMPTEUR DÉPEND DE LA VOITURE QUE VOUS CONDUISEZ CE JOUR-LÀ.**
Cherchez la classe de votre véhicule dans `GET /classes` : sans `modes.free`,
cette voiture ne fait pas le compteur, même si le pays l'ouvre — une
exploitation peut vouloir le compteur en eco et pas en van. Changez de
véhicule, le bouton change.

⚠️ **CE QUE LE COMPTEUR FACTURE EST ÉCRIT LÀ AUSSI** (`base_xof`, `per_km_xof`,
`per_min_xof`, `min_fare_xof`, v4.31.0 — c'était dans les réglages du pays, le
même tarif pour toutes les voitures). Montrez-le au passager qui le demande :
un compteur dont on ne peut pas lire le tarif n'inspire rien.

---

### 🧾 La course LIBRE — votre compteur

Un passager monte dans la rue, sans avoir rien commandé. Vous lancez un
compteur ; la facture s'affiche à la fin.

```
POST /rides/free  { "vehicle_id": "…" }   → la course, avec son free_code
PATCH /rides/{id}/status { "status": "completed" }   → la facture
```

La course naît directement **`in_transit`** : il n'y a personne à aller
chercher. Elle est **en espèces** — le passager vous paie à la descente.

⚠️ **POUSSEZ VOS POSITIONS SOUS `mission_id` = l'identifiant de cette course,
dès le démarrage.** C'est ce tampon qui donne la distance réelle, **donc le
prix**. Sans lui, la course est facturée **à la durée seule** — c'est légal,
mais moins juste pour vous comme pour le passager.

⚠️ **MONTREZ LE `free_code` EN GRAND.** C'est ce que le passager scanne pour
suivre la course et recevoir sa facture. Un QR **et** les 6 caractères en
clair : la caméra refuse souvent, et le code est fait pour être lu à voix
haute (pas de O/0, I/1, S/5).

| Refus au démarrage | Pourquoi |
|---|---|
| `409 free_rides_off` | ce pays ne propose pas le mode — le bouton n'aurait pas dû s'afficher |
| `403 driver_not_active` | hors ligne ou suspendu — **mettez-vous en ligne d'abord** |
| `409 vehicle_not_usable` | ce véhicule est immobilisé par l'exploitation |
| `409 ride_in_progress` | vous avez déjà une course — terminez-la |
| `402 debt_limit_reached` | dette au-dessus du plafond — voir §6 |

⚠️ **Les mêmes barrières qu'une course appelée, et c'est voulu** : sans elles,
ce bouton serait une façon de contourner toutes les règles.

⚠️ **UN COMPTEUR OUBLIÉ EST FERMÉ D'OFFICE** au-delà de `free.max_hours`
(réglages du pays — la même borne pour toutes les voitures). La
course est **terminée et facturée** — jamais annulée, elle a bien eu lieu — et
elle revient avec `auto_closed: true`. Dites-le clairement : « la plateforme a
fermé ce compteur après 6 h ». Un compteur qui tourne la nuit facture la nuit,
et c'est le passager du lendemain qui découvrirait le problème.

---

### ⏳ La LOCATION — un passager vous retient pour des heures

```json
{ "type": "call", "call_id": "…", "ref": "<ride_id>",
  "meta": { "mode": "rental", "rental_hours": 5, "rental_included_km": 80,
            "rental_max_radius_km": 60, "no_destination": true,
            "fare_xof": 20000 } }
```

⚠️ **UN ÉCRAN D'APPEL DISTINCT, ET CE N'EST PAS UNE COQUETTERIE.** L'appel
arrive dans la même trame qu'une course ordinaire, avec les mêmes **cinq
secondes** pour décider — mais ce n'est pas du tout le même engagement : vous
bloquez **des heures**, sans destination, et vous ne prendrez rien d'autre
pendant ce temps. Accepter une location en croyant prendre une course de
quinze minutes se répare en annulant, ce qui pénalise tout le monde.

**Ce que l'écran doit dire, en un coup d'œil :**

| À montrer | Pourquoi |
|---|---|
| **« LOCATION · 5 h »**, en grand | c'est la seule information qui change la décision |
| Le forfait (`fare_xof`) | ce que vous gagnez pour ces heures — il dépend de **votre voiture** : un van ne se loue pas au prix d'une eco, et la commission non plus n'est pas la même (v4.31.0) |
| **« sans destination »** (`no_destination`) | ne dessinez pas un point d'arrivée vide |
| Les km compris et le rayon | ce que vous vous engagez à ne pas dépasser |

Une couleur ou un liseré différents, et le mot **LOCATION** écrit : pas
seulement un badge discret dans un coin.

#### Pendant la location

```
PUT /rides/{id}/rental-stops  { "stops": [ { "label": "…", "geo": [lng, lat] }, … ] }
```

⚠️ **VOUS POUVEZ NOTER LES DESTINATIONS VOUS-MÊME.** Le passager vous les dit
souvent de vive voix, et il faut pouvoir les inscrire : c'est ce qui trace le
parcours et ce qui vérifie le rayon. Le passager peut les envoyer aussi, de
son côté — **relisez la course** quand une trame `status` arrive.

**Vous pouvez aussi rouler sans destination du tout.** C'est normal pour ce
mode : ne bloquez pas l'écran sur « ajoutez une arrivée ».

⚠️ **`422 rental_out_of_range`** : l'arrêt sort du rayon. `meta` porte
`distance_km` et `max_km` — dites les deux au passager, il choisit encore.
Refuser sans chiffres vous met en position de ne rien pouvoir expliquer.

**Prévenez AVANT le plafond.** Vous avez votre position et le départ :
alertez à `rental_alert_km_before` km du rayon. Prévenir laisse le temps de
faire demi-tour ; découvrir un refus au franchissement laisse un passager en
route à qui l'on dit non trop tard.

#### La durée, et la fin

⚠️ **`rental_ends_at` COURT À LA MONTÉE À BORD**, pas à l'acceptation : le
temps que vous mettez à venir n'est pas du temps loué. C'est un **instant**, à
décompter localement.

Terminez comme une course ordinaire. ⚠️ **Le retard n'est pas facturé** — la
durée est ce qui a été vendu. Les **kilomètres** au-delà des compris, eux, le
sont (`rental_overage_km`, motif `rental_overage`) : c'est votre carburant et
votre usure.

---

## 4 quater. 📴 HORS LIGNE — conduire sans réseau (v4.32.0)

**Une course ne s'arrête pas parce que le réseau s'arrête.** Vous conduisez, le
passager descend, il vous paie en espèces : tout cela a lieu. Ce qui manque,
c'est le **récit** — et cette section dit comment le garder, puis le rendre.

⚠️ **RIEN NE DOIT ÊTRE PERDU EN SILENCE.** Un geste qui disparaît parce que le
réseau a coupé est pire qu'un refus affiché : le chauffeur croit avoir fait le
travail, et l'apprend deux jours plus tard par une paie qui ne tombe pas.

### Ce qui marche sans réseau, et ce qui ne marche pas

| Vous pouvez | Vous ne pouvez pas |
|---|---|
| **Lancer et arrêter un compteur** (course libre) | Recevoir un appel |
| Avancer une course déjà acceptée (`arrived`, `in_transit`, `completed`) | Accepter une course |
| Noter les arrêts d'une location | Être payé par portefeuille |
| **Annoncer un prix** (estimé avec la grille en cache) | Facturer — c'est le serveur qui facture |

### 1. La grille en poche — `GET /tariffs/snapshot`

```
GET /tariffs/snapshot            → { country, currency, version, issued_at,
                                     rounding_xof, classes[], surge[],
                                     modes{…}, tracking{…} }
```

**Téléchargez-le à la connexion, gardez-le sur le disque, et ne le jetez
jamais** — même périmé, il vaut mieux que rien.

⚠️ **UN SEUL DOCUMENT, ET C'EST LE POINT.** Vous ne choisissez pas le moment où
le réseau tombe : ce que vous avez en poche doit être **complet et cohérent**.
Trois documents rafraîchis à trois instants différents se contredisent au pire
moment — une grille de janvier avec une politique de mars.

**Revalidez avec `ETag`**, ne retéléchargez pas :

```
GET /tariffs/snapshot
If-None-Match: "04176fcc6442bae8"     → 304 si rien n'a changé
```

`version` empreinte le **contenu** : deux lectures d'une facturation inchangée
rendent la même. ⚠️ Une application qui retélécharge sans raison à chaque
retour de réseau consomme le forfait du chauffeur pour répéter ce qu'elle a
déjà — et finit par ne plus télécharger du tout.

**La version voyage aussi dans le `meta` de chaque appel** (`tariff_version`) :
comparez-la à celle de votre cache et rafraîchissez si elle diffère. ⚠️ C'est
le **seul moment où l'on est sûr que vous écoutez** — sans cela, vous pouvez
rouler une journée entière avec la grille de la semaine dernière.

⚠️ **CE QUE VOUS CALCULEZ EST UNE ESTIMATION, JAMAIS UNE FACTURE.** Dites-le à
l'écran (« montant estimé »). Le prix qui compte est celui que le serveur
recalcule à la resynchronisation : la grille a pu changer, la distance réelle
différer de ce que le GPS a mesuré. **N'imprimez pas de reçu définitif hors
ligne.**

### 2. La file locale — ce qu'on garde, et comment

**Une file persistante sur le disque**, vidée dans l'ordre, qui survit à la
fermeture de l'application et au redémarrage du téléphone. Chaque élément
porte :

| Champ | Pourquoi |
|---|---|
| `client_ref` | ⚠️ **La clé d'idempotence**, tirée au sort par VOUS (UUID). Sans elle, chaque tentative crée une course de plus : le chauffeur en voit trois, et la commission est prise trois fois. |
| `kind` | `free_ride` · `status` · `stops` |
| `at` | L'instant du geste, **à la seconde où il a eu lieu** — pas celui de l'envoi |
| le corps | ce que l'appel aurait porté en ligne |

⚠️ **LA CLÉ SE TIRE AU MOMENT DU GESTE, PAS AU MOMENT DE L'ENVOI.** Une clé
tirée à l'envoi change à chaque nouvelle tentative, et l'idempotence ne sert
plus à rien — c'est l'erreur qui fait les courses en triple.

### 3. Rendre le récit — `POST /rides/sync`

```
POST /rides/sync
{ "items": [
    { "client_ref": "3f2a…", "kind": "free_ride", "at": "…",
      "vehicle_id": "…", "started_at": "…", "ended_at": "…",
      "start": { "geo": [1.2231, 6.1375] }, "end": { "geo": [1.2456, 6.1301] },
      "distance_m": 4200, "quoted_xof": 2300 },
    { "client_ref": "9b41…", "kind": "status", "at": "…",
      "ride_id": "…", "status": "completed" } ] }

→ 200 { "server_time": "…",
        "results": [ { "client_ref": "3f2a…", "outcome": "applied",
                       "ride_id": "6ab…", "fare_xof": 2500 },
                     { "client_ref": "9b41…", "outcome": "rejected",
                       "code": "ride_not_found", "message": "…",
                       "retryable": false } ] } 
```

**Cinquante éléments au plus par envoi.** Envoyez-les par paquets ; ne tentez
pas de vider une journée en une requête.

| `outcome` | Ce que vous faites |
|---|---|
| `applied` | **Retirez** l'élément de la file. Affichez le `fare_xof` **du serveur**. |
| `duplicate` | **Retirez-le aussi** — il était déjà passé. `ride_id` est le même. |
| `rejected` + `retryable: true` | **Gardez-le**, réessayez plus tard |
| `rejected` + `retryable: false` | **Retirez-le** et **dites-le au chauffeur** : ce refus sera le même dans une heure |

⚠️ **LA RÉPONSE EST `200` MÊME QUAND DES ÉLÉMENTS SONT REFUSÉS.** Ne traitez
pas le lot en bloc : un seul élément fautif ferait rejouer le lot entier, le
même élément le ferait échouer à chaque fois, et **rien ne passerait plus
jamais**. Lisez `results`, élément par élément.

⚠️ **UN REFUS DÉFINITIF SE MONTRE.** Retirer en silence un travail réel est
exactement ce qu'il ne faut pas faire : le chauffeur a roulé, il doit savoir
que cette course n'a pas été enregistrée et pourquoi.

#### Réessayer : le rythme

**Exponentiel avec du hasard**, et un plafond :

```
1 s · 2 s · 4 s · 8 s · 16 s · 32 s · 60 s · 60 s · 60 s…   (± 30 % de hasard)
```

⚠️ **LE HASARD N'EST PAS UN DÉTAIL.** Quand une antenne revient, tous les
téléphones du quartier réessaient **à la même seconde** : sans dispersion, ils
font tomber le service qu'ils attendaient. C'est le seul rôle du ±30 %.

**Réessayez tout de suite** — sans attendre le prochain palier — quand le
système signale que le réseau est revenu. Et **ne réessayez jamais en boucle
serrée** : une file vide n'a rien à envoyer, une file pleine attend son palier.

### 4. Les positions — le parcours qu'on rattrape

**Gardez vos positions hors ligne** (avec leur `ts`), puis poussez-les sur le
socket comme d'habitude, en les marquant :

```json
{ "vehicle_id": "…", "mission_id": "<ride_id>", "lng": 1.2231, "lat": 6.1375,
  "ts": 1790000000000, "backfill": true }
```

⚠️ **`backfill: true` ET SON `ts` D'ORIGINE, TOUJOURS.** Un rattrapage sans
horodatage est **refusé** : daté de maintenant, il prétendrait dire où vous
êtes. Une position plus ancienne ne remplace jamais la position courante — le
serveur s'en garde — mais c'est elle qui **complète le parcours, donc la
distance, donc le prix**.

⚠️ **L'ORDRE D'ENVOI EST LIBRE** : chaque point porte son temps, et le serveur
les remet en ordre. En revanche, **poussez-les après la resynchronisation**
pour une course libre faite hors ligne : le `mission_id` est l'identifiant que
`POST /rides/sync` vient de vous rendre — il n'existait pas avant.

### 5. La cadence — pousser moins souvent, jamais s'arrêter

`snapshot.tracking` donne, **par mode**, l'intervalle et la distance minimale :

| Mode | Intervalle | Distance minimale |
|---|---|---|
| `normal` | 5 s | 20 m |
| `free` | 20 s | 50 m |
| `rental` | 30 s | 80 m |

⚠️ **UNE LOCATION DE CINQ HEURES N'A PERSONNE DEVANT L'ÉCRAN.** Pousser toutes
les quatre secondes pendant cinq heures vide une batterie et un forfait pour
rien — sur une course qui se facture à la durée, pas aux kilomètres.

⚠️ **MAIS ESPACER N'EST PAS ÉTEINDRE.** C'est le parcours qui mesure le
dépassement de kilomètres d'une location et la distance d'un compteur : une
course qu'on ne suit plus est une course qu'on ne sait plus facturer — ni
défendre quand elle est contestée.

### 6. La course LIBRE entièrement hors ligne

1. Le bouton s'affiche si **votre cache** dit que le pays ouvre le mode
   (`modes.free.enabled`) **et** que la classe de votre véhicule vend le mode.
2. **Relevez votre position** au démarrage, et **à l'arrêt** : ce sont les deux
   points de la facture. ⚠️ Ne cherchez pas l'adresse — **le serveur la
   nommera** par géocodage inverse. Un géocodeur demande du réseau, celui-là
   même qui vous manque.
3. Comptez la distance avec le GPS, annoncez le montant **estimé**.
4. À la reconnexion : `POST /rides/sync` (`kind: "free_ride"`), puis poussez
   les positions sous le `ride_id` rendu.

⚠️ **LA DISTANCE QUE VOUS ENVOYEZ EST BORNÉE** à ce qu'on peut parcourir dans
le temps annoncé (120 km/h de moyenne). Un GPS qui décroche sous un pont fait
bondir la position de plusieurs kilomètres, et ils seraient facturés au
passager. Filtrez aussi de votre côté : un saut de plus de 200 m en une seconde
n'est pas un déplacement.

⚠️ **VOTRE HORLOGE NE FAIT PAS AUTORITÉ.** Le serveur recadre chaque instant
entre l'étape précédente et maintenant. La réponse porte `server_time` :
**mesurez votre dérive** et corrigez vos prochains horodatages, plutôt que de
les laisser recadrer en silence.

### 7. Ce que le chauffeur DOIT voir

| Situation | À l'écran |
|---|---|
| Hors ligne | Un bandeau permanent, pas une icône discrète : « **hors ligne — vos courses sont enregistrées** » |
| File non vide | « **3 courses en attente d'envoi** », avec la plus ancienne |
| Envoi en cours | Un état visible, et jamais bloquant : on continue de conduire |
| Élément refusé définitivement | Un message qui **nomme** la course et le motif |
| Prix estimé | Le mot « **estimé** » à côté du montant, tant que le serveur ne l'a pas confirmé |

⚠️ **NE MONTREZ JAMAIS UNE FILE VIDE COMME UNE RÉUSSITE TANT QU'ELLE N'EST PAS
PARTIE.** « Tout est synchronisé » alors que trois courses attendent est le
message qui fait perdre confiance dans l'application entière.

### 8. Pendant ce temps, côté exploitation

Au bout de **deux minutes** sans position pendant une course, la console
affiche « course hors radar » avec votre nom et votre numéro. ⚠️ **Elle
n'annule rien** — la course continue, et votre téléphone la reprendra. C'est
une alerte, pas une sanction : un tunnel et un accident commencent de la même
façon, et l'exploitation préfère appeler pour rien.

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
| `POST /auth/login` · `/auth/refresh` · `/auth/logout` | la session — ⚠️ la connexion porte `app: "driver"` depuis la v4.26.0 |
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

> ⚠️ **`payment_method: "subscription"` (v4.23.0) — N'ENCAISSEZ RIEN.** Le
> passager est abonné : la course a été payée d'avance, en même temps que
> tout son mois. Votre part vous est créditée comme pour une course
> `wallet`. Traitez ce moyen **exactement comme** `wallet` ou `online` à
> l'écran de fin : `fare_xof` s'affiche, mais rien n'est à prendre. Un
> chauffeur qui réclame l'argent d'une course déjà payée, c'est une
> réclamation à tous les coups.
