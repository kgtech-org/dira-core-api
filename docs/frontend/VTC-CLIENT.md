# App CLIENT — COURSES (VTC) — contrat d'API

> **Version 4.30.0** · 26 septembre 2026
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
l'image du marqueur sur une carte à plat et `nav_icon_url` la même sur une
carte de navigation (voir ci-dessous) ; à
défaut (`null`), `map_icon` nomme une silhouette de repli (`voiture` ·
`berline` · `suv` · `van` · `moto` · `tricycle` · `velo` · `pieton`) —
sans image ni silhouette connue, dessinez `voiture`. Mettez les images en
cache par URL : elles changent d'URL quand elles changent. La `key`
reste l'identifiant technique (devis, course) — jamais un libellé.

> ### 🧭 ⚠️ LES DEUX PINS D'UN VÉHICULE, ET LEUR ORIENTATION (v4.20.0)
>
> **Un véhicule a DEUX images de marqueur, pas une.** Elles viennent de le mode du chauffeur — `GET /classes` :
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
  "promo_title": "Offre de lancement", "promo_discount_xof": 250,
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

### 🎁 Les PROMOTIONS (v4.16.0)

> ⚠️ **`fare_xof` EST DÉJÀ REMISÉ.** C'est ce que le passager paie, un point
> c'est tout. Ne soustrayez **rien** : `promo_discount_xof` est là pour
> **afficher** la remise, pas pour la calculer. Servir un prix plein à
> soustraire ensuite aurait obligé chaque écran à refaire l'opération, et l'un
> d'eux l'aurait oublié — sur le total, ou sur le reçu.

`promo_title` et `promo_discount_xof` ne sont **servis que si une promotion
s'applique**, exactement comme la majoration. Affichez alors le prix barré et
le titre de l'offre :

```
  2 500 F  2 250 F   ·  Offre de lancement
  ───────
```

Le prix plein se reconstitue par `fare_xof + promo_discount_xof` — si vous
tenez à le barrer. Ne l'affichez **pas** quand `promo_discount_xof` est
absent : un « −0 F » se lit comme une remise nulle, pas comme une absence de
remise.

**La promotion est FIGÉE avec le devis.** Une offre retirée entre le devis et
la confirmation ne change pas le prix promis, et `promo_title` reste sur la
course. C'est la même règle que pour la majoration : ce qui a été affiché est
ce qui est payé.

**Les offres en cours**, pour une bannière ou un écran « promotions » :

```
GET /promotions
→ { "items": [ { "id": "6ab…", "scope": "class", "class_keys": ["eco"],
                 "title": "−20 % sur Éco", "description": "…",
                 "kind": "percent", "value": 20,
                 "starts_at": "…", "ends_at": "…", "live": true } ] }
```

Une seule promotion s'applique à une course — **la plus avantageuse**. Il n'y
a pas de cumul : deux offres lancées par deux personnes différentes
offriraient la course sans que ni l'une ni l'autre ne l'ait voulu.

> ⚠️ **`GET /promotions` ANNONCE, LE DEVIS DÉCIDE (v4.17.0).** Une offre
> listée là peut très bien ne pas s'appliquer à **votre** course, et ce n'est
> pas un bogue :
>
> - chaque opération a une **enveloppe**, et quand elle est consommée l'offre
>   s'arrête — parfois dans la journée ;
> - une offre peut être limitée à **N fois par personne**, et vous l'avez
>   peut-être déjà utilisée ;
> - une remise qui ne tient plus dans ce qui reste du budget est **refusée**,
>   jamais rabotée — les petites courses en profitent donc encore quand les
>   longues n'y ont plus droit.
>
> **N'affichez donc jamais un prix calculé à partir de `GET /promotions`.**
> Le seul prix vrai est `fare_xof` du devis. Servez-vous de cette liste pour
> une bannière — « des offres en ce moment » — et laissez le devis dire
> combien.

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
center: [lng, lat], radius_km, tolerance_km, active, country } ] }` — pour
dire « nous ne desservons pas encore ici » dès qu'un passager pose un point,
avant même le devis : un point est acceptable s'il est à moins de `radius_km
+ tolerance_km` du `center` d'une ville active, et les deux points doivent
tomber dans la **même** ville.

---

### 🧳 LA COURSE DU VOYAGEUR — elle se fait là où elle se fait (v4.27.0)

**Un passager commande dans le pays où il EST, pas dans celui où il s'est
inscrit.** Un Togolais de passage à Dakar pose son départ au Plateau et
obtient un prix, comme chez lui.

⚠️ **Ce qui se passait avant.** Le pays du COMPTE décidait de tout. Un
compte togolais à Dakar recevait `out_of_service_area` — « ce lieu est à
2 249 km de Lomé » — et l'application, qui ne recevait que les villes du
Togo, refusait même le point **avant d'appeler le serveur** : écran bloqué,
aucune raison lisible, alors que Dakar est desservie depuis des mois.

**Ce qui suit le passager** — tout ce qui fait la course : villes
desservies, tarifs et modes, majorations, promotions de ville, réglages
d'appel, chauffeurs appelés, facturation. Le pays vient du **point de
départ** et il est **figé sur le devis**, comme le prix ; la confirmation ne
le relit pas.

**Ce qui reste chez lui** : son compte, son historique, son portefeuille.

```
POST /rides/quote → items[].country = "SN"     ⚠️ v4.27.0
GET  /rides/{id}  → country = "SN"
```

⚠️ **`country` DIT LA MONNAIE DU PRIX.** Un montant de la plateforme est un
entier dans la monnaie de son pays : `fare_xof` vaut des francs CFA à Dakar,
et des francs guinéens à Conakry. Formatez avec la monnaie de **`country`**
(lue dans `GET /countries`), **jamais** avec celle du compte — sinon vous
écrivez le bon nombre derrière le mauvais symbole.

**`GET /cities` rend désormais TOUTES les villes, tous pays confondus**,
chacune avec son `country`. Votre contrôle local marche donc tel quel : la
ville qui sert le point est trouvée, où qu'elle soit.

#### ⚠️ Le catalogue des modes doit suivre le lieu, lui aussi

```
GET /classes?near=-17.4370,14.6690      ⚠️ v4.27.0
```

**Envoyez `near` (le `lng,lat` du départ) dès que le passager a posé un
point.** Sans lui, le catalogue reste celui du pays du COMPTE — et vous
afficheriez les modes du Togo (leurs noms, leurs pictogrammes, leurs frais
d'attente) avec des prix sénégalais collés dessus. Un mode servi à Dakar
mais absent du Togo n'aurait même aucune ligne où s'afficher.

Un `near` illisible est **ignoré, pas refusé** : le catalogue reste public
et lisible.

⚠️ **Re-demandez le catalogue quand le départ change de pays.** Les frais
d'attente et de temps réel affichés sous chaque mode viennent de ce
catalogue : laissés sur ceux de chez soi, ils annonceraient un tarif que la
course n'appliquera pas.

#### 💳 Le portefeuille, lui, ne traverse pas une monnaie

```
POST /rides   { quote_id, payment_method: "wallet" }
422 { "error": { "code": "wallet_other_currency", "reason": "wallet_currency",
                 "message": "Votre portefeuille est dans une autre monnaie que ce pays." } }
```

Le solde est un entier dans la monnaie de SON pays. Débiter 45 000 d'un
solde en francs CFA pour une course facturée 45 000 francs guinéens
prendrait **treize fois** le prix. Le refus ne vise donc **que ce
moyen-là** : proposez les espèces ou le paiement en ligne, et n'annulez
pas la course.

Entre deux pays de **même monnaie** — Togo et Sénégal, Gabon et Tchad — le
portefeuille marche comme chez soi : c'est le même franc.

⚠️ **Le refus arrive à la CONFIRMATION, pas au devis** : le devis ne sait
pas encore comment on paiera. Si votre écran présélectionne le
portefeuille, prévoyez le repli.

#### Là où nous ne sommes pas du tout

Le pays du compte reste, et le refus nomme une ville de chez lui. C'est
voulu : nous n'y opérons pas, et nommer une ville lointaine serait moins
honnête que nommer la sienne.

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

> `payment_method: "subscription"` (v4.23.0) se **lit** sur une course née
> d'un abonnement — il ne se **commande** pas ici. Cette course est déjà
> payée : pas d'écran de paiement à la fin — section **4 ter**.

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

### ⚠️ « Pas demain » et « plutôt à 8 h » — UNE occurrence (v4.23.0)

```
POST /scheduled-rides/{id}/skip-next          → la prochaine, et seulement elle, est sautée
POST /scheduled-rides/{id}/postpone { "minutes": 45 }
```

Mettre en pause arrête la série ; annuler y met fin. **Ni l'un ni l'autre ne
répond à « pas demain matin »** — un rendez-vous, un jour férié, un enfant
malade —, et c'est le geste que fait le plus souvent quelqu'un qui roule tous
les jours. Ces deux routes sont **le bouton du rappel** : quand la
notification `ride_scheduled_soon` arrive, le passager doit pouvoir sauter ou
reporter d'un geste, sans ouvrir la liste des programmations.

- `skip-next` : la série continue. Une programmation **ponctuelle** n'a rien
  à sauter — c'est une annulation, et la réponse le dit (`409 not_recurring`).
- `postpone` : 1 à 720 minutes (12 h). ⚠️ **Seulement la prochaine** : le
  motif ne bouge pas. « Aujourd'hui à 8 h au lieu de 7 h » ne veut pas dire
  « désormais à 8 h ». Les rappels repartent à zéro — le passager sera
  prévenu à la **nouvelle** heure. Un report qui rattraperait l'occurrence
  suivante est refusé (`409 postpone_overlaps`) : deux départs le même matin,
  c'est deux chauffeurs.
- Les deux rendent la programmation à jour : relisez `at` et `alert_at`.

---

## 4 ter. 🎟️ L'ABONNEMENT — le trajet de tous les jours (v4.23.0)

Un abonnement, c'est **les mêmes trajets, tous les jours, payés d'avance et
moins cher**. Aller au bureau le matin, rentrer le soir, déposer les enfants
à l'école en chemin. Le passager décrit son rythme une fois ; la plateforme
appelle le chauffeur toute seule, chaque jour, aux heures dites.

Trois moments, et l'application les suit dans cet ordre : **estimer**,
**payer**, **être prévenu**.

### 1. Estimer — ce que ça coûte, avant de s'engager

```
POST /subscriptions/estimate
{ "tz": "Africa/Dakar",  "starts_on": "2026-10-05T00:00:00Z",   # demain par défaut
  "legs": [
    { "label": "Aller bureau",
      "stops": [ { "kind": "pickup", "label": "Maison",  "geo": [lng, lat] },
                 { "kind": "stop",   "label": "École",   "geo": [lng, lat] },
                 { "kind": "dest",   "label": "Bureau",  "geo": [lng, lat] } ],
      "class_key": "eco", "days": [1,2,3,4,5], "time": "07:00" },
    { "label": "Retour",
      "stops": [ … ], "class_key": "eco", "days": [1,2,3,4,5], "time": "18:00" } ] }

→ 200 { "legs": [ { "label": "Aller bureau", "distance_m", "duration_s",
                    "unit_xof": 2500, "surge_name", "surge_multiplier",
                    "occurrences": 5, "normal_xof": 12500 }, … ],
        "weekly":  { "period": "weekly",  "from", "to", "occurrences": 10,
                     "normal_xof": 25000, "discount_pct": 10,
                     "price_xof": 22500, "saving_xof": 2500 },
        "monthly": { "period": "monthly", "occurrences": 46, "normal_xof": 115000,
                     "discount_pct": 20, "price_xof": 92000, "saving_xof": 23000 } }
```

- Un **trajet** (`leg`) est un aller OU un retour, avec ses propres jours et
  son heure. Un `leg` peut avoir **jusqu'à 5 arrêts** : l'école puis le
  bureau, c'est **un seul trajet et une seule course**. Six trajets au plus.
- `days` : jours ISO, 1 = lundi … 7 = dimanche. `time` : `"HH:MM"`, l'heure
  de **récupération**, **locale au fuseau du passager** — « 7 h » veut dire
  7 h chez lui.
- ⚠️ **Les deux périodes arrivent ENSEMBLE.** Ne demandez pas « la semaine »
  puis « le mois » : le passager choisit en **comparant**, et il faut les
  montrer côte à côte, avec `saving_xof` en évidence. C'est l'économie qui
  décide, pas le prix.
- ⚠️ **Le mois n'est pas « quatre semaines ».** `occurrences` compte les
  jours **réels** entre deux dates : du 5 octobre au 5 novembre, il y a 23
  jours ouvrés — pas 20, pas 21,67. Affichez ce nombre : c'est ce que
  quelqu'un qui travaille cinq jours par semaine vérifie avant de s'engager.
- ⚠️ **La majoration est dans le prix.** `surge_name` / `surge_multiplier`
  (en millièmes, 1400 = ×1,4) sont rendus par trajet, **présents seulement si
  une majoration s'applique**. Un abonnement au départ de l'aéroport coûte ce
  que coûtent des courses depuis l'aéroport — dites-le, sinon le passager
  croira à une erreur.
- `422` : un motif qui ne produit **aucune** course dans la période, ou
  **trop peu** (« *at least 4 rides over the period* ») — en dessous du
  plancher du pays, l'abonnement n'a pas de sens : deux courses par mois se
  commandent à la course.

Les **conditions du pays** — pour annoncer « jusqu'à −20 % le mois » avant
même l'estimation :

```
GET /settings/subscription
→ { "weekly_discount_pct": 10, "monthly_discount_pct": 20,
    "min_occurrences": 4, "alert_leads_min": [30, 1], "pending_hours": 24 }
```

### 2. Souscrire et payer — l'abonnement est payé d'AVANCE

```
POST /subscriptions
{ …le même corps que l'estimation…, "period": "monthly",
  "payment_method": "wallet",     # ou "online" — PAS d'espèces
  "note": "…" }
→ 201 { "id", "status": "pending_payment", "period", "price_xof", "normal_xof",
        "saving_xof", "occurrences", "starts_on", "ends_on",
        "alert_leads_min": [30, 1], "legs": [ … ] }

POST /subscriptions/{id}/pay
→ 200 { "status": "active", "paid_at" }                       # portefeuille
→ 200 { "status": "pending_payment", "payment_url": "https://…" }   # en ligne
```

- ⚠️ **Le prix est RECALCULÉ à la souscription**, il n'est pas repris de
  l'estimation. Une estimation vieille d'une heure n'engage pas la
  plateforme. Montrez le prix rendu par le `201`, pas celui que vous aviez.
  S'il a changé, dites-le.
- ⚠️ **`pending_payment` n'est PAS un abonnement.** Aucune course ne part
  tant que le paiement n'est pas acquis. Passé `pending_hours` (24 h par
  défaut), il **expire tout seul**.
- `payment_url` **n'est pas un encaissement** : ouvrez-la, puis attendez. Le
  statut passe à `active` quand l'opérateur rappelle le socle — relisez
  `GET /subscriptions/{id}`, ou attendez la notification
  `ride_subscription_active`.
- `402 insufficient_funds` : solde insuffisant. Proposez de recharger, puis
  de rappeler `/pay` — l'abonnement est toujours là.
- À l'activation, **chaque trajet devient une programmation récurrente** :
  `legs[].schedule_id` apparaît. C'est elle qui préviendra et appellera.
- Les courses lancées par un abonnement portent
  `payment_method: "subscription"` : ⚠️ **elles ne sont pas re-facturées**.
  N'affichez ni prix à payer ni écran de paiement à la fin — c'est déjà payé.
  Le montant reste visible à titre indicatif.

### 3. Vivre avec — être prévenu, et pouvoir dire non

⚠️ **Le passager est prévenu DEUX FOIS avant chaque départ** :
`alert_leads_min` vaut `[30, 1]` par défaut — **30 minutes pour se préparer,
1 minute pour renoncer**. Un seul rappel loin du départ s'oublie ; un seul
rappel juste avant ne laisse pas le temps de s'habiller.

Les deux rappels sont la notification `ride_scheduled_soon` (data :
`{ "type": "scheduled_ride", "schedule_id", "at" }`), suivie de
`ride_scheduled_started` quand l'appel part — exactement comme une
programmation ordinaire, parce que **c'en est une**.

⚠️ **Un rappel sans bouton pour dire non est une alarme, pas un service.**
Chaque rappel doit offrir, sur place :

| Le passager veut | Vous appelez |
|---|---|
| « pas aujourd'hui » | `POST /scheduled-rides/{schedule_id}/skip-next` |
| « dans 30 minutes » | `POST /scheduled-rides/{schedule_id}/postpone { "minutes": 30 }` |
| « laissez partir » | rien — l'appel part à l'heure |

Le `schedule_id` est celui du **trajet** (`legs[].schedule_id`), pas celui de
l'abonnement : on saute un aller, pas un mois.

Et sur l'abonnement lui-même :

```
GET  /subscriptions              # les miens en cours ; ?all=true pour l'historique
GET  /subscriptions/{id}
POST /subscriptions/{id}/pause · /resume · /cancel
```

- `pause` arrête les départs ; ⚠️ **la période ne s'allonge pas d'autant** —
  un mois payé est un mois de calendrier. Pour une absence d'**un jour**,
  `skip-next` sur le trajet concerné, pas une pause.
- `cancel` : un abonnement encore `pending_payment` est simplement abandonné.
  Un abonnement **actif n'est pas remboursé au prorata** par cette route —
  le remboursement est une décision de l'exploitation, à demander au support.
- Statuts : `pending_payment` · `active` · `paused` · `expired` (la période
  est passée — notification `ride_subscription_ended`) · `cancelled`.
- ⚠️ **Un abonnement ne se renouvelle PAS tout seul.** À `ends_on`, il
  expire. Proposez de reprendre le même — c'est le moment où le passager y
  pense, et le seul.

---

## 4 quater. 🚕 LES AUTRES MODES DE COURSE (v4.29.0)

**Deux modes en plus du mode ordinaire, et chacun s'allume PAR PAYS.**

```
GET /settings/modes → { free:   { enabled, min_fare_xof, scannable, max_hours },
                        rental: { enabled, tiers: [ { hours, price_xof, included_km } ],
                                  extra_per_km_xof, max_radius_km, alert_km_before } }
```

⚠️ **LISEZ CE RÉGLAGE AVANT DE MONTRER QUOI QUE CE SOIT.** Un mode éteint doit
**disparaître de l'écran**, pas y rester et échouer. Et n'écrivez jamais les
durées d'une location en dur : c'est cette liste qui dit ce que le pays vend.

---

### 📷 La course LIBRE — scanner un taxi qu'on a hélé

Un chauffeur peut lancer un compteur sans que personne ne l'ait commandé : un
taxi dans la rue. Vous montez, puis vous **scannez son code** pour suivre la
course et recevoir la facture à la fin.

```
GET  /rides/free/{code}        → la course, avec la carte du chauffeur
POST /rides/free/{code}/join   → elle devient la vôtre
```

**Deux temps, et l'ordre compte.** Montrez d'abord ce que `GET` rend — le nom
du chauffeur, la voiture, la plaque — et ne rattachez qu'après confirmation.
⚠️ Un scan qui rattache d'un coup, sans rien montrer, fait des réclamations :
la personne n'a pas vérifié dans quelle voiture elle monte.

Le code fait **6 caractères** de `ABCDEFGHJKLMNPQRTUVWXYZ2346789` — **sans
O/0, I/1 ni S/5**, parce qu'on le recopie à la main quand la caméra refuse.
Acceptez la saisie manuelle, en majuscules, et validez sur cet alphabet avant
d'appeler.

| Refus | Ce que ça veut dire | Ce que vous affichez |
|---|---|---|
| `404 free_ride_not_found` | code inconnu, course finie, ou **course d'un autre pays** | « ce code n'est plus valable » — proposez de réessayer |
| `409 free_ride_taken` | quelqu'un a scanné avant vous | « cette course est déjà suivie par un autre passager » |
| `409 free_ride_not_scannable` | le pays veut le compteur sans le rattachement | ne montrez pas le scanner du tout |
| `409 free_ride_own` | c'est votre propre course (compte chauffeur) | — |

⚠️ **RATTACHER NE CHANGE PAS LE MOYEN DE PAIEMENT.** La course reste **en
espèces** : un passager monté dans la rue n'a pas de portefeuille Dira.
Vous obtenez la **facture**, pas une autre façon de payer. Ne proposez ni
portefeuille ni mobile money sur une course libre.

⚠️ **LE PRIX N'EXISTE PAS AVANT LA FIN.** `fare_xof` vaut **0** pendant toute
la course : elle est facturée sur ce qui a été réellement roulé, au tarif du
mode du véhicule. **N'affichez pas « 0 F »** — dites « compteur en cours », et
montrez le montant à `completed`.

⚠️ **`auto_closed: true`** : la plateforme a fermé le compteur elle-même,
parce qu'il tournait depuis plus longtemps que la borne du pays. La course est
**terminée et facturée**. Dites-le, sans accuser le chauffeur.

---

### ⏳ La LOCATION — retenir un chauffeur pour une durée

Vous n'achetez pas un trajet, vous achetez **du temps**. Le prix est le
**forfait de la plage**, et il ne bouge pas avec le chemin parcouru.

```
POST /rides/rental/quote  { "class_key": "van", "hours": 5,
                            "pickup": { "label": "…", "geo": [lng, lat] } }
  → { mode: "rental", class_key: "van", fare_xof: 35000, rental_hours: 5,
      rental_included_km: 80, rental_extra_per_km_xof: 150,
      rental_max_radius_km: 60, rental_alert_km_before: 10, expires_at }

POST /rides  { quote_id, payment_method }        # comme un devis ordinaire
```

⚠️ **`class_key` EST OBLIGATOIRE, ET IL SERT DEUX FOIS** : il choisit le
**prix** et il décide **qui sera appelé**. Une journée de van ne se vend pas
au prix d'une journée d'eco — ce n'est ni le même véhicule, ni le même
carburant, ni le même chauffeur qu'on immobilise.

⚠️ **PAS DE DESTINATION À DEMANDER.** Un seul point : le départ. Un écran qui
réclamerait une arrivée empêcherait de commander ce que le passager veut
justement — un chauffeur à disposition.

⚠️ **LA DURÉE DOIT ÊTRE UNE PLAGE VENDUE POUR CE MODE.** « 3 h » entre 1 h et
5 h est refusé (`422`, `fields: ["hours"]`, avec `class_key` dans `meta`) :
interpoler inventerait un prix que personne n'a décidé, et le passager verrait
un montant introuvable dans la grille.

#### ⚠️ Ne proposez que les durées DU MODE choisi

`rental.tiers` porte un `class_key` par ligne, et **une ligne sans `class_key`
vaut pour tous les modes qui n'ont pas la leur**. Pour un mode donné, les
durées à afficher sont donc :

> **les `tiers` de ce `class_key`**, plus **les `tiers` sans `class_key`** dont
> ce mode n'a pas déjà sa propre ligne.

```jsonc
// "van" se loue 1 h, 5 h et 24 h ; "eco" se loue 1 h et 5 h.
"tiers": [
  { "hours": 1,  "price_xof": 5000,   "included_km": 20 },   // tous les modes
  { "hours": 5,  "price_xof": 20000,  "included_km": 80 },   // tous les modes
  { "class_key": "van", "hours": 5,  "price_xof": 35000, "included_km": 80 },
  { "class_key": "van", "hours": 24, "price_xof": 200000, "included_km": 300 }
]
```

⚠️ **LE MODE EXACT L'EMPORTE TOUJOURS** sur la ligne commune : ci-dessus, 5 h
en van coûte 35 000 et non 20 000. Si vous appliquiez la ligne commune en
premier, vous afficheriez un van au prix d'une eco.

⚠️ **MONTRER TOUTE LA GRILLE EST UNE ERREUR.** Un passager qui choisit « 24 h »
sur une eco qui ne se loue pas à la journée se verrait refuser après coup —
c'est pire qu'une durée qu'on ne lui a jamais proposée.

#### Dire les garde-fous AVANT de réserver

**Les trois chiffres du devis sont à montrer sur l'écran de confirmation**,
pas enfouis dans des conditions générales :

| Champ | Ce que vous dites |
|---|---|
| `rental_included_km` | « 80 km compris » |
| `rental_extra_per_km_xof` | « puis 150 F le km » — `0` : le dépassement est offert, ne dites rien |
| `rental_max_radius_km` | « à 60 km de votre départ au plus » |

⚠️ **« CINQ HEURES » NE VEUT PAS DIRE « CINQ HEURES DE ROUTE ».** Personne ne
roule cinq heures d'affilée en ville, et le forfait n'est pas calculé pour ça.
Un passager qui découvre ces bornes à la facture n'a pas acheté ce qu'on lui
avait promis — et c'est la réclamation la plus coûteuse qui existe.

#### Pendant la location

```
PUT /rides/{id}/rental-stops  { "stops": [ { "label": "…", "geo": [lng, lat] }, … ] }
```

**Donnez les destinations au fur et à mesure.** La liste ENTIÈRE des arrêts
après le départ, dans l'ordre ; le départ ne se change pas.

⚠️ **CE N'EST PAS `PATCH /rides/{id}/stops`, et surtout ne l'utilisez pas
ici.** L'autre route **recalcule le prix** et fait bouger l'argent. Sur une
location, le prix ne change **jamais** avec le trajet : c'est la durée qui est
vendue.

⚠️ **LE CHAUFFEUR PEUT AUSSI APPELER CETTE ROUTE** — pour noter ce qu'on lui a
dit de vive voix. Vos arrêts peuvent donc changer sans que vous les ayez
envoyés : **relisez la course** sur une trame `status` comme d'habitude, et
n'écrasez pas la liste sans l'avoir relue.

**`422 rental_out_of_range`** : l'arrêt demandé sort du rayon. `meta` porte
`distance_km` et `max_km` — **affichez les deux** : « Kaolack est à 190 km,
votre location va jusqu'à 60 km ». Un « trop loin » sans chiffres laisse
deviner de combien.

#### Le temps qui reste

⚠️ **`rental_ends_at` COURT À LA MONTÉE À BORD**, pas à la commande : le temps
que le chauffeur met à venir n'est pas du temps loué. Le champ est **absent**
tant que vous n'êtes pas monté — n'affichez pas de compte à rebours avant.

C'est un **instant** : décomptez-le localement, sans réinterroger.

#### À la fin

`rental_overage_km` dit les kilomètres facturés au-delà des compris, et le
supplément apparaît dans `fare_adjustments` avec le motif `rental_overage`.
⚠️ **Le kilomètre commencé est dû** — c'était écrit dans les conditions.
Aucun supplément pour un **retard** : la durée est ce qu'on a vendu.

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
| `picking_up` | il roule vers le point de départ — `eta_at` dit quand il y sera (v4.28.0) |
| `arrived` | il est là et attend (v4.14.0) — `arrived_at`, et le compteur d'attente : `waiting_free_min` minutes offertes, puis `waiting_per_min_xof` la minute |
| `in_transit` | le passager est à bord — `waiting_minutes` / `waiting_fee_xof` disent ce que l'attente a coûté, déjà compris dans `fare_xof` ; `eta_at` décompte le **prochain arrêt** (v4.28.0) |
| `completed` | terminée |
| `cancelled` | `cancelled_by` dit qui, `cancelled_reason` pourquoi |

```
GET /rides/{id}
GET /rides?cursor=…     # l'historique de VOS courses, page par page
```

L'historique rend **les plus récentes d'abord** — par date de création, puis identifiant (v4.12.1) ; `?cursor=` est l'identifiant de la dernière course reçue et rend la page suivante, plus ancienne.

### ⏱️ DANS COMBIEN DE TEMPS — le décompte du PROCHAIN arrêt (v4.28.0)

```jsonc
GET /rides/{id} → {
  "status": "picking_up",
  "eta_stop_index": 0,                    // de quel arrêt on parle
  "eta_at": "2026-09-26T08:41:30Z"        // quand il y sera
}
```

⚠️ **`eta_at` EST UN INSTANT, à décompter localement.** Pas une durée à
réinterroger : une durée vieillit dans le tuyau et dans l'écran — « 4 min »
servi il y a trois minutes en vaut une. Faites tourner votre compteur sur
`eta_at`, et ne rappelez l'API que sur un signal (trame `status`, push, ou
votre rafraîchissement ordinaire).

⚠️ **CE QUI MANQUAIT.** La seule durée servie était `duration_s`, celle du
devis : le trajet entier, du départ à la destination. Vous ne pouviez donc
décompter que l'arrivée FINALE. Pendant l'approche, le passager qui attend
sur le trottoir n'avait **aucun chiffre** ; et sur un trajet à plusieurs
arrêts, le décompte sautait les étapes intermédiaires comme si elles
n'existaient pas.

**`eta_stop_index` dit DE QUEL arrêt il s'agit** — et il change en cours de
route :

| État de la course | `eta_stop_index` | Ce que vous affichez |
|---|---|---|
| `accepted`, `picking_up` | **`0`** — le départ | « il arrive dans 4 min » |
| `arrived` | *absent* | rien : il est là. C'est le compteur d'**attente** qui prend le relais |
| `in_transit` | l'arrêt **suivant** celui atteint (`stop_index + 1`) | « prochain arrêt dans 7 min » |
| dernier arrêt atteint, `completed`, `cancelled`, `searching` | *absent* | rien |

⚠️ **ABSENTS, LES DEUX CHAMPS VEULENT DIRE « ON NE SAIT PAS »** — chauffeur
silencieux, position trop vieille (plus de 2 min), moteur d'itinéraire muet,
course à l'arrêt. **N'affichez alors aucun chiffre, et surtout pas le dernier
connu** : un décompte qui continue de tourner sur une estimation morte
afficherait « 2 min » pendant que le chauffeur est immobile depuis un quart
d'heure. Effacez la ligne, ou dites « position en attente ».

⚠️ **Ne recalculez pas d'estimation vous-même** à partir de la position du
socket et d'une distance à vol d'oiseau. Elle serait systématiquement
optimiste — et le passager la comparerait à la nôtre.

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

#### 🔔 Il klaxonne — `ride_driver_honked` (v4.24.0)

Le chauffeur est sur place et ne vous voit pas : il appuie sur un bouton, et
vous recevez **`ride_driver_honked`** — « [driver] est devant, dans
[vehicle]. Il vous cherche. »

```
data : { "type": "ride_status", "ride_id", "status": "arrived",
         "event": "honk", "honk_count": "1" }
```

⚠️ **Ce n'est pas `ride_driver_arrived` à nouveau.** L'arrivée est une
**information** ; le klaxon est un **appel** : il arrive plus tard, il veut
dire « maintenant », et il doit sonner comme tel. Donnez-lui le son et la
vibration que vous réservez aux choses urgentes — sinon il se noie dans un
fil que le passager ne regarde pas, puisqu'il croyait avoir encore cinq
minutes.

Le passager n'a rien à répondre : le bon écran est celui de la course, avec
le nom et la plaque **en grand**. C'est ce qu'il cherche des yeux en sortant.

Le chauffeur ne peut klaxonner **qu'une fois arrivé**, et son application
grise le bouton une minute après chaque coup. `honk_count` reste sur la
course : un passager qui se plaint d'avoir été harcelé doit pouvoir être cru.

#### 📍 Ce que le chauffeur a roulé pour venir (v4.24.0)

Quand il signale son arrivée, la plateforme fige son **approche** sur la
course : `approach_duration_s` (depuis l'**acceptation** — depuis le moment
où vous avez commencé à attendre), `approach_distance_m`,
`approach_polyline` (son parcours réel) et `approach_source`.

De quoi dire « il a mis 14 min » sur l'écran de fin, et de quoi trancher une
réclamation sur autre chose qu'une impression.

⚠️ **`actual_distance_m` ne compte plus ces kilomètres-là** : la distance de
la course commence là où vous montez. Une course de 8 km après 6 km
d'approche affichait 14 km avant la v4.24.0.

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

#### ⏳ Au bout d'un moment, la plateforme abandonne (v4.25.0)

Quand le pays l'a réglé (`search_expiry_min`, 0 = jamais), une recherche
épuisée depuis trop longtemps est **abandonnée par la plateforme** :

```
status: cancelled · cancelled_by: "system" · cancelled_reason: "search_expired"
```

Le passager est **remboursé** (si la course était payée) et reçoit
`ride_cancelled`. Côté application, c'est une annulation comme une autre —
rien de spécial à coder ; affichez simplement le motif, et proposez de
commander à nouveau.

⚠️ **Le délai se compte depuis l'ÉPUISEMENT, pas depuis la commande.** Chaque
**Relancer** remet le compteur à zéro : une recherche relancée n'est jamais
coupée au milieu de la tentative suivante.

⚠️ **Ce n'est pas la fin de la recherche, c'est la fin de l'attente.** Tant
que le délai n'est pas écoulé, les deux gestes restent — et sur un pays où
le délai vaut 0, la course attend indéfiniment, comme avant.

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
`client` — c'est votre trajet, du début à la fin :

| Le point | Le pin |
|---|---|
| le **départ** (`kind: "pickup"`) — vous, qui attendez | `map_icon_url`, le pin **principal** |
| les **étapes** (`kind: "stop"`) | `numbered`, dans l'ordre de `stops[]` : le 1ᵉʳ arrêt après le départ porte le pin **1** |
| l'**arrivée** (`kind: "dest"`) | `dest_icon_url`, le pin de **destination** |

Le pin principal marque **quelqu'un** — vous —, celui de destination un
**lieu**. C'est ce qui permet de lire une carte sans lire les libellés.

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

{ "type": "hello",    "mission_id": "…", "vehicle_id": "…", "vehicle_type": "voiture",
  "plate": "…", "lng": 1.2255, "lat": 6.1319, "heading": 122.5,
  "heading_source": "gps", "speed": 8.3, "ts": 1757… }          ⚠️ v4.28.0
{ "type": "position", "vehicle_id": "…", "vehicle_type": "voiture", "plate": "…",
  "lng": 1.2255, "lat": 6.1319, "heading": 122.5, "speed": 8.3, "ts": 1757… }
{ "type": "status",   "status": "picking_up", "ts": 1757… }
```

L'identifiant de course sert d'identifiant de mission (`mission_id` =
`ride_id`). Le jeton d'accès passe dans l'URL (`?token=`) — un WebSocket de
navigateur ne porte pas d'en-tête.

### 🛰️ L'ACCUEIL PORTE LA DERNIÈRE POSITION CONNUE (v4.28.0)

**`hello` arrive désormais avec la position du véhicule**, aux mêmes champs
qu'une trame `position`. **Dessinez la voiture dès l'accueil**, sans attendre
autre chose.

⚠️ **Ce que ça répare.** L'accueil ne disait que « bonjour ». Il fallait
attendre la trame suivante — jusqu'à plusieurs secondes — pour savoir où
était le chauffeur. À chaque reconnexion (réseau retrouvé, application
revenue au premier plan, jeton rafraîchi), le passager voyait donc **sa carte
se vider, puis se remplir**. Sur un trajet, ce clignotement revenait
plusieurs fois.

- **`ts` est l'horodatage du DERNIER point reçu**, pas l'instant de la
  connexion. Il peut dater de quelques dizaines de secondes ; c'est à vous
  de dire « il y a 12 s » si vous le jugez utile. La plateforme ne l'invente
  pas et ne l'extrapole pas.
- **Un `hello` NU — sans `lng`/`lat` — reste possible** : chauffeur pas
  encore attribué, téléphone silencieux depuis trop longtemps, suivi
  indisponible. Comportez-vous alors comme avant : attendez la première
  `position`. **Ne dessinez jamais un `hello` sans coordonnées** comme s'il
  en avait.
- Un `hello` n'annule pas la règle du §5 : **l'état de la course se relit sur
  `GET /rides/{id}`**, le socket ne porte que la position et le mot du
  statut.

### 🔁 LA RECONNEXION EST OBLIGATOIRE, PAS OPTIONNELLE (v4.28.0)

**Tant qu'une course est vivante** (`searching` → `in_transit`), l'application
**doit** rouvrir le socket toute seule. Ce n'est pas un confort : un socket
tombé sans reconnexion laisse une voiture figée sur la carte, et rien à
l'écran ne dit que l'information a cessé d'arriver.

| Événement | Ce que l'application fait |
|---|---|
| Socket fermé, quelle qu'en soit la cause | rouvrir, back-off **1 s, 2 s, 4 s … 30 s** |
| Retour au **premier plan** | rouvrir **immédiatement**, sans attendre le back-off |
| Réseau retrouvé | rouvrir immédiatement |
| Code **4401 `token_expired`** | `POST /auth/refresh` **puis** rouvrir — jamais avec le même jeton |
| **403** à la poignée de main | s'arrêter : ce rôle ne suit pas cette course |
| Course terminée ou annulée | fermer, et ne plus rouvrir |

À chaque réouverture : relisez aussi **`GET /rides/{id}`**. Le socket rend la
position ; la course, elle, a pu changer d'état pendant la coupure.

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
| `ride_driver_honked` | **il klaxonne** : sur place, il ne vous voit pas (v4.24.0) — ⚠️ son et vibration d'urgence, ce n'est pas l'arrivée redite | `status: arrived`, `event: honk`, `honk_count` |
| `ride_cancelled` | annulée par le chauffeur ou la plateforme (`reason`) | `status: cancelled` |
| `ride_search_exhausted` | personne n'a pris la course — relancer ou annuler (§5, v4.1.0) | `status: searching`, `dispatch_state: exhausted` |
| `ride_cancelled` (`search_expired`) | la plateforme a **abandonné** une recherche épuisée depuis trop longtemps, et **remboursé** (§5, v4.25.0) | `status: cancelled`, `cancelled_by: system` |
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
| `POST /auth/register` · `/auth/login` · `/auth/refresh` · `/auth/logout` | la session — ⚠️ la connexion porte `app: "client"` depuis la v4.26.0 |
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

### 🚪 DIRE QUELLE APPLICATION SE CONNECTE — `app` (v4.26.0)

`POST /auth/login` accepte un champ `app`. **Envoyez `app: "client"` à chaque
connexion**, à côté du téléphone et du mot de passe :

```
POST /auth/login   { phone | email, password, app: "client" }
```

⚠️ **Ce qui se passait sans lui** : un chauffeur ou un livreur se connectait ici, et rien ne
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

Affichez « Ce compte est un compte chauffeur. Ouvrez l'application Dira
Chauffeur. » — **jamais « identifiants invalides »** : le mot de passe était
juste, et envoyer la personne changer un mot de passe correct ne mène nulle
part.

**Ce que la règle NE sépare PAS.** Elle sépare des FAMILLES de comptes, pas
des applications. `driver` vaut pour le chauffeur VTC **et** le livreur —
même rôle au socle ; `client` vaut pour la course **et** la livraison. Deux
applications de la même famille ne se distinguent donc pas l'une de l'autre
à la connexion. La famille `client`, elle, est bien tenue à l'écart des
autres.

**Un compte de DIRECTION entre partout.** C'est voulu, pas un trou :
l'exploitation ouvre votre application pour reproduire ce qu'un utilisateur
décrit au support.

**`app` omis ne vérifie rien.** Une version d'application pas encore mise à
jour continue donc de fonctionner exactement comme avant — mais le mauvais
compte y entre aussi comme avant. C'est la raison d'envoyer le champ dès
cette version.


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
