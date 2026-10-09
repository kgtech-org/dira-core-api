# App CLIENT — COURSES (VTC) — contrat d'API

> **Version 4.60.0** · 9 octobre 2026
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
| Fil | `X-Request-ID` — **sur chaque réponse** ; à l'aller, facultatif mais recommandé (voir la section *Le fil d'une requête*) |

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

## 🧵 LE FIL D'UNE REQUÊTE — `X-Request-ID` (v4.34.0)

**Chaque réponse porte un `X-Request-ID`.** C'est le nom sous lequel cette
requête est écrite dans nos journaux, et le même voyage **jusqu'au bout de la
chaîne** : un geste dans l'application traverse jusqu'à quatre services — le
métier, le socle (identité, portefeuille), le suivi (l'appel des chauffeurs) et
les cartes (l'itinéraire). Quatre journaux, un seul identifiant.

**Le minimum : lisez cet en-tête et gardez-le avec l'erreur que vous
enregistrez.** Un ticket qui cite le fil se lit en trente secondes. Sans lui, il
faut recouper quatre journaux à l'horodatage — ce qui marche jusqu'au jour où
deux personnes font le même geste la même seconde.

**Il est sur TOUTES les réponses, y compris un refus et y compris un `500`** —
précisément celles dont on parle. Un `5xx` est d'ailleurs rangé côté serveur
**avec ce fil dedans** : citez-le, et le défaut est retrouvé sans chercher.

**Mieux : envoyez le vôtre.** L'en-tête est accepté **à l'aller** aussi, et
repris tel quel.

⚠️ **C'EST LE SEUL MOYEN DE RETROUVER UN APPEL DONT LA RÉPONSE N'EST JAMAIS
ARRIVÉE.** Délai dépassé, tunnel, réseau qui tombe entre la question et la
réponse : il n'y a **rien à lire**, et ce sont exactement les appels qu'on
cherche à comprendre. L'identifiant que l'application a émis, lui, est dans nos
journaux — et s'il n'y est pas, cela répond aussi : la requête ne nous a jamais
atteints, le problème est en amont de nous.

⚠️ Un build qui tourne **dans un navigateur** ne peut lire un en-tête de réponse
que si la façade l'expose : une raison de plus d'envoyer le vôtre, qui marche
partout.

**La forme** : **64 caractères au plus**, pris dans `a-z A-Z 0-9 - _ .`

⚠️ **Hors de ces règles, l'identifiant est REFUSÉ — pas tronqué.** On tire le
nôtre, et la réponse dit lequel a été retenu : l'application n'est jamais
laissée sans fil. Tronquer aurait fait se confondre deux fils différents au
moment précis où on les cherche.

⚠️ **JAMAIS DE DONNÉE PERSONNELLE DEDANS.** Ni numéro de téléphone, ni nom, ni
jeton, ni identifiant de compte : ce mot est recopié dans les journaux de six
services, et une base de journaux ne se purge pas comme un compte se supprime.
Un identifiant d'installation plus un compteur suffit : `a1f9c2-1874`.

⚠️ **UN PAR REQUÊTE.** Le même sur tous les appels d'un écran regrouperait tout
et n'identifierait rien. Sur une **reprise du même appel**, en revanche, garder
le même est utile : les trois tentatives se relisent comme une seule histoire.

⚠️ **Le fil n'est pas une référence métier.** Il nomme un APPEL, pas une
commande ni une course : deux appels sur la même course ont deux fils, et un
appel rejoué à l'identique en a un troisième. Pour désigner l'objet, c'est son
`id` qu'on cite ; le fil dit *ce qui s'est passé cette fois-là*.

---

## 🗺️ LE FOND DE CARTE — `maps` (v4.40.0)

La plateforme sert **deux fonds de carte** : le nôtre, et Google. Le pays règle
celui qu'on voit **d'abord** ; la personne choisit ensuite celui qui lui va, et
son choix reste chez elle.

**Ce que le serveur rend**, dans la réponse de connexion, d'inscription et de
rafraîchissement :

```json
"maps": { "basemap": "dira" }
```

| | |
|---|---|
| `basemap` | `dira` ou `google` — le fond par **défaut** du pays, la case cochée d'avance |

C'est **tout** ce que le serveur a à dire de votre carte : quel fond montrer
d'abord ici. Un seul champ, une seule question.

### ⚠️ `google_key` ET `google_map_type` ONT DISPARU (v4.40.0)

Jusqu'à la 4.39.1, `maps` portait la clé Google du pays pour votre plateforme.
**Ces deux champs ne sont plus servis.** La raison est simple et elle vous
concerne : **une clé Google se restreint par ce qui l'utilise** — nom de paquet
+ empreinte SHA-1 sur Android, identifiant de bundle sur iOS, référent HTTP sur
le web. Elle appartient donc à **l'application**, pas au pays, et vous en avez
déjà une, correctement restreinte. Une clé servie par l'API arrivait trop tard,
pour quelqu'un qui n'en avait pas besoin.

Ce que ça change pour vous, dans l'ordre :

1. **Si vous lisiez `google_key`, cessez.** Elle sera absente, définitivement.
2. **Si vous conditionniez le CHOIX à sa présence, ne le conditionnez plus** :
   proposez **toujours** les deux fonds. C'était la règle inverse jusqu'ici, et
   c'est le seul point où votre code doit changer.
3. **Affichez Google avec VOTRE clé** — le SDK natif sur Android et iOS.
4. `platform` sur `/auth/login`, `/auth/register` et `/auth/refresh` est
   désormais **acceptée et ignorée**. ⚠️ **Ne la retirez pas** de vos requêtes :
   le serveur refuse les champs inconnus, mais celui-là reste déclaré
   exprès — l'envoyer ne coûte rien, et le retirer n'apporte rien.

### Où lire le défaut d'un pays — DEUX endroits, et il faut les deux (v4.41.0)

| | Quand | Ce qu'il dit |
|---|---|---|
| `maps.basemap` de `/auth/login`, `/auth/register`, `/auth/refresh` | à la connexion et à chaque rafraîchissement de jeton | le fond du pays où l'on **se connecte** |
| **`basemap` de chaque pays dans `GET /countries`** | à chaque ouverture de l'application, sans jeton | le fond de **n'importe quel** pays ouvert |

⚠️ **LE BLOC `maps` NE SUFFIT PAS, ET C'EST LE PIÈGE.** Il voyage avec le jeton :
il dit le fond du pays où l'on s'est connecté, **une fois**. Or l'application
rouvre sans se reconnecter (le jeton d'accès se rafraîchit, mais pas à chaque
lancement), et surtout **le pays peut changer sans reconnexion** — un passager
togolais qui ouvre l'application à Dakar opère au Sénégal. Une application qui ne
lirait que `maps` garderait le fond de la veille, dans le mauvais pays.

`GET /countries` est **public** et déjà appelé avant l'inscription : chaque pays
y porte désormais `basemap`.

```jsonc
{ "items": [
  { "code": "SN", "name": "Sénégal", "currency": "XOF", …, "basemap": "google" },
  { "code": "TG", "name": "Togo",    "currency": "XOF", …, "basemap": "dira" } ] }
```

### Ce que vous faites, concrètement

1. **Au lancement**, lisez `GET /countries` (vous l'appelez déjà) et gardez le
   `basemap` de chaque pays avec le reste de la fiche.
2. **Le fond effectif** se décide dans cet ordre, et il n'y en a pas d'autre :

   ```
   le choix de la personne (gardé chez vous)
     sinon  le basemap du pays où elle OPÈRE  (celui de l'en-tête X-Dira-Country)
       sinon  dira
   ```

3. **Tant que la personne n'a rien choisi, suivez le pays** — c'est exactement ce
   à quoi sert ce réglage. Dès qu'elle choisit, son choix **prime** et ne se
   perd plus. ⚠️ Il **survit au changement de pays** : quelqu'un qui passe du
   Togo au Sénégal a changé de pays, pas d'avis.
4. **Au changement de pays**, relisez le `basemap` du nouveau pays et
   **redessinez la carte** si le fond effectif change. Un fond lu à la première
   carte et jamais relu est un fond qui ment dès le second voyage.
5. `maps` **absent** (socle plus ancien) ou `basemap` inconnu = **notre fond**,
   et rien d'autre à faire.

> ⚠️ **Le fond change, la carte doit RENAÎTRE.** Les SDK lisent leur style à la
> création : changer la valeur sans reconstruire la vue ne se voit pas, et on
> croit le réglage sans effet. C'est le défaut que la console a eu le
> 30 septembre — le logo Google posé sur les tuiles de l'autre fond.

⚠️ **LE LOGO GOOGLE EST OBLIGATOIRE** dès que ses tuiles s'affichent : **16 dp de
haut au minimum**, **10 dp de dégagement**, **jamais recouvert** par un autre
logo — le vôtre compris. Ce n'est pas une politesse, c'est une clause du contrat
d'utilisation de l'API. Les SDK natifs le dessinent eux-mêmes ; ne le masquez pas
sous une barre ou un bouton flottant.

⚠️ **VOTRE clé se protège par sa RESTRICTION, pas par le secret.** Elle voyage en
clair sur chaque requête de tuile — Google l'exige. Restreignez-la (empreinte,
bundle, référent), plafonnez son quota, et ne la recopiez pas dans un journal ni
dans un rapport de plantage : ces fichiers partent chez un tiers et se gardent
des mois.

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

## 🔒 LE VERROU DE L'APPLICATION — `app_lock` (v4.46.0)

Biométrie ou code secret devant l'application : qui ouvre le téléphone de
quelqu'un n'ouvre pas pour autant son compte Dira. **Le verrou vit chez vous** —
le serveur ne peut ni le poser, ni vérifier qu'il y est. Ce qu'il dit, c'est la
**politique du pays**, réglée depuis la console :

```jsonc
// à la connexion, à l'inscription, à la vérification d'un code, au rafraîchissement
"app_lock": {
  "mode": "optional",      // "off" · "optional" · "required"
  "biometrics": true,      // false = CODE SEUL
  "pin_length": 4,         // 4 ou 6
  "grace_seconds": 120,    // temps hors de l'app avant de redemander ; 0 = à chaque retour
  "max_attempts": 5        // échecs avant DÉCONNEXION
}
```

Et dans `GET /countries`, pour l'application qui change de pays sans se
reconnecter.

**Ce que chaque mode demande**

| `mode` | Ce que l'application fait |
|---|---|
| `off` | ne rien proposer — pas de case dans les réglages ; une case qui ne fait rien est pire que pas de case |
| `optional` | proposer dans les réglages, **éteint par défaut** |
| `required` | **exiger** la pose d'un verrou avant le premier écran, et ne pas offrir de le retirer |

⚠️ **LE CODE SECRET EST TOUJOURS POSSIBLE, MÊME AVEC LA BIOMÉTRIE.** Un capteur
cassé, un doigt mouillé, un visage dans le noir : sans code de secours, le
verrou enferme dehors quelqu'un qui n'a rien fait. `biometrics: false` veut dire
« code **seul** », jamais l'inverse.

⚠️ **AU-DELÀ DE `max_attempts`, ON DÉCONNECTE — ON NE BLOQUE PAS.** Jetez le
jeton de rafraîchissement (`POST /auth/logout`), effacez l'état local, revenez à
l'écran de connexion. Un téléphone volé qui se *bloque* garde un jeton valide
**trente jours** ; déconnecté, il ne garde rien. Et la personne légitime
retrouve son compte avec un code à usage unique (client) ou son mot de passe.

⚠️ **NE VERROUILLEZ JAMAIS UN APPEL DE COURSE NI UN BOUTON D'URGENCE.** Un appel
dure trente secondes : derrière un code, c'est un appel manqué, et le chauffeur
désactivera le verrou le jour même. Le verrou garde l'application, pas l'écran
qui sonne.

⚠️ **`grace_seconds` DÉCIDE SI LE VERROU SERA SUPPORTÉ OU CONTOURNÉ.** Compter
depuis le passage en arrière-plan, pas depuis la dernière saisie : redemander le
code parce que quelqu'un est allé lire le SMS de son opérateur fait désinstaller
l'application. `0` existe et se choisit — pour un parc de téléphones partagés.

⚠️ **CE QUE LE VERROU NE PROTÈGE PAS.** Il arrête l'ami curieux et le téléphone
laissé sur une table. Il n'arrête pas qui extrait le stockage d'un appareil
débridé : le jeton est là. Ce qui protège la donnée, c'est **le stockage
sécurisé** (Keychain `AfterFirstUnlockThisDeviceOnly`, Keystore) et la durée de
vie du jeton — jamais ce réglage.

⚠️ **NE L'ENVOYEZ PAS AU SERVEUR, NE LE RANGEZ PAS EN CLAIR.** Le code ne quitte
pas le téléphone ; il se dérive (PBKDF2, Argon2) avec un sel dans le stockage
sécurisé, ou mieux, il déverrouille le trousseau du système. Le serveur ne le
connaît pas, et ne doit pas le connaître : il n'aurait aucun moyen de le
vérifier sans devenir le point de panne de l'ouverture de l'application.

**La politique change sans reconnexion** : relisez-la à chaque réponse qui la
porte (connexion, inscription, vérification de code, **rafraîchissement**) et
appliquez-la à chaud. Un pays qui passe à `required` ne doit pas attendre
l'expiration d'un jeton de trente jours.

---

### 📱 COMBIEN D'APPAREILS — et lequel se déconnecte (v4.46.0)

Votre compte tient **plusieurs appareils** : le téléphone, la tablette, celui
qu'on vient de changer. Le nombre est réglé **par pays** (trois par défaut) et
se lit dans `GET /countries` → `max_devices`.

⚠️ **AU-DELÀ, LA SESSION LA PLUS SILENCIEUSE PART** — pas la première ouverte.
Une session est datée de son **dernier rafraîchissement** : le téléphone dont on
se sert tous les jours se redate seul, celui qui dort dans un tiroir s'en va le
premier. L'appareil évincé ne reçoit rien sur le moment : il l'apprend à son
prochain rafraîchissement, qui répond **`401`**. Traitez-le comme une session
expirée ordinaire — ramenez à l'écran de connexion, n'affichez pas « erreur ».

⚠️ **ENVOYEZ `device_id` À LA CONNEXION, MÊME SI VOUS ÊTES UNE APPLICATION DE
CLIENT.** Il ne va pas dans votre jeton et ne vous soumet à aucune règle d'agent ;
il sert à une seule chose : **reconnaître le même téléphone qui revient**. Sans
lui, une réinstallation compte pour un appareil de plus et pousse dehors un
autre de vos appareils. Avec lui, votre session précédente est simplement
remplacée. Il doit **survivre aux redémarrages** et vivre aussi longtemps que le
jeton de rafraîchissement, à côté de lui.

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

### 🛵 LE MOTO-TAXI — un mode à part, ouvert dans tous les pays (v4.48.0)

```json
{ "key": "moto", "name": "Moto", "note": "Moto-taxi · 1 passager", "seats": 1,
  "map_icon": "moto", "icon_url": null,
  "modes": { "free": { … }, "rental": { … } } }
```

Il arrive **en tête du catalogue** — c'est la course la moins chère et la plus
courante de la région. Il était jusqu'ici rangé dans `eco`, avec les
citadines.

⚠️ **`seats` VAUT 1, ET CE N'EST PAS DÉCORATIF.** Ne proposez pas « 2
passagers », ni bagage, ni siège enfant sur un mode à une place. Lisez `seats`
plutôt que de le déduire de la `key` : le jour où un tricycle arrive, il en
portera trois.

⚠️ **PAS DE COURSE PARTAGÉE SUR UNE MOTO**, et vous le savez **sans règle
spéciale** : `modes` ne porte pas la clé `pool`. C'est déjà la règle générale —
une clé absente = mode non vendu par ce véhicule —, et c'est pour cela qu'il
n'y a rien à coder de particulier. N'écrivez surtout pas « si key == moto
alors… » : c'est le serveur qui décide, mode par mode et pays par pays.

⚠️ **`icon_url` EST `null` AU DÉPART**, le temps que l'exploitation pose son
image. Dessinez alors la silhouette `map_icon` (`moto`) — **pas** l'icône d'une
voiture, et surtout pas une image en dur de votre côté : elle resterait là le
jour où la vraie arrive. C'est la même règle que pour tous les modes, et c'est
le premier mode qui la met vraiment à l'épreuve.

⚠️ **UNE MOTO N'EST JAMAIS ENVOYÉE SUR UNE COURSE DE VOITURE, NI L'INVERSE.**
Le reste du catalogue est une hiérarchie — une berline prend une course éco, le
passager monte dans mieux que ce qu'il a payé — mais la moto est **en dehors** :
elle ne sert qu'elle-même, et aucune voiture ne répond à un appel de moto. Rien
à faire de votre côté : c'est le serveur qui trie avant de sonner. Ne promettez
donc pas « une voiture si aucune moto n'est libre » — cela n'arrivera pas, et
l'écran d'attente doit dire la vérité.

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

### 🎟️ LE CODE PROMO — celui qu'on TAPE (v4.51.0)

Une promotion s'applique d'elle-même ; un **code** se saisit. Il vient d'une
affiche, d'un influenceur ou du parrainage d'un proche, et il est **unique pour
toute la plateforme** : le même mot ne peut pas valoir une chose sur une course
et une autre sur une commande de repas.

**Un seul champ, au devis** :

```
POST /rides/quote
{ "stops": [ … ], "code": "DIRA2000" }
```

La réponse ne change pas de forme : chaque classe porte son prix, déjà remisé,
plus ce que le code a donné **sur cette classe**.

```json
{ "items": [ {
  "class_key": "eco", "fare_xof": 2000,
  "promo_code": "DIRA2000", "promo_code_xof": 500,
  "expires_at": "2026-10-08T12:43:20Z"
} ] }
```

> ⚠️ **`fare_xof` EST DÉJÀ REMISÉ**, comme avec une promotion automatique. Ne
> soustrayez **rien** : `promo_code_xof` sert à **afficher** la remise, pas à
> la calculer.

> ⚠️ **LA REMISE N'EST PAS LA MÊME SUR TOUTES LES CLASSES**, et c'est normal :
> 20 % d'une moto et 20 % d'un van ne font pas le même nombre de francs, et un
> code peut être plafonné. Affichez `promo_code_xof` **ligne par ligne**, jamais
> une remise unique en tête de liste — elle serait fausse sur trois classes sur
> quatre.

> ⚠️ **UN SEUL APPEL, POUR LES QUATRE CLASSES.** Le serveur valide le code
> **une fois** et calcule une remise par prix. C'est ce qui garantit qu'un code
> ne peut pas être « accepté pour la moto et refusé pour le van » dans le même
> écran : le verdict est unique, seule la remise varie.

#### ⚠️ JAMAIS DEUX REMISES SUR UNE COURSE

`promo_title` et `promo_code` ne sont **jamais servis en même temps**. Une
application qui additionnerait les deux afficherait un montant que personne ne
paie.

C'est **la meilleure des deux** qui gagne, pour le passager. Et quand c'est
l'offre automatique qui gagne, le code n'est pas effacé pour autant — il revient
avec un drapeau :

```json
{ "fare_xof": 2100, "promo_title": "Soirée", "promo_discount_xof": 400,
  "promo_code": "DIRA2000", "promo_code_ignored": true }
```

> ⚠️ **`promo_code_ignored` DOIT ÊTRE DIT À L'ÉCRAN.** Un code accepté qui ne
> change pas le prix se lit comme un code cassé : le passager le ressaisit,
> vérifie les majuscules, puis appelle le support — qui n'en saura pas plus. La
> seule phrase vraie est **« gardez-le : une meilleure offre s'applique déjà »**,
> et ce champ est la seule chose qui permet de l'écrire. Ne le traitez pas comme
> un refus : il n'y a rien à corriger, et le code n'a **rien consommé** — il
> servira une autre fois.

#### Les refus, et ce que l'écran doit en faire

Un code saisi mérite une réponse qui dit **quoi faire**. Le devis échoue avec un
code nommé plutôt que de servir le prix plein en silence — sans quoi le passager
croirait que son code marche et qu'il ne donne rien.

| Code | Ce qui se passe | Ce que l'écran dit |
|---|---|---|
| `promo_code_invalid` | la saisie n'a pas la forme d'un code | « vérifiez le code » — garder le champ ouvert |
| `promo_code_unknown` | ce code n'existe pas | « ce code n'existe pas » |
| `promo_code_expired` | hors de sa fenêtre, ou éteint | « cette offre est terminée » |
| `promo_code_wrong_country` | valable ailleurs — `meta.valid_in` porte le pays | « ce code vaut au Togo, pas ici » |
| `promo_code_wrong_service` | valable pour l'autre métier — `meta.valid_for` | « ce code est pour les commandes » |
| `promo_code_amount_too_low` | sous le minimum — `meta.min_amount_xof` | « à partir de 3 000 F » |
| `promo_code_exhausted` | l'enveloppe de l'offre est consommée | « cette offre est épuisée » |
| `promo_code_already_used` | la limite par personne est atteinte — `meta.max_uses_per_user` | « vous avez déjà utilisé ce code » |
| `promo_code_own_referral` | c'est **son propre** code de parrainage | « partagez-le : il est pour vos proches » |

> ⚠️ **N'AFFICHEZ PAS « CODE INVALIDE » POUR TOUT.** C'est le message qui envoie
> tout le monde au support, et le support n'en saura pas plus que l'écran. Les
> neuf refus ci-dessus ont chacun une suite différente : l'un demande de corriger
> une faute de frappe, l'autre de renoncer, l'autre d'attendre d'avoir une course
> plus longue. `message` est déjà traduit par l'API — affichez-le.

> ⚠️ **`promo_code_own_referral` N'EST PAS UNE ERREUR DE L'UTILISATEUR.** C'est
> le premier geste de tout le monde : on reçoit son code, on l'essaie. L'écran
> doit l'accueillir comme une explication, pas comme un échec — « ce code est
> celui que vous donnez à vos proches ».

#### Le code est FIGÉ avec le devis, et engagé à la commande

Comme la majoration et la promotion : **ce qui a été affiché est ce qui est
payé**. Le code, son libellé et son montant restent sur la course, et le reçu
pourra l'expliquer des semaines plus tard.

L'enveloppe de l'offre, elle, n'est **engagée qu'à la commande** — un passager
demande dix prix et en commande un. Conséquence : un code encore valable au
devis peut voir son enveloppe se vider entre deux écrans. **Le prix promis ne
change pas pour autant** : la course part au prix affiché. Vous ne verrez jamais
un prix bouger sous les yeux du passager à cause de notre comptabilité.

Annuler la course **rend** l'enveloppe : le code redevient utilisable.

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
POST /ai/voice                multipart : file=<audio>, language=<code facultatif>
                                                                → { "text": "…", "language": "fr" }
```

**La langue parlée (v4.39.0).** Ajoutez `language=<code>` au multipart quand
la personne a choisi sa langue ; **n'envoyez rien** sinon — le serveur prend
alors la **première langue du pays**, réglée par l'exploitation (au Sénégal
ce peut être le wolof, au Togo le français). ⚠️ **Ce n'est PAS
`Accept-Language`** : celui-ci dit la langue de l'interface, et un téléphone
réglé en français n'empêche pas son propriétaire de parler wolof. Douze codes
sont admis, tout autre est refusé (`422 unknown_language`) :

| Code | Langue | Pays |
|---|---|---|
| `fr` · `en` | français · anglais | tous |
| `ee` · `kbp` | éwé · kabiyè | Togo |
| `wo` · `ff` · `srr` | wolof · peul (pulaar) · sérère | Sénégal |
| `man` · `sus` · `ff` | malinké · soussou · peul (pular) | Guinée |
| `shu` · `sba` · `ff` | arabe tchadien · ngambay (sara) · peul (fulfulde) | Tchad |
| `fan` | fang | Gabon |

La réponse porte la langue retenue : `{ "text": "…", "language": "wo" }`.
Deux refus de plus : `503 language_not_served` (`reason` = la langue) quand
aucun fournisseur configuré ne l'entend — gardez le clavier, comme pour
`assistant_unavailable` — et `422 unknown_language`. Un vocal dans une
langue locale est transcrit **dans cette langue**, mots français compris :
ne le traduisez pas, montrez-le tel quel.

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

## 4 quater. 🚕 LES AUTRES MODES DE COURSE (v4.31.0)

**Trois modes en plus du mode ordinaire. Le PAYS les ouvre, et chaque VOITURE
dit lesquels elle vend — les deux conditions, pas l'une ou l'autre.**

```
GET /settings/modes?near=lng,lat
  → { free:   { enabled, scannable, max_hours },
      rental: { enabled, max_radius_km, alert_km_before },
      pool:   { enabled, pickup_radius_m, dropoff_radius_m,
                min_distance_m, search_ttl_s, max_riders,
                fare_pct } }                                      # v4.44.0

GET /classes?near=lng,lat
  → items[].modes = {
       "free":   { base_xof, per_km_xof, per_min_xof, min_fare_xof },
       "rental": { tiers: [ { hours, price_xof, included_km } ], extra_per_km_xof },
       "pool":   { } }    # v4.44.0 — présence = cette voiture partage
```

⚠️ **PASSEZ `near` SUR LES DEUX, AVEC LE POINT DE DÉPART** (v4.42.0). Les deux
routes rendent alors les réglages et le catalogue **du pays où la course se
fera**. Sans `near`, elles rendent ceux du pays du **compte** — et un passager
togolais à Dakar lisait jusqu'ici les modes du Togo avec les prix du Sénégal :
deux pays dans un seul écran. Tant que vous ne connaissez pas encore le départ,
appelez sans `near` ; **rappelez dès que le passager l'a posé**.

⚠️ **DEUX DOCUMENTS, ET ILS NE DISENT PAS LA MÊME CHOSE.** Les réglages disent
ce que le pays **ouvre** et les bornes qui valent pour toutes les voitures ; le
catalogue dit ce que **cette voiture-là** vend, et **à quel prix**.

⚠️ **LISEZ-LES AVANT DE MONTRER QUOI QUE CE SOIT.** Un mode éteint doit
**disparaître de l'écran**, pas y rester et échouer.

⚠️ **UNE CLÉ PRÉSENTE DANS `modes` = UN MODE VENDU PAR CETTE VOITURE, DANS CE
PAYS.** Le serveur a déjà croisé les deux conditions : `modes` ne contient
jamais un mode que le pays a fermé. Vous n'avez donc **pas** à recouper les
deux documents écran par écran — et c'est le point : le premier écran qui
l'oublierait proposerait une location qu'on refuse à la commande, au pire
moment. Une voiture **sans** `modes.rental` ne se loue pas ; ne l'affichez pas
dans la liste des locations.

⚠️ **LA POLITIQUE EST DANS LES RÉGLAGES, LES PRIX DANS LE CATALOGUE — et il
n'y a qu'un endroit pour chacune.** « Le partage est-il ouvert ? à partir de
combien de kilomètres ? pendant combien de temps cherche-t-on ? » :
`GET /settings/modes`. « Combien coûte ce trajet dans cette voiture ? » : le
devis, et la grille du catalogue pour l'expliquer. Si vous trouvez la même
valeur aux deux endroits, c'est une erreur de notre part — dites-le.

⚠️ **LES PRIX D'UN MODE NE SONT PLUS DANS LES RÉGLAGES DEPUIS LA v4.31.0.**
Les plages de location, leur forfait, les kilomètres compris, le tarif du
compteur et son plancher **vivent sur la voiture**, parce qu'ils en dépendent :
un van immobilisé cinq heures n'est pas une eco. Une application qui lirait
encore `settings.rental.tiers` n'y trouverait **plus rien** — et n'aurait aucune
durée à proposer.

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

#### ⚠️ Ne proposez que les durées DE LA VOITURE choisie

Les durées à afficher sont **celles de sa classe, et rien d'autre** :

```
GET /classes → items[].modes.rental.tiers
```

```jsonc
// "van" se loue 1 h, 5 h et 24 h ; "eco" se loue 1 h et 5 h — et pas au même prix.
{ "key": "eco", "name": "Eco", "modes": {
    "rental": { "extra_per_km_xof": 150, "tiers": [
      { "hours": 1, "price_xof": 5000,  "included_km": 20 },
      { "hours": 5, "price_xof": 20000, "included_km": 80 } ] } } }

{ "key": "van", "name": "Van", "modes": {
    "rental": { "extra_per_km_xof": 250, "tiers": [
      { "hours": 1,  "price_xof": 12000,  "included_km": 20 },
      { "hours": 5,  "price_xof": 35000,  "included_km": 80 },
      { "hours": 24, "price_xof": 200000, "included_km": 300 } ] } } }
```

⚠️ **IL N'Y A PLUS DE GRILLE COMMUNE À RECOUPER** (v4.31.0). Jusqu'à la
v4.30.0 les plages vivaient dans les réglages du pays, une ligne pouvait ne
nommer aucune classe, et il fallait faire l'emboîtement soi-même — le mode
exact l'emportant sur la ligne commune. Une seule application qui inversait
l'ordre affichait un van au prix d'une eco. **Chaque voiture porte maintenant
ses propres plages, entières.** Prenez-les telles quelles.

⚠️ **MONTRER LA GRILLE D'UNE AUTRE VOITURE EST UNE ERREUR.** Un passager qui
choisit « 24 h » sur une eco qui ne se loue pas à la journée se verrait refuser
après coup — c'est pire qu'une durée qu'on ne lui a jamais proposée.

⚠️ **NI `modes.rental` NI DE `tiers` = CETTE VOITURE NE SE LOUE PAS.** Retirez-la
de l'écran de location ; elle reste commandable normalement.

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

**`422 rental_out_of_range`** : l'arrêt demandé sort du rayon. ⚠️ **Sa phrase
porte les deux chiffres** — « Cet arrêt est à 190 km du départ : votre location
va jusqu'à 60 km » — et c'est elle qu'il faut afficher : un « trop loin » sans
chiffres laisse deviner de combien.

⚠️ **Corrigé en v4.42.0** : ce paragraphe annonçait un `meta` portant
`distance_km` et `max_km`. **Il n'arrive pas** — l'enveloppe ne rend que `code`,
`message`, `fields` et `reason` (§8), et c'est la règle depuis le début. Les
nombres n'ont jamais voyagé qu'à l'intérieur de la phrase traduite.

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

### 🤝 La course PARTAGÉE — deux passagers, une voiture (v4.42.0)

Deux personnes dont les **départs** et les **arrivées** sont proches montent
dans la même voiture et paient **chacune son trajet**, moins cher. Le chauffeur
sert deux courses pour un seul déplacement.

⚠️ **ET LE MODE CHERCHE LE CO-PASSAGER *AVANT* LE CHAUFFEUR.** C'est ce qui le
distingue de tous les autres modes, et ce qui change l'écran d'attente : lisez
la section suivante avant de câbler quoi que ce soit.

> ⚠️ **Ce n'est PAS le « partage de trajet » de l'écran sécurité** (envoyer sa
> position à un proche), qui n'existe toujours pas. Ici, on partage la
> **voiture** avec un inconnu.

```
POST /rides/pool/quote  { "stops": [ { kind: "pickup", … }, { kind: "dest", … } ] }
  → { items: [ { id, class_key, mode: "pool", fare_xof, distance_m, duration_s,
                 country, expires_at }, … ] }

POST /rides  { quote_id, payment_method }        # comme un devis ordinaire
```

#### ⚠️ LE PRINCIPE : ON CHERCHE LE CO-PASSAGER *AVANT* LE CHAUFFEUR

**C'est la seule chose à retenir, et tout le reste en découle.** Une course
partagée ne cherche pas une voiture tout de suite : elle cherche d'abord
**quelqu'un qui fait le même trajet**. Le groupe formé, et seulement alors, on
appelle **un** chauffeur pour les deux.

```
1. RECHERCHE DU CO-PASSAGER          2. RECHERCHE DU CHAUFFEUR
   dispatch_state: "pooling"            dispatch_state: "calling"
   jusqu'à 5 min (pool_until)           UN seul appel, pour le groupe
   ⚠️ AUCUN chauffeur n'est appelé      sur un cercle couvrant les 2 départs
            │                                      │
            └──── le groupe est FIGÉ ──────────────┘
                  (pool_id, pool_size: 2)
```

⚠️ **PENDANT L'ÉTAPE 1, PERSONNE N'EST APPELÉ. Ne dites pas « nous cherchons un
chauffeur »** — ce serait faux, et le passager s'étonnerait que ça dure cinq
minutes. Dites **« nous cherchons quelqu'un qui fait le même trajet »**, et
décomptez `pool_until`.

⚠️ **POURQUOI DANS CET ORDRE, ET PAS L'INVERSE.** Appeler un chauffeur dès le
premier passager aurait semblé plus rapide, et ne tenait pas : un chauffeur
accepte en vingt secondes. Il aurait fallu, ensuite, soit **lui imposer un arrêt
qu'il n'a pas accepté**, soit **faire partir le premier passager seul au prix du
partage**. Le groupe est donc figé **avant** que le téléphone d'un chauffeur ne
sonne : ce qu'il accepte est ce qu'il roulera.

⚠️ **CONSÉQUENCE DIRECTE : ON NE REJOINT PLUS UN GROUPE APRÈS L'ACCEPTATION.**
Un troisième passager qui arriverait après coup ne sera **pas** ajouté — ni à
votre course, ni à la voiture.

**Les mêmes deux temps, champ par champ :**

```
POST /rides
   │
   ├─▶ dispatch_state: "pooling"      ← 1. on cherche un CO-PASSAGER
   │     pool_until: <instant>           AUCUN chauffeur n'est appelé
   │
   ├─▶ dispatch_state: "calling"      ← 2. le groupe est formé, on appelle
   │     pool_id, pool_size: 2           UNE voiture pour les deux
   │
   └─▶ status: "accepted"             ← un chauffeur a pris le groupe
```

⚠️ **`pool_until` EST UN INSTANT, pas une durée** — comme `eta_at`. Décomptez-le
localement, sans réinterroger : une durée servie il y a trois minutes en vaut
une de moins.

**Quand le groupe se forme**, le passager qui attendait reçoit le push
**`ride_pool_matched`** (`data.type: "ride_status"`, `dispatch_state: "calling"`,
`pool_id`) — relisez la course. Celui qui vient de commander, lui, voit déjà
`calling` dans la réponse de `POST /rides` : il n'y a pas de push pour lui.

#### Le marché, et les règles qui en découlent

**Trois personnes y gagnent quelque chose, et chacune y renonce à quelque
chose.** C'est le marché, et tout le reste en découle :

| | Ce qu'elle gagne | Ce qu'elle accepte |
|---|---|---|
| **Le passager** | un prix plus bas (70 % du tarif, par défaut) | attendre qu'on trouve quelqu'un, un détour, un inconnu à bord |
| **Le chauffeur** | deux courses pour un seul déplacement | un arrêt de plus, et un trajet qui n'est pas le plus direct |
| **La plateforme** | des sièges vides remplis | apparier, et une commission par course un peu plus haute |

⚠️ **SI L'UN DES TROIS N'Y GAGNE RIEN, LE MODE NE PREND PAS.** C'est pour cela
qu'il y a un **trajet minimum** (sur deux kilomètres, le détour coûte plus que
la remise), une **remise réelle** (sans elle, personne n'accepterait l'inconnu),
et une **limite de détour** (les deux rayons). Ces trois réglages ne sont pas
des garde-fous techniques : ce sont les termes du marché.

##### 1. Ce qui décide que deux trajets peuvent se partager

Le serveur compare votre course aux courses **qui attendent déjà** un
co-passager. Cinq conditions, **toutes** nécessaires :

| Condition | Pourquoi |
|---|---|
| **même classe de véhicule** | le prix, les places et ce que chacun a choisi en dépendent : une eco et un van ne se partagent pas |
| **même pays** | réglages, monnaie et villes desservies diffèrent |
| **départs proches** — ≤ `pickup_radius_m` | c'est le détour que le second impose au premier **avant** de rouler |
| **arrivées proches** — ≤ `dropoff_radius_m` | ⚠️ **les deux, pas l'une des deux** : deux personnes peuvent partir du même immeuble et aller à l'opposé de la ville |
| **le groupe n'est pas complet** — < `max_riders` | deux par défaut |

Entre deux candidates valables, le serveur garde **la plus ancienne** : elle
attend depuis plus longtemps, et sa fenêtre se referme en premier.

```
Lomé — rayons à 2,5 km, trajet minimum 3 km

  Ana  : Tokoin ─────────────▶ Aéroport        ⏳ attend depuis 2 min
  Boris: Nukafu ─────────────▶ Aéroport        ✅ départs à 1,0 km, même arrivée
  Carl : Agoé   ─────────────▶ Aéroport        ❌ départ à 5,4 km de Tokoin
  Dina : Tokoin ─────────────▶ Bè Plage        ❌ même départ, arrivée à 4 km
```

⚠️ **CE N'EST PAS VOTRE TRAVAIL DE CALCULER ÇA**, et il ne faut pas essayer : le
serveur ne vous donne jamais les courses qui attendent (ce serait donner
l'adresse d'inconnus). Vous proposez le mode, vous commandez, vous affichez ce
qui revient. Les rayons sont servis dans les réglages **pour que vous puissiez
l'expliquer**, pas pour que vous l'appliquiez.

##### 2. La chronologie, les deux passagers côte à côte

Ana commande à 9 h 00, Boris à 9 h 02.

| | Ana (première) | Boris (second) |
|---|---|---|
| 9 h 00 | `POST /rides` → `pooling`, `pool_until` = 9 h 05. Écran : « on cherche quelqu'un qui fait le même trajet », décompte 5 min | — |
| 9 h 02 | push **`ride_pool_matched`** → relire la course : `calling`, `pool_id`, `pool_size: 2` | `POST /rides` répond **déjà** `calling` + `pool_id`. ⚠️ **Pas de push pour lui** : il a la réponse sous les yeux |
| 9 h 02 | « nous cherchons une voiture pour vous deux » | idem |
| 9 h 03 | `accepted` — push `ride_accepted`, la carte du chauffeur | `accepted` aussi, **même chauffeur, même plaque** |
| 9 h 03+ | la voiture vient chercher Ana | ⚠️ la voiture vient chercher **Ana d'abord** |
| 9 h 09 | Ana à bord (`in_transit`) | la voiture arrive ensuite chez Boris |
| 9 h 14 | — | Boris à bord |
| 9 h 30 | Ana déposée → `completed`, note, reçu | course encore `in_transit` |
| 9 h 36 | — | Boris déposé → `completed` |

⚠️ **LES DEUX COURSES AVANCENT SÉPARÉMENT.** Chacune a son `status`, son prix,
sa note, son reçu, sa conversation. Il n'existe **aucun** objet « course de
groupe » à suivre : vous suivez **votre** course, comme toujours.

##### 3. Pourquoi la voiture semble venir d'ailleurs

L'appel du chauffeur n'est **pas** centré sur votre départ : il est centré au
**milieu** des deux départs, sur un cercle élargi de leur demi-distance.

```
     Tokoin ●───────── 1 000 m ─────────● Nukafu
                       ▲
                   le centre de l'appel (ni l'un ni l'autre)
     rayon = rayon d'appel du pays + 500 m
```

⚠️ **APPELER SUR LE DÉPART DE L'UN DES DEUX SERAIT FAUX** : le chauffeur le plus
proche d'Ana peut être hors de portée de Boris, et l'on appellerait des voitures
qui n'ont aucune raison d'être proches du trajet réel. Conséquence pour vous :
**le chauffeur peut arriver d'une direction inattendue**, et son `eta_at` peut
être plus long que sur une course ordinaire. N'expliquez pas, **prévenez** : « il
récupère d'abord l'autre passager » suffit.

##### 4. L'ordre de route : les deux montées, PUIS les deux descentes

⚠️ **LE CHAUFFEUR NE DÉPOSE PERSONNE AVANT D'AVOIR PRIS LES DEUX.** Déposer le
premier avant d'aller chercher le second ferait **deux courses à la suite**, pas
une course partagée — et le second attendrait tout le trajet du premier.

Pour vous, cela veut dire deux choses :

- **si vous montez en premier**, la voiture fait un arrêt **avant** de partir
  vers votre destination. Dites-le **au moment de commander**, pas quand ça
  arrive ;
- **si vous montez en second**, la voiture arrive chez vous avec quelqu'un
  dedans.

⚠️ **ET `eta_at` EST OPTIMISTE POUR LE SECOND PASSAGER — sachez-le.** Il est
calculé en ligne directe depuis la position du chauffeur vers **votre** arrêt :
il **ne passe pas** par la prise en charge de l'autre. Tant que le chauffeur n'a
pas récupéré le premier passager, le chiffre annoncé est donc **plus court** que
la réalité.

Ce qu'on vous demande d'en faire, et c'est la seule chose honnête : **ne
l'affichez pas comme une promesse sur une course partagée tant que `pool_size`
> 1 et que vous n'êtes pas encore à bord.** Dites « il récupère d'abord l'autre
passager » plutôt qu'un décompte qui va glisser. Dès que vous êtes `in_transit`,
`eta_at` redevient exact : il n'y a plus d'arrêt intercalé.

⚠️ **VOUS NE RECEVEZ NI LE NOM NI L'ADRESSE DE L'AUTRE PASSAGER**, et c'est
délibéré : on ne donne pas l'adresse de quelqu'un à un inconnu avant qu'il ne
monte en voiture. Vous savez seulement que vous partagez (`pool_size`). Le
chauffeur, lui, a l'itinéraire complet : c'est lui qui conduit.

##### 5. Les quatre façons dont ça se termine

```
pooling ──┬──▶ personne n'est venu          → exhausted · pool_no_match
          └──▶ groupe formé → calling ──┬──▶ un chauffeur prend  → accepted ✅
                                        ├──▶ aucun preneur       → exhausted
                                        ├──▶ le co-passager annule→ exhausted · pool_partner_left
                                        └──▶ l'appel n'a pas pu s'ouvrir
                                                                 → exhausted · pool_call_failed
```

⚠️ **DANS LES QUATRE CAS D'ÉCHEC, LA COURSE RESTE LA VÔTRE** : `searching`, non
annulée, mode et prix inchangés. C'est à vous de proposer la suite — et
`dispatch_reason` dit laquelle (voir plus bas).

##### 6. Ce que le passager va demander

Préparez ces réponses : ce sont celles que le support reçoit.

| Il demande | Répondez |
|---|---|
| « pourquoi ça prend si longtemps ? » | on cherche quelqu'un qui fait le même trajet — jusqu'à `search_ttl_s` ; après, vous décidez |
| « où est mon chauffeur, il va dans l'autre sens » | il récupère l'autre passager d'abord |
| « ça disait 4 min et ça fait 10 » | ⚠️ sur une course partagée, avant d'être à bord, le décompte ne compte pas l'autre prise en charge — **ne le montrez pas** |
| « qui est cette personne ? » | un autre client Dira, qui va au même endroit. Ni son nom ni son adresse ne vous sont donnés |
| « pourquoi je paie moins ? » | `100 − share_pct` % de moins, parce que vous partagez la voiture |
| « l'autre a annulé, je paie plus ? » | **non** — le prix est figé, et l'annulation n'est pas la vôtre |
| « je peux partager avec un ami ? » | **non** : c'est le serveur qui apparie, sur la proximité des trajets |

#### Le prix : chacun sa distance, remisée d'une PART (v4.44.0)

Le devis et la course portent **`share_pct`** : la **part du tarif ordinaire**
que ce passager paie.

```jsonc
// `fare_xof: 2000` au lieu de 2 850 — et on vous dit pourquoi
{ "mode": "pool", "fare_xof": 2000, "share_pct": 70, … }
```

⚠️ **`share_pct: 70` VEUT DIRE « IL PAIE 70 % », donc 30 % de moins.** C'est
une **part**, pas une remise : le nombre servi est celui qu'on paie. Pour
afficher l'économie, montrez `100 − share_pct`.

⚠️ **AFFICHEZ-LA.** « 2 000 F » à côté de « 2 850 F » ne se comprend pas tout
seul : c'est « −30 % » qui donne une raison d'accepter un détour et un inconnu
à bord. Et sur le **reçu**, des mois plus tard, c'est la seule chose qui
explique un montant inférieur au tarif — sans elle, le client lit une erreur de
facturation.

⚠️ **IL N'Y A PLUS DE GRILLE DE PARTAGE** (v4.44.0). Jusqu'à la v4.43.0 chaque
voiture avait la sienne, servie dans `modes.pool`. Elle ne facture plus et
n'est plus servie : le prix est **une part de la grille ordinaire**, réglée par
**pays** (`settings.modes.pool.fare_pct`). Une application qui lirait encore
`modes.pool.base_xof` n'y trouverait **rien**.

⚠️ **IL N'EXISTE AUCUN PRIX COMMUN.** Chaque passager paie **son** trajet,
remisé. Les deux ne font pas le même chemin — l'un roule peut-être deux fois
plus longtemps que l'autre. **N'affichez pas un montant « à deux », ne divisez
rien** : `fare_xof` est ce que *cette* personne paie.

⚠️ **LE PRIX EST FIGÉ AU DEVIS, ET IL NE BOUGE PLUS.** Trouver un co-passager
ne le baisse pas ; ne pas en trouver ne le remonte pas ; le co-passager qui
annule en route ne le change pas non plus. ⚠️ Et un **pays** qui change sa part
à midi ne change pas un devis déjà affiché. C'est ce qui a été vendu.

⚠️ **PAS DE MAJORATION DE ZONE, PAS DE PROMOTION** sur un devis partagé : la
remise **est** la promotion de ce mode. `surge_name`, `promo_title` sont
absents — ne réservez pas de place pour eux sur cet écran.

#### Quand ça ne marche pas — `dispatch_reason` décide de ce que vous proposez

La recherche s'arrête, la course **reste** `searching`, et **rien n'est changé
à sa place** : son mode et son prix sont ceux qu'elle a achetés.

| `dispatch_reason` | Ce qui s'est passé | Push | Ce que vous proposez |
|---|---|---|---|
| `pool_no_match` | personne ne partageait ce trajet | `ride_pool_no_match` | **Relancer** · **Prendre une course ordinaire** · Annuler |
| `pool_partner_left` | le co-passager a annulé | `ride_pool_partner_left` | **Relancer** · Annuler — dites que le prix ne change pas |
| `pool_call_failed` | le groupe était formé, l'appel n'a pas pu s'ouvrir | `ride_search_exhausted` | **Relancer** · Annuler |
| *(absent)* | aucun chauffeur n'a pris le groupe | `ride_search_exhausted` | **Relancer** · Annuler |

⚠️ **« PRENDRE UNE COURSE ORDINAIRE » EST UN AUTRE DEVIS, ET UN AUTRE PRIX.**
Annulez la course partagée (`POST /rides/{id}/cancel` — remboursée, personne ne
l'a prise), puis repassez par `POST /rides/quote` + `POST /rides`. **Il n'existe
pas de route qui change le mode d'une course** : elle ferait payer un tarif que
le passager n'a pas vu.

⚠️ **NE BASCULEZ JAMAIS D'OFFICE.** Au bout de cinq minutes, l'écran demande —
il ne décide pas. C'est la règle de tout ce document : le passager a payé, c'est
à lui de choisir.

```
POST /rides/{id}/relaunch
```

**La relance reprend là où ça s'est arrêté**, et vous n'avez rien à distinguer :
sans groupe, elle recherche un co-passager (`pooling`) ; avec un groupe intact,
elle rappelle une voiture (`calling`). La réponse dit lequel.

| Refus | Quand |
|---|---|
| `409 search_running` | `dispatch_state` vaut `pooling` **ou** `calling` — masquez « Relancer » dans les deux cas. ⚠️ Sinon un passager impatient remet sa fenêtre à zéro toutes les dix secondes et n'atteint jamais les cinq minutes, donc jamais un groupe |
| `409 pool_off` | le pays a fermé le partage entre-temps — proposez d'annuler et de commander une course ordinaire |

#### Ce que le devis refuse, et pourquoi le dire AVANT

| Refus | Ce que vous affichez |
|---|---|
| `409 pool_off` | ne montrez pas le mode du tout (lisez `settings.pool.enabled`) |
| `409 pool_too_short` | **la phrase du serveur, telle quelle** : « Ce trajet fait 2 100 m : la course partagée commence à 3 000 m » |
| `409 pool_direct_only` | « une course partagée va d'un point à un autre » — retirez les arrêts intermédiaires |
| `409 pool_no_class` | aucune voiture ne le propose ici pour le moment |

⚠️ **LES CHIFFRES SONT DANS LA PHRASE, PAS DANS UN `meta`.** L'enveloppe
d'erreur ne rend que `code`, `message`, `fields` et `reason` — c'est la règle de
toute la plateforme (§8), et `pool_too_short` ne fait pas exception : son
`message` est déjà traduit **et déjà chiffré**. Si vous voulez votre propre
mise en forme, **prenez les deux nombres de `settings.pool.min_distance_m` et du
`distance_m` du devis ordinaire** — ne tentez pas de lire un `meta` qui
n'arrive pas.

⚠️ **PROPOSEZ LE MODE SEULEMENT QUAND IL EST POSSIBLE.** `settings.pool` vous
donne tout pour le savoir avant d'appeler : `enabled`, et `min_distance_m` à
comparer au `distance_m` du devis ordinaire. Un bouton qu'on refuse est pire
qu'un bouton absent.

⚠️ **N'ÉCRIVEZ JAMAIS LE MINIMUM EN DUR.** Il valait 5 km jusqu'à la v4.44.0,
3 km depuis la v4.45.0, et **un pays peut régler autre chose** : « à partir de
3 km » codé dans l'application mentirait chez le voisin. Lisez
`min_distance_m`, et composez la phrase avec lui.

⚠️ **PAS D'ARRÊT INTERMÉDIAIRE.** Le partage n'accepte que **deux** étapes, un
départ et une arrivée : l'itinéraire du groupe en contient déjà quatre, et une
escale la ferait disparaître sans rien dire. `PATCH /rides/{id}/stops` reste
possible **après** l'acceptation, comme sur une course ordinaire.

#### Ce que vous montrez d'une course partagée

| Champ | Quand | Ce que vous en faites |
|---|---|---|
| `mode: "pool"` | toujours | un badge « partagée » — un passager doit savoir qu'un inconnu montera |
| `pool_until` | pendant `pooling` | le décompte de la recherche de co-passager |
| `pool_id` | dès le groupe formé | rien à afficher ; utile au support |
| `pool_size` | dès le groupe formé | « vous partagez avec 1 personne » |
| `share_pct` | toujours | « −30 % » (`100 − share_pct`) — au devis **et** sur le reçu |

⚠️ **VOUS NE RECEVEZ NI LE NOM NI L'ADRESSE DE L'AUTRE PASSAGER, et c'est
délibéré.** On ne donne pas l'adresse de quelqu'un à un inconnu avant qu'il ne
monte en voiture. Le chauffeur, lui, a l'itinéraire complet : c'est lui qui
conduit. Sur la carte, vous verrez donc la voiture faire un détour que vous ne
pouvez pas expliquer point par point — **dites-le d'avance** : « le chauffeur
prend d'abord l'autre passager ».

⚠️ **LE CHAUFFEUR PREND LES DEUX PASSAGERS AVANT D'EN DÉPOSER UN.** Si vous
montez en second, la voiture arrive chez vous après un premier arrêt ; si vous
montez en premier, vous attendez une prise en charge de plus. `eta_at` en tient
compte — **servez-le tel quel**, ne recalculez pas.

⚠️ **VOTRE COURSE RESTE LA VÔTRE.** Statut, prix, reçu, note, pourboire,
conversation : tout fonctionne exactement comme sur une course ordinaire. Le
partage est un **lien**, pas une fusion — ne fusionnez rien à l'écran.

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
GET /rides?before=2026-10-05T00:00:00Z   # bornée à ce qui est né AVANT (v4.43.0)
```

⏳ **`before` BORNE L'HISTORIQUE À UNE FENÊTRE DE DATE (v4.43.0)** — un instant
RFC 3339, **strictement** avant. Il se combine avec `cursor` sans se gêner :
l'un borne la fenêtre, l'autre dit où l'on en était dedans.

⚠️ **UNE DATE ILLISIBLE EST REFUSÉE** (`422`, `fields: ["before"]`), pas
ignorée : l'ignorer servirait silencieusement l'historique entier là où vous
demandiez une tranche, et une application qui pagine ne s'en apercevrait jamais.

Il existe pour **l'application cliente unifiée** (`DIRA-CLIENT.md`), qui mêle
les courses et les commandes semaine par semaine : sans borne de date, elle
repartait du haut à chaque page. Vous n'en avez pas besoin pour un historique
ordinaire — `cursor` suffit.

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
| `pooling` | course **partagée** uniquement (v4.42.0) : on cherche un **co-passager**, aucun chauffeur n'est encore appelé | « nous cherchons quelqu'un qui fait le même trajet » + le décompte de `pool_until` — ⚠️ **pas** « nous cherchons un chauffeur », ce serait faux |
| `exhausted` | la recherche s'est arrêtée **sans preneur** — la course n'est **pas** annulée | « aucun chauffeur disponible » + deux boutons : **Relancer** et **Annuler** |

`dispatch_at` date le dernier passage.

⚠️ **`dispatch_reason` (v4.42.0) DIT POURQUOI, quand il y a plusieurs façons de
s'arrêter** — et il change ce que vous proposez. Il n'apparaît que sur une
course partagée : `pool_no_match`, `pool_partner_left`, `pool_call_failed`.
Voir §4 quater. Sur une course ordinaire il est **absent** : il n'y a qu'une
façon d'y échouer. Le passager l'apprend par **push**
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
POST /rides/{id}/cancel   { "reason_code": "wait_too_long", "reason": "…" }   // les deux facultatifs
```

`409 invalid_transition` sur une course `in_transit` : le bouton doit
disparaître à ce statut, pas échouer.

### 🏷️ LE MOTIF, NOMMÉ — `reason_code` et `GET /rides/cancel-reasons` (v4.55.0)

Le motif d'annulation était un **texte libre**, et il ne se comptait pas :
« changé d'avis », « Changé d'avis », « chg avis », « il est pas venu » — quatre
façons d'écrire deux faits, et aucun moyen de répondre à « combien de
passagers posés par un chauffeur cette semaine ? ». Un champ qu'on ne peut pas
grouper n'existe que pour celui qui l'a tapé.

**Demandez la liste, n'inventez pas les codes :**

```
GET /rides/cancel-reasons
→ { "by": "rider", "items": [ { "code": "changed_mind", "grave": false }, … ] }
```

```jsonc
{ "by": "rider", "items": [
  { "code": "changed_mind",      "grave": false },   // je n'ai plus besoin de la course
  { "code": "wait_too_long",     "grave": false },   // l'attente est trop longue
  { "code": "driver_not_moving", "grave": false },   // il a accepté et ne vient pas
  { "code": "driver_asked",      "grave": false },   // le chauffeur m'a demandé d'annuler
  { "code": "wrong_address",     "grave": false },   // je me suis trompé de départ
  { "code": "price",             "grave": false },   // le prix ne me convient pas
  { "code": "other",             "grave": false } ] }
```

⚠️ **LA LISTE DÉPEND DU RÔLE DU JETON**, et le serveur **refuse** un code de
l'autre rôle. Avec votre jeton de client vous recevez les motifs du PASSAGER ; « le passager n'est pas venu » n'y est pas, et l'envoyer serait refusé. Les deux listes ne décrivent pas les mêmes faits. Appelez la route, mettez le résultat en cache pour la
session, et composez la liste depuis elle — un code écrit en dur dans
l'application sera refusé le jour où la taxonomie change.

⚠️ **TRADUISEZ LES CODES CHEZ VOUS.** La route sert les codes et leur gravité,
**pas des phrases** : à vous de les afficher dans la langue de l'utilisateur.
Un serveur qui renverrait du français vous obligerait à l'ignorer de toute
façon.

⚠️ **LE CODE RESTE FACULTATIF, ET LA PHRASE LIBRE RESTE À CÔTÉ.** Les deux
ensemble, parce qu'ils ne disent pas la même chose : le code **se compte**, la
phrase **explique le cas**. Une taxonomie ne couvre jamais tout, et forcer un
choix fait cocher le premier élément de la liste — ce qui est pire qu'un champ
vide, parce qu'on le croit. `other` existe pour ça.

⚠️ **UN CODE INVENTÉ EST REFUSÉ** — `422` avec `fields: ["reason_code"]` et la
liste `allowed`. N'envoyez rien plutôt qu'un code approximatif : une annulation
refusée pour un détail de formulaire laisse l'utilisateur coincé sur une course
dont il veut sortir.

⚠️ **ET LE MOTIF SE RELIT** : la course rend `cancelled_reason_code` à côté de
`cancelled_reason`. Les courses **annulées avant la 4.55.0** n'ont que la
phrase — ne traduisez pas son absence par `other`, et affichez-la telle quelle :
c'est leur seule explication.

⚠️ **`driver_asked` EST LE MOTIF QU'IL FAUT VRAIMENT PROPOSER.** « Le chauffeur
m'a demandé d'annuler » est un **signal de fraude** : un chauffeur qui fait
annuler son passager évite les frais d'annulation **et** garde sa place dans le
vivier. Sans ce choix, le geste est indistinguable d'un passager qui renonce, et
il ne se compte nulle part. Formulez-le en clair dans la liste.

⚠️ **AUCUN MOTIF N'EST `grave` CÔTÉ PASSAGER**, et `grave` vous est pourtant
servi : lisez-le plutôt que de supposer qu'il vaut toujours `false`. Un passager
qui signale une agression passe par le **support** (§ support), pas par une
annulation — c'est un échange, pas une case à cocher.

### 💸 CE QU'ANNULER COÛTE — à lire AVANT le bouton (v4.50.0)

Un barème d'annulation se règle par pays et par véhicule. Il voyage **avec la
course**, dans `cancellation` :

```jsonc
{ "cancellation": {
    "fee_xof": 800,        // ce qui serait retenu. 0 = gratuit
    "pct": 40,             // la part du tarif appliquée, pour l'expliquer
    "step": "waiting",     // free | on_the_way | waiting
    "why": "",             // la RAISON quand c'est gratuit
    "grace_left_s": 0 } }  // secondes restantes avant que ça devienne payant
```

⚠️ **RETENIR DE L'ARGENT À QUELQU'UN QUI N'A PAS PU LE LIRE AVANT EST
INDÉFENDABLE**, et c'est la réclamation qu'on ne peut pas gagner. L'écran de
confirmation doit dire le montant — ou dire que c'est gratuit, **et jusqu'à
quand**. Ne montrez jamais un bouton « Annuler » nu quand `fee_xof > 0`.

⚠️ **`grace_left_s` EST LE CHAMP QUI ÉVITE LA RÉCLAMATION.** Annoncer
« gratuit » sans dire jusqu'à quand fait découvrir les frais **après** le
geste. Rendez le compte à rebours **depuis ce nombre**, pas depuis l'horloge du
téléphone — et relisez-le à chaque rafraîchissement de la course plutôt que de
le décompter indéfiniment : le chauffeur peut arriver avant la fin de la grâce,
et la grâce tombe alors d'un coup.

⚠️ **AFFICHEZ `why` QUAND C'EST GRATUIT.** « C'est gratuit » sans dire pourquoi
laisse croire à une faveur, et la prochaine fois que ce sera payant, personne
ne comprendra ce qui a changé.

| `why` | Ce que l'écran dit |
|---|---|
| `no_driver_yet` | « Personne n'a encore pris votre course : l'annulation est gratuite. » |
| `within_grace` | « Gratuit pendant encore N secondes. » |
| `no_schedule` | rien de particulier — ce pays n'a pas de barème |
| `not_the_rider` | ne vous concerne pas (c'est le chauffeur qui annule) |

| `step` | Quand |
|---|---|
| `free` | rien à payer |
| `on_the_way` | un chauffeur roulait vers vous |
| `waiting` | il était **arrivé** et vous attendait |

⚠️ **COMMENT C'EST PERÇU, et il faut le dire à l'écran** : sur une course déjà
payée, le remboursement est **amputé** des frais (vous recevez le tarif moins
les frais, en un seul mouvement). Sur une course **en espèces**, personne n'a
rien versé : les frais deviennent une **dette** réglée sur votre prochaine
recharge. Un écran qui promettrait « remboursement intégral » dans le second
cas annoncerait un mouvement qui n'existe pas.

⚠️ **ET CE QUI EST RETENU VA AU CHAUFFEUR**, moins la commission. Le dire
change la façon dont les frais sont reçus : ce n'est pas une pénalité de la
plateforme, c'est un dédommagement pour quelqu'un qui s'est déplacé.



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

## 🧾 LE REÇU ET LE RELEVÉ, EN PDF (v4.52.0)

Deux documents téléchargeables, pensés pour être envoyés à un employeur, à un
comptable ou à une assurance.

```
GET /rides/{id}/receipt.pdf
GET /rides/statement.pdf?from=2026-09-01&to=2026-09-30
```

Les deux rendent `application/pdf` avec `Content-Disposition: attachment` et un
nom de fichier déjà daté — `dira-course-2026-10-08-718293.pdf`. **Ne les ouvrez
pas dans une WebView** : laissez le système les enregistrer ou les partager, c'est
ce que la personne veut en faire.

> ⚠️ **`Cache-Control: no-store`, ET CE N'EST PAS DÉCORATIF.** Un reçu porte un
> nom, un trajet et un montant. Ne le gardez pas dans un cache disque applicatif :
> sur un téléphone partagé, il se relit après la déconnexion. Téléchargez à la
> demande.

### Ce que le reçu dit de la distance — et pourquoi c'est le cœur du document

Le reçu imprime la **distance réellement parcourue**, celle mesurée sur le trajet
du chauffeur, et **nomme sa source** :

| `distance_source` | Ce que le document écrit |
|---|---|
| `tracked` | « mesurée sur le trajet réel » |
| `planned` (ou absent) | « estimée — le suivi n'a rien enregistré » |

> ⚠️ **LE SECOND CAS EST COURANT, PAS RARE** : il suffit que le téléphone du
> chauffeur ait perdu le réseau pendant la course. C'est pourquoi la source est
> écrite sur le document plutôt que sous-entendue — « 11,4 km » présenté comme
> mesuré alors qu'il vient d'une estimation est la phrase d'un reçu qu'on ne peut
> plus défendre devant une réclamation.

> ⚠️ **L'ESTIMATION DU DEVIS FIGURE À CÔTÉ**, jamais à la place : l'écart entre
> les deux est exactement ce qu'un litige examine (« il a fait un détour »).

> ⚠️ **ET L'APPROCHE EST SÉPARÉE.** Ce que le chauffeur a roulé pour venir est
> imprimé à part. Les additionner ferait lire « 10,3 km » pour une course de
> 8,2 km — avec un prix qui ne colle plus avec la distance affichée juste
> au-dessus.

### Ce que le reçu garantit

- **Le détail s'additionne jusqu'au prix payé.** Si ce n'est pas le cas, le
  serveur **ne sert rien** (500) plutôt qu'un document faux : un reçu dont les
  lignes ne tombent pas juste prouve une erreur, sous notre nom, devant le
  comptable du client.
- La **majoration** est une ligne d'information, sans montant : elle est déjà dans
  le tarif, et l'ajouter la compterait deux fois.
- **Une seule remise** par course, et le libellé dit laquelle — code ou promotion.
- Le **pourboire** est une note, pas une ligne : il n'est pas dans le prix de la
  course.
- Une **course annulée** imprime ce qui a été **retenu**, pas le tarif d'une
  course qui n'a pas eu lieu. Rien à payer ⇒ le document le dit en une phrase.
- La **monnaie** est celle du pays de la course, et l'**heure** celle de son
  fuseau.

### Le relevé d'une période

Une ligne par course — date, trajet, distance réellement parcourue, montant —
puis le nombre de courses, la distance totale et la somme.

> ⚠️ **`from` ET `to` SONT OBLIGATOIRES, SANS DÉFAUT.** Un défaut implicite
> produirait un document dont la période n'est pas celle qu'on croyait demander —
> et c'est un document qu'on additionne. Format `2026-09-01` ou RFC 3339. Une date
> illisible est **refusée** (`422`), pas ignorée.

> ⚠️ **IL REFUSE AU LIEU DE TRONQUER.** Au delà de **500 courses** ou d'une
> fenêtre de **366 jours** : `409 statement_too_large`, avec `meta.max_rows`.
> Proposez de **découper la période** — c'est la seule suite utile. Un relevé qui
> s'arrêterait en silence à la cinq-centième ligne produirait une somme fausse,
> présentée comme vraie.

> ⚠️ **SI LA PÉRIODE COUVRE PLUSIEURS PAYS**, le total mêle des monnaies et le
> document le dit. Il reste servi : le retirer priverait du relevé quelqu'un qui
> n'a roulé qu'une fois à l'étranger.

### Où mettre le bouton

- Sur une course terminée ou annulée : « Reçu » dans l'écran de détail. Pas sur
  une course en cours — il n'y a pas encore de prix définitif.
- Dans l'historique : « Exporter la période », avec deux dates à choisir.

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

### 📴 QUAND C'EST LE CHAUFFEUR QUI N'A PLUS DE RÉSEAU (v4.32.0)

**Votre socket va très bien, et la voiture ne bouge plus.** Ce n'est pas la
même panne, et cela ne se traite pas de la même façon.

⚠️ **NE DITES JAMAIS QUE LA COURSE EST TERMINÉE, ANNULÉE OU PERDUE.** Elle a
lieu : le chauffeur conduit, son téléphone garde tout, et il rendra le récit en
retrouvant le réseau. Une application qui annonce « course introuvable » parce
qu'une position manque fait descendre un passager d'une voiture qui l'emmenait
au bon endroit.

| Ce que vous observez | Ce que vous affichez |
|---|---|
| Aucune position depuis **> 1 min**, course vivante | « position en attente » sur le marqueur, **sans le déplacer** |
| Aucune position depuis **> 3 min** | « nous ne voyons pas la voiture en ce moment — la course continue » |
| L'état change (`status`) mais les positions manquent | l'état fait foi : **relisez `GET /rides/{id}`** |

- ⚠️ **NE FAITES PAS GLISSER LE MARQUEUR VERS UNE POSITION VIEILLE.** Le
  serveur ne rediffuse **pas** les positions rattrapées : il ne vous enverra
  jamais le trajet manqué, justement pour que la voiture ne recule pas sur
  votre carte. Après une coupure, la voiture **saute** à sa vraie position —
  et c'est la vérité de la route, pas un défaut.
- Le **temps d'arrivée estimé** vieillit pendant une coupure : montrez-le
  comme une estimation, pas comme un compte à rebours exact.
- ⚠️ **Le bouton d'appel téléphonique reste la sortie de secours.** C'est le
  seul canal qui ne dépend pas de nos serveurs.

**À la fin**, une course faite en partie hors ligne peut porter un prix qui
n'est pas celui que le chauffeur vous a annoncé — il calculait avec la grille
gardée dans son téléphone. **C'est le montant de la course qui fait foi**, et
le support tranche les écarts.

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
| `ride_pool_matched` | course **partagée** : on a trouvé un co-passager, on cherche maintenant une voiture (§4 quater, v4.42.0) — ⚠️ seulement pour celui qui **attendait** | `status: searching`, `dispatch_state: calling`, `pool_id` |
| `ride_pool_no_match` | personne ne partageait ce trajet — relancer **ou** prendre une course ordinaire (v4.42.0) | `status: searching`, `dispatch_state: exhausted`, `dispatch_reason: pool_no_match` |
| `ride_pool_partner_left` | le co-passager a annulé — relancer, le prix ne change pas (v4.42.0) | `status: searching`, `dispatch_state: exhausted`, `dispatch_reason: pool_partner_left` |
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

## 🔒 CE QUE VOUS AVEZ LE DROIT D'AFFICHER DE L'AUTRE PARTIE (v4.49.0)

**C'est l'exploitation qui décide, pays par pays et métier par métier** —
Paramètres › Confidentialité de la console. Vous n'avez rien à arbitrer, et
surtout rien à deviner : **le serveur n'envoie pas ce qu'on n'a pas le droit de
montrer.**

`GET /rides/{id}` → `driver` porte ce qui est ouvert de votre chauffeur :

```jsonc
{ "driver": {
    "id": "…", "rides_count": 1240,
    "name": "Kofi A.",             // déjà masqué au niveau choisi, ou absent
    "rating_avg": 4.8, "rating_count": 530,  // absents si la note est fermée
    "vehicle": { "description": "Toyota Avensis rouge", "license_plate": "TG-4417", … },
    "contact": {                   // ⚠️ ABSENT quand rien n'est ouvert — le défaut en course
      "first_name": "…", "last_name": "…", "avatar_url": "…", "gender": "…",
      "phone": "+228…",            // ⚠️ présent dès qu'il est AFFICHABLE **ou** COMPOSABLE
      "show_phone": false, "direct_call": true, "in_app_alert": false } } }
```

⚠️ **`contact` ABSENT EST LE CAS NORMAL EN COURSE**, et ce n'est pas une panne :
par défaut, aucun numéro ne s'échange — la conversation est le canal. Ne
construisez pas un écran qui suppose sa présence.

⚠️ **LE VÉHICULE N'EST JAMAIS FERMÉ**, et c'est voulu : une plaque, une marque et
une couleur ne nomment personne, et c'est ce que vous guettez au trottoir. C'est
ce qui reste quand tout le reste est fermé — y compris si le serveur n'a pas pu
lire la politique, où vous ne recevrez QUE la voiture.

⚠️ **NE PROMETTEZ PAS CE QUE VOUS N'AVEZ PAS.** Si `direct_call` est faux, pas de
bouton « Appeler le chauffeur » grisé ni d'écran qui le mentionne : un bouton
qui n'existe pas se cache, il ne se désactive pas.

⚠️ **UN CHAMP ABSENT N'EST PAS UNE PANNE.** C'est un champ que l'exploitation
n'a pas ouvert dans ce pays. Affichez votre propre libellé — « votre chauffeur » —
comme pour toute donnée manquante, et **ne réessayez pas** : rien ne viendra.

⚠️ **N'ÉCRIVEZ AUCUNE RÈGLE EN DUR.** Pas de « si le pays est le Togo », pas de
« si c'est une course alors pas de téléphone ». Ces décisions changent sans
redéploiement de votre application, et une règle recopiée chez vous
contredirait celle du serveur sans que personne ne sache laquelle croire.

### ⚠️ TROIS PERMISSIONS DE CONTACT, ET CE NE SONT PAS LES MÊMES

| Champ | Ce qu'il autorise |
|---|---|
| `show_phone` | **afficher** le numéro à l'écran |
| `direct_call` | proposer un **bouton d'appel** |
| `in_app_alert` | faire **sonner** l'application de l'autre |

⚠️ **`phone` PRÉSENT NE VEUT PAS DIRE « AFFICHABLE ».** Le numéro arrive dès
qu'il peut être affiché **ou composé** — un bouton d'appel a besoin du numéro
pour le composer. **Lisez `show_phone` avant de l'écrire à l'écran.** Un
`phone` affiché parce qu'il était là annulerait le réglage dans l'écran même
qui le lit, et c'est l'erreur que ce paragraphe existe pour empêcher.

⚠️ **`direct_call: true` AVEC `show_phone: false`** est le cas le plus
courant : un bouton « Appeler », et le numéro **nulle part** — ni à l'écran, ni
dans un champ copiable, ni dans une capture d'écran. Composez-le sans le
rendre. Nous savons qu'il reste dans le journal d'appels du téléphone : c'est
de la friction, pas du secret, et c'est assumé côté serveur. Ne compensez pas
en l'affichant « puisqu'il est de toute façon visible ».

⚠️ **TOUT FERMÉ N'EST PAS UNE IMPASSE** : la **conversation** reste le canal, et
elle existe précisément pour que ces deux-là se parlent sans rien s'échanger.
Un écran sans nom ni numéro doit mener à elle, pas à un cul-de-sac.

### Les quatre niveaux de `name`

Le nom arrive **déjà masqué** — vous n'avez rien à couper. `full` (« Awa
Diallo »), `first` (« Awa »), `initials` (« A. D. »), ou **absent**.

⚠️ **NE RECONSTRUISEZ JAMAIS UN NOM** à partir de `first_name` et `last_name` :
ces deux champs ont leurs propres interrupteurs, et les concaténer rendrait un
nom complet là où l'exploitation n'a ouvert que des initiales.

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

## 🆘 LE BOUTON D'ALERTE — SOS (v4.56.0)

Un bouton, dans **toutes** les applications. Il ne se coupe pas, il ne se règle
pas, il n'a pas de condition : ce qui se règle, c'est ce qui le propose **sans
qu'on le touche**.

```
GET  /sos/settings          → ce que ce pays décide (détections, délai, numéros)
POST /sos                   → DÉCLENCHER
GET  /sos/me                → mon alerte en cours (200 avec `alert: null` s'il n'y en a pas)
POST /sos/{id}/position     → où je suis MAINTENANT
POST /sos/{id}/cancel       → « fausse alerte »
```

> ⚠️ **AU SOCLE** (`…/api/v1/sos`, **sans** `/vtc` ni `/food`). Le même bouton
> pour un passager de course, un client de livraison, un chauffeur et un
> livreur : l'exploitation doit les voir dans la même file.

### ⚠️ LA RÈGLE QUI GOUVERNE TOUT CE QUI SUIT

**Une alerte ne se perd jamais.** `POST /sos` n'a **aucun champ obligatoire** —
pas même la position.

```jsonc
POST /sos
{ "source": "button",        // button | shake | crash | voice. Absent = button
  "confirmed": true,         // une DÉTECTION validée par la personne
  "lat": 6.1319, "lng": 1.2255, "accuracy_m": 18,
  "battery": 7,              // le pourcentage de batterie
  "vertical": "vtc",         // vtc | food, quand il y a une opération
  "ride_id": "…",            // ou delivery_id
  "note": "un homme me suit" }
→ 201  { "id": "…", "status": "open", "trigger": "bouton pressé", … }
```

⚠️ **ENVOYEZ CE QUE VOUS AVEZ, TOUT DE SUITE — N'ATTENDEZ RIEN.** C'est la
consigne la plus importante de cette section : n'attendez **pas** un point GPS
pour envoyer. Le serveur accepte une alerte **sans position**, et « on ne sait
pas où il est » est une alarme qu'un opérateur traite en premier — pas une
requête à compléter. Attendre un fix de dix secondes au fond d'un parking
souterrain, c'est perdre les dix secondes qui comptent. Envoyez d'abord,
**poussez la position ensuite** (`POST /sos/{id}/position`).

⚠️ **ET RÉESSAYEZ, EN BOUCLE, JUSQU'À UN `2xx`.** Le seul échec possible de
cette route est un serveur ou un réseau en panne. Mettez l'appel dans la file
que vous avez déjà pour le hors-ligne, mais **en tête** et sans attendre la
fenêtre de synchronisation : une alerte remise trois minutes plus tard ne sert
plus à personne.

⚠️ **N'AJOUTEZ AUCUNE VALIDATION DE VOTRE CÔTÉ.** Pas de « position
obligatoire », pas de « choisissez un motif », pas d'écran de confirmation à
deux champs. Tout ce qui est bancal est **corrigé par le serveur**, jamais
rejeté : source inconnue → `button`, coordonnées impossibles → position
ignorée (l'alerte reste), note trop longue → coupée proprement.

### 📍 LE DOUBLE APPUI N'EST PAS UNE ERREUR

Quelqu'un qui panique appuie cinq fois. Le serveur rend **la même alerte** et y
ajoute la position — une seule alerte ouverte par personne.

⚠️ **NE DÉSACTIVEZ DONC PAS LE BOUTON** après le premier appui, et n'affichez
pas « déjà envoyé » comme une erreur. Laissez-le actif : appuyer encore
**améliore** l'alerte (une position de plus) et rassure. Ce que vous devez
montrer, c'est que c'est **parti** — pas que c'est interdit.

### 🧭 POUSSEZ LA POSITION PENDANT TOUTE L'ALERTE

```
POST /sos/{id}/position   { "lat": …, "lng": …, "accuracy_m": … }   → 204
```

⚠️ **L'OPÉRATEUR A BESOIN DE SAVOIR OÙ LA PERSONNE EST, PAS OÙ ELLE A APPUYÉ.**
Un véhicule continue de rouler. Poussez toutes les **5 à 10 secondes** tant que
l'alerte est vivante, plus souvent que votre cadence habituelle : c'est le seul
moment du produit où la fraîcheur d'une position vaut la batterie qu'elle coûte.

Le serveur ignore silencieusement un point envoyé après la fermeture (`204`) :
**ne traitez pas cela comme une erreur** et n'arrêtez pas votre boucle sur un
refus — relisez `GET /sos/me` pour savoir si c'est fini.

### 🔁 RETROUVEZ L'ÉCRAN APRÈS UN REDÉMARRAGE — `GET /sos/me`

```
GET /sos/me → { "alert": { … } }   ou   { "alert": null }
```

⚠️ **APPELEZ-LA AU DÉMARRAGE, TOUJOURS.** Un téléphone qui redémarre après un
choc, une application tuée par le système, un réseau qui revient : sans cela, la
personne ne sait plus si son alerte est partie, et elle appuie encore — ou, bien
pire, elle croit avoir appelé alors que non.

⚠️ `200` **avec `alert: null`**, et non `404` : « je n'ai pas d'alerte en
cours » est le cas NORMAL.

### 🛑 ANNULER — et ce que ça ne fait pas

```
POST /sos/{id}/cancel → l'alerte, refermée
```

⚠️ **L'ALERTE N'EST PAS SUPPRIMÉE, ET L'EXPLOITATION LA VOIT ENCORE PENDANT
QUINZE MINUTES.** Dites-le à la personne : « l'exploitation a été prévenue et
vous rappellera peut-être pour vérifier ». Une annulation peut être
**contrainte** — c'est le scénario même que ce bouton existe pour couvrir —, et
promettre que « tout est effacé » serait un mensonge.

⚠️ **PAS DE DEUXIÈME CONFIRMATION POUR ANNULER.** Un appui, c'est annulé. Un
« êtes-vous sûr ? » à ce moment-là fait rester une fausse alerte dans la file de
l'exploitation, qui appelle pour rien, et la prochaine vraie sera prise moins au
sérieux.

### ⚙️ CE QUE LE PAYS DÉCIDE — `GET /sos/settings`

```jsonc
{ "button": true,            // TOUJOURS true
  "shake": true,             // la secousse propose l'alerte
  "crash": true,             // la détection de choc la propose
  "voice": false,            // le mot-clé vocal — ÉTEINT par défaut
  "countdown_seconds": 10,   // le délai d'annulation d'une DÉTECTION
  "calls_back": true }       // ⚠️ CE QUE VOUS DEVEZ PROMETTRE : on vous rappelle
```

⚠️ **RELISEZ-LA À CHAQUE OUVERTURE**, et n'écrivez aucune de ces valeurs en
dur : elles changent depuis la console, sans redéploiement, et un pays peut
couper une détection du jour au lendemain.

### ⚠️⚠️ IL N'Y A AUCUN NUMÉRO À COMPOSER, ET CE N'EST PAS UN OUBLI (v4.58.0)

**Le téléphone de la personne en danger ne compose rien.** L'alerte part au
**SERVICE CLIENT** : un opérateur l'appelle, et c'est **lui** qui appelle les
secours s'il le faut.

Cette route **ne sert donc aucun numéro**, et il n'y a pas de champ `numbers` à
lire. **N'en inventez aucun** — pas de 112, pas de numéro codé dans
l'application, pas de repli « au cas où », pas de liste tirée d'un site. Un
écran qui proposerait « appeler la police » enverrait quelqu'un composer un
numéro que nous ne lui avons pas donné.

⚠️ **CE QUE VOUS AFFICHEZ À LA PLACE** : « **Le service client a été prévenu et
va vous appeler.** » C'est `calls_back`, et c'est la seule chose que la personne
cherche à savoir après avoir appuyé.

⚠️ **POURQUOI C'EST MIEUX, ET PAS SEULEMENT DIFFÉRENT** — dites-le à votre
équipe produit, parce que la question viendra :
>
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

### 🫨 LES DÉTECTIONS — ⚠️ ELLES PROPOSENT, ELLES N'ENVOIENT PAS

C'est la règle la plus importante de cette partie, et elle vaut pour les trois
(`shake`, `crash`, `voice`).

**Une détection ouvre un compte à rebours de `countdown_seconds`, visible et
sonore, avec UN bouton « Annuler ». À l'expiration, l'alerte part** avec
`confirmed: false`.

```
secousse / choc / mot-clé  →  écran plein, compte à rebours, vibration + son
                           →  « Annuler »   : rien ne part
                           →  « Envoyer »   : POST /sos  { confirmed: true }
                           →  rien du tout  : POST /sos  { confirmed: false }
```

⚠️ **POURQUOI UN COMPTE À REBOURS, ET NON UN ENVOI IMMÉDIAT.** Un dos-d'âne,
un téléphone qui tombe, un sac qu'on pose : l'envoi direct remplirait la file de
faux, l'opérateur apprendrait à les ignorer, et la vraie alerte se noierait
dedans. **Un faux positif traité comme une vraie alerte coûte plus cher qu'un
faux positif annulé.**

⚠️ **POURQUOI IL PART QUAND PERSONNE N'ANNULE.** C'est tout l'intérêt : après
un choc violent, **personne n'annule parce que personne ne peut**. Un compte à
rebours qui s'arrêterait sans rien envoyer serait un bouton de plus, pas une
détection.

⚠️ **`confirmed: false` EST PLUS GRAVE, PAS MOINS — et dites-le-vous bien, parce
que l'intuition dit l'inverse.** Le serveur le traite comme tel (`grave: true`,
et `trigger` vaut `"choc détecté, PERSONNE N'A ANNULÉ"`). Ne le présentez donc
jamais à la personne comme « envoyé par erreur », et ne le rangez pas plus bas
dans vos écrans.

⚠️ **PENDANT LE COMPTE À REBOURS, PRÉPAREZ TOUT.** Démarrez l'acquisition GPS,
lisez la batterie, composez le corps de la requête : à l'expiration, l'envoi
doit partir en une milliseconde. Un compte à rebours qui finit sur « recherche
du GPS… » a gaspillé dix secondes.

⚠️ **UN SEUL BOUTON PENDANT LE COMPTE À REBOURS : ANNULER.** Pas de champ de
texte obligatoire, pas de choix de motif, pas de liste de contacts. La note est
facultative, et elle s'ajoute **après** l'envoi si la personne en a le temps.

#### Les trois, et ce que chacune coûte

| Détection | Ce qu'elle écoute | Ce qu'elle coûte |
|---|---|---|
| `shake` | l'accéléromètre déjà allumé | rien — aucune permission, aucune batterie en plus |
| `crash` | un pic d'accélération | rien de plus que `shake` |
| `voice` | **le micro, en permanence** | une permission, de la batterie, et une surveillance |

⚠️ **LA SECOUSSE EST LA PLUS UTILE DU LOT, et pas la plus gadget.** Elle existe
pour le cas où **on ne peut pas regarder l'écran** : téléphone en poche, main
sur le volant, quelqu'un à côté qui ne doit pas voir. C'est précisément quand
viser un bouton est impossible qu'on en a besoin. Réglez le seuil pour qu'une
marche rapide ou un nid-de-poule ne suffise pas : trois secousses franches,
pas un mouvement.

⚠️ **LE VOCAL NE S'ALLUME PAS EN SILENCE.** `voice: false` par défaut, et quand
un pays l'active : demandez la permission du micro **en expliquant**, montrez un
indicateur visible quand l'écoute tourne, et offrez de la couper dans vos
réglages. Une application qui écoute sans le dire est un problème plus grave que
celui qu'elle résout. La reconnaissance se fait **sur l'appareil** : n'envoyez
aucun flux audio à nos serveurs — aucune route ne l'accepte.

### 🖥️ CE QUE L'ÉCRAN D'ALERTE DOIT MONTRER

Pendant qu'une alerte est vivante :

> - **« Le service client a été prévenu et va vous appeler »** — la seule chose
>   que la personne cherche à savoir. Dites-le dès le `201`, pas après un
>   aller-retour.
> - **Annuler**, en un appui.
> - **De quoi ajouter une note**, facultatif et secondaire — et elle sert
>   vraiment : l'opérateur la lit avant d'appeler, et « un homme me suit » change
>   ce qu'il dit au téléphone.
> - ⚠️ **AUCUN BOUTON D'APPEL**, ni vers les secours, ni vers nous. C'est
>   l'exploitation qui appelle ; un bouton « appeler Dira » ferait patienter
>   quelqu'un en danger dans une file d'attente téléphonique pendant qu'un
>   opérateur essaie de le joindre sur la même ligne.
>
> ⚠️ **ET RIEN D'AUTRE.** Pas de menu, pas de navigation, pas de retour
> accidentel vers la course. Cet écran se tient devant quelqu'un dont les mains
> tremblent.

⚠️ **PAS DE VERROU D'APPLICATION SUR CET ÉCRAN** (`app_lock`, v4.46.0) : un
code secret entre quelqu'un et son bouton d'alerte serait indéfendable. La règle
est déjà écrite dans la section du verrou — elle vaut ici littéralement.

⚠️ **LE BOUTON SOS RESTE ATTEIGNABLE EN DEUX APPUIS MAXIMUM**, depuis n'importe
quel écran d'une opération en cours. Enterré dans un menu « Aide › Sécurité », il
n'existe pas.

---

> ⚠️ **LE PASSAGER EST LA PARTIE LA PLUS EXPOSÉE D'UNE COURSE**, et c'est la
> raison pour laquelle ce bouton n'est pas réservé aux chauffeurs : il est seul,
> dans le véhicule de quelqu'un d'autre, sur un trajet qu'il ne choisit pas.
> Le bouton doit être atteignable **pendant toute la course**, depuis l'écran de
> suivi, en deux appuis au plus.
>
> ⚠️ **NE LE CONFONDEZ PAS AVEC LE SUPPORT** (ci-dessous). Le support est une
> conversation qu'on relit le lendemain ; le SOS est une alarme qu'un opérateur
> prend dans la minute. Un passager qui se trompe de porte parce que les deux se
> ressemblent perd exactement le temps qu'il n'a pas.
>
> ⚠️ **ET LE SOS N'ANNULE PAS LA COURSE.** Ce sont deux gestes : on peut
> déclencher l'alerte et rester dans le véhicule — c'est même le cas le plus
> fréquent. N'enchaînez pas automatiquement sur une annulation.

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

### 🆕 S'INSCRIRE ET SE CONNECTER PAR CODE — le téléphone, et rien d'autre (v4.46.0)

Deux routes, un écran de six chiffres, **pas de mot de passe à choisir ni à
retrouver** :

```
POST /auth/otp          { phone, channel?: "whatsapp"|"sms", app: "client", locale? }
   → 200 { sent, channel, expires_at, resend_after, dev_code? }

POST /auth/otp/verify   { phone, code, name?, first_name?, last_name?,
                          app: "client", device_id?, device_name? }
   → 201 { …, created: true }   le compte VIENT DE NAÎTRE
   → 200 { …, created: false }  connexion
```

Le corps de la réponse de `verify` est **exactement celui de
`POST /auth/login`** (`user`, `access_token`, `refresh_token`, `session?`,
`maps?`), augmenté de `created`.

⚠️ **LA PASSERELLE N'EST PAS ENCORE CÂBLÉE — LE CODE REVIENT DANS LA RÉPONSE.**
Tant que le serveur répond `channel: "echo"`, **rien n'est envoyé** : le code
est dans **`dev_code`**. C'est ce qui vous permet de câbler l'écran dès
maintenant. ⚠️ **`dev_code` disparaîtra sans préavis** le jour où WhatsApp ou
le SMS sera branché : traitez-le comme un bonus de développement — pré-remplir
le champ si vous voulez —, **jamais** comme la source du code. L'écran doit
fonctionner à l'identique quand il n'y est plus.

⚠️ **LE COMPTE À REBOURS SE REND DEPUIS `expires_at`**, jamais depuis l'horloge
du téléphone, et le bouton « renvoyer » se rouvre après `resend_after`
secondes. Un code vit **5 minutes** et meurt après **5 essais**.

⚠️ **NE DEMANDEZ LE NOM QU'APRÈS LA VÉRIFICATION, ET SEULEMENT SI
`created: true`.** La demande de code **ne dit jamais** si le numéro est déjà
client — ce serait un annuaire : qui veut savoir si quelqu'un utilise Dira
n'aurait qu'à poster son numéro. Sans nom, le serveur affiche le **numéro**
comme nom ; la personne le changera depuis son profil (`PATCH /me`).

⚠️ **CE COMPTE N'A PAS DE MOT DE PASSE.** `POST /auth/login` lui répondra
toujours `401 invalid_credentials` : ne proposez pas « se connecter avec un mot
de passe » à quelqu'un qui s'est inscrit par code, et ne gardez pas d'écran de
mot de passe oublié pour lui.

⚠️ **RÉSERVÉE AUX CLIENTS.** Un compte chauffeur, livreur, marchand ou de
direction reçoit `403 otp_not_available` et **aucun code** — six chiffres ne
remplacent un mot de passe que pour un client. Les applications d'agent gardent
`POST /auth/login`.

| Refus | Quand | Ce que l'écran fait |
|---|---|---|
| `422 validation_failed` (`fields: ["phone"]`) | le `+` manque | souligner le champ ; pré-remplir l'indicatif du pays |
| `429 otp_too_soon` | un code vient de partir | garder le bouton « renvoyer » grisé jusqu'à `resend_after` |
| `429 otp_too_many_requests` | plafond horaire de ce numéro | proposer le support, pas un nouvel essai |
| `503 otp_delivery_failed` | la passerelle n'a rien pris | **redemander tout de suite est légitime** : rien n'a été consommé |
| `401 otp_invalid` | code faux **ou** aucun code en cours | « code incorrect », garder la saisie |
| `401 otp_expired` | plus de 5 minutes | « demandez un nouveau code » |
| `401 otp_too_many_attempts` | 5 essais | le code est **mort** : revenir à l'écran du numéro |
| `403 otp_not_available` | compte qui se connecte par mot de passe | « ouvrez l'application correspondante » |
| `403 account_suspended` | compte suspendu | le dire tel quel, ne pas renvoyer vers « mot de passe oublié » |

⚠️ **DITES QUELLE APPLICATION SE CONNECTE — `app`.** À la connexion, envoyez
`app: "client"`. Le serveur refuse `403 wrong_app` quand le compte n'a pas le
droit d'entrer ici, et **`error.reason` nomme l'application à ouvrir**
(`client` · `driver` · `courier` · `merchant` · `console`). Sans ce mot, un
chauffeur se connectait dans l'application cliente, recevait un jeton
parfaitement valide, puis voyait chaque écran répondre `403` — et croyait
l'application cassée.

⚠️ **UN SEUL CARNET D'ADRESSES, ET C'EST VOULU.** « Maison » sert au repas
comme au taxi. N'entretenez pas deux listes.

⚠️ **UN SEUL SOLDE, DEUX MÉTIERS QUI LE DÉBITENT.** Un écran de solde qui ne
montrerait que les opérations d'un métier laisserait un client chercher où sont
passés ses 2 000 F. `GET /wallet/transactions` les rend toutes, des deux
métiers.

⚠️ **NE CONCLUEZ JAMAIS UN PAIEMENT SUR LA SEULE RÉPONSE HTTP.** Suivez
`GET /payments/{id}` jusqu'à `succeeded`, ou attendez le passage de l'opération
à son statut payé. Un écran qui annonce « payé » sur un `202` fait réclamer un
remboursement pour un paiement qui n'a jamais abouti.

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
| `error.reason` | **l'application à ouvrir** : `client` · `driver` (courses) · `courier` (livraison, **v4.37.0**) · `merchant` · `console` |
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

**Ce que la règle sépare, et ce qu'elle ne sépare pas — v4.37.0.**
`client` vaut pour la course **et** la livraison : deux applications de
client, un seul compte, et elles ne se distinguent pas l'une de l'autre à la
connexion. En revanche, les deux applications d'**agent** sont désormais
DEUX mots : `driver` pour le chauffeur des courses, **`courier` pour le
livreur** — elles envoyaient toutes les deux `driver` jusqu'à cette version.
Un compte d'agent appartient à l'une ou à l'autre, et le socle lui refuse
l'entrée dans celle qui n'est pas la sienne.

⚠️ **Pourquoi cela vous concerne, même ici.** Les valeurs de `error.reason`
ne sont plus quatre mais cinq, et `courier` en fait partie : un `switch` qui
ne connaît que `client`, `driver`, `merchant` et `console` retombera sur son
cas par défaut — c'est-à-dire, presque toujours, sur « erreur de connexion »,
exactement le message que cette règle existe pour éviter. **Prévoyez un
libellé pour `courier`** (« ouvrez l'application Dira Livreur »), et une
phrase générique de repli pour une valeur que vous ne connaîtriez pas.

La famille `client`, elle, reste tenue à l'écart des autres, comme avant.

**Un compte de DIRECTION entre partout.** C'est voulu, pas un trou :
l'exploitation ouvre votre application pour reproduire ce qu'un utilisateur
décrit au support.

**`app` omis ne vérifie rien.** Une version d'application pas encore mise à
jour continue donc de fonctionner exactement comme avant — mais le mauvais
compte y entre aussi comme avant. C'est la raison d'envoyer le champ dès
cette version.

#### 📱 VOUS N'ÊTES PAS BORNÉ À UN SEUL APPAREIL (v4.35.0)

La v4.35.0 introduit un champ `device_id` à la connexion, et une règle **« un
seul appareil à la fois »** — **elle ne concerne QUE les chauffeurs et les
livreurs**, jamais un passager.

⚠️ **N'ENVOYEZ PAS `device_id`, ET NE CODEZ RIEN POUR CETTE RÈGLE.** Un passager a le droit d'être connecté sur sa tablette **et** son téléphone, et de suivre sa course depuis les deux.
Le champ est ignoré pour votre public, le jeton émis ne nomme aucun appareil, et
**aucune** de vos sessions ne peut être fermée par une connexion ailleurs.

Concrètement :

- Vous **ne recevrez jamais** `401 session_superseded`, ni sur
  `POST /auth/refresh`, ni sur un appel authentifié : inutile de le traiter.
- Se connecter sur un second appareil **n'invalide pas** le premier : les deux jetons de rafraîchissement continuent de vivre côte à côte, chacun avec sa propre rotation.
- La réponse de `POST /auth/login` ne porte **pas** d'objet `session` pour vous.

**Pourquoi c'est écrit ici alors que ça ne vous concerne pas.** La règle existe parce que deux
téléphones connectés sur un compte de chauffeur poussent **deux flux de
positions pour un seul véhicule**, et que le vivier d'appel le lit comme deux
véhicules. Rien de tel chez un passager, qui ne pousse aucune position — et l'appliquer « par prudence » à un public qui
n'a pas ce problème coûterait des déconnexions quotidiennes pour rien.


---

## 🎁 INVITER UN AMI — le parrainage (v4.51.0)

```
GET /me/referral
→ { "enabled": true, "code": "AWA7K2M",
    "invitee_xof": 1000, "sponsor_xof": 500, "max_sponsored": 10,
    "ends_at": "2027-10-08T00:00:00Z", "uses": 3 }
```

Un seul appel, et il dit tout ce que l'écran doit afficher : le code à partager,
ce que gagne celui qui arrive, ce que gagne celui qui invite, et combien de
personnes ont déjà utilisé ce code.

> ⚠️ **`enabled` EST EXPLICITE — NE LE DÉDUISEZ PAS DE L'ABSENCE DE CODE.** Le
> parrainage se règle **pays par pays**, et il est **éteint par défaut** : tant
> qu'une direction ne l'a pas décidé et budgété, il ne distribue rien. Quand
> `enabled` est faux, il n'y a **pas** de code — et il faut **cacher l'entrée**
> « inviter un ami », pas afficher un écran vide ni un bouton qui ne copie rien.

```json
{ "enabled": false, "invitee_xof": 0, "sponsor_xof": 0, "max_sponsored": 0 }
```

> ⚠️ **LE CODE EST TIRÉ À LA DEMANDE**, au premier appel de cette route. Il ne
> change plus ensuite : c'est le même code toute l'année, celui qu'on peut
> imprimer, dicter au téléphone ou coller dans un statut. Appelez cette route
> quand l'écran s'ouvre, et gardez le résultat.

> ⚠️ **AFFICHEZ LES DEUX MONTANTS, ET DITES QUI REÇOIT QUOI.** `invitee_xof` est
> une **remise** sur la première opération du filleul ; `sponsor_xof` est un
> **crédit** sur le solde du parrain. Les confondre — « gagnez 1 500 F » — fait
> attendre 1 500 F à tout le monde, et produit deux déceptions au lieu d'un
> parrainage.

> ⚠️ **LE PARRAIN N'EST PAYÉ QUE QUAND LA REMISE EST VRAIMENT CONSOMMÉE** — la
> course terminée, la commande livrée. Jamais à la saisie du code. Écrivez-le sur
> l'écran : sans cela, quelqu'un qui voit son filleul commander et son solde ne
> pas bouger pensera que le parrainage ne marche pas. La bonne phrase est
> **« crédité quand votre filleul aura terminé sa première course »**.

> ⚠️ **`max_sponsored` EST UN PLAFOND, ET `uses` DIT OÙ ON EN EST.** À
> `max_sponsored` atteint, le code cesse de valoir : montrez
> « 3 / 10 » plutôt qu'un compteur nu, sinon le dixième filleul découvrira un
> refus que rien n'annonçait. Quand `max_sponsored` vaut `0`, il n'y a pas de
> plafond — n'affichez alors aucun rapport.

**Le code de parrainage est un code promo comme un autre** : le filleul le saisit
dans le même champ que n'importe quel autre code, et les mêmes refus s'appliquent
— avec un de plus, pour celui qui essaie le sien.

---

## 🗑️ SUPPRIMER SON COMPTE — et ce qui reste des courses passées (v4.47.0)

```
DELETE /me
{ "password": "…" }      ← compte avec mot de passe
{ "code": "483920" }     ← compte né par code (demandez-en un par POST /auth/otp)
   → 200 { "status": "closed", "erase_at": "2026-11-06T18:40:00Z" }
```

C'est un **droit**, et l'écran qui le porte doit être trouvable depuis le
profil — pas enterré derrière le support.

**EN DEUX TEMPS, ET IL FAUT DIRE LES DEUX.**

1. **Tout de suite** — le compte est **fermé**. Toutes les sessions tombent, sur
   **tous** les appareils ; les notifications cessent ; la connexion répond
   ensuite `403 account_closed`, par mot de passe **comme par code**. Videz le
   jeton, le stockage sécurisé, les caches et les écrans, et revenez à la
   connexion.
2. **À `erase_at`** (trente jours plus tard par défaut) — l'**identité** part :
   le nom devient « Compte supprimé », le téléphone est brouillé puis
   **libéré**, l'e-mail, la photo, l'état civil, les préférences et le **carnet
   d'adresses** sont effacés, la boîte de notifications est purgée, et les
   messages écrits au chauffeur, au livreur ou au support sont jetés.

⚠️ **LES COURSES ET LES COMMANDES PASSÉES RESTENT — dites-le AVANT le bouton,
pas après.** Ce sont des **écritures comptables** : prix, commission, date, déjà
déclarés dans une facturation par pays. Les effacer trouerait un journal en
partie double. Mais elles ne **désignent plus personne** — aucun service ne
stocke de nom ni de téléphone, il les demande au compte au moment d'afficher —,
donc anonymiser le compte anonymise **tout** l'historique d'un seul coup,
partout, console et support compris. Un écran qui promet « tout sera effacé »
promet ce qui ne peut pas l'être, et c'est la réclamation qui suivra.

⚠️ **AFFICHEZ `erase_at`.** « Votre compte est fermé. Vos données seront
effacées le 6 novembre. » se comprend. « Compte supprimé » tout seul ne se
comprend pas — et la première réclamation arrive le jour où la personne appelle
le support, qui lit encore son nom à l'écran.

⚠️ **IL FAUT PROUVER QUI ON EST**, et c'est tout le corps de la requête :
`password` pour un compte qui en a un, `code` pour un compte né par code
(demandez-en un par `POST /auth/otp` ; il est **consommé** ici). L'opération est
irréversible depuis l'application : sans cette preuve, un téléphone déverrouillé
posé sur une table suffirait à faire disparaître le compte de quelqu'un.

⚠️ **VIDEZ LE PORTEFEUILLE D'ABORD.** Un solde, des jetons ou une **dette**
font refuser `409 wallet_not_empty`, et `error.meta.reason` dit lequel
(`money` · `tokens` · `debt`). On ne détruit pas de l'argent en silence :
emmenez la personne vers son solde, ou vers le support pour un remboursement.

⚠️ **PENDANT LES TRENTE JOURS, LE SUPPORT PEUT ANNULER.** C'est le seul recours
de qui a touché le bouton par erreur, et c'est la raison même du délai : dites-le
sur l'écran de confirmation, avec le moyen d'écrire au support. Se **réinscrire**
avec le même numéro pendant ce délai répond `403 account_closed` — pas
`phone_taken` : le numéro n'est rendu qu'à `erase_at`.

⚠️ **NE SUPPRIMEZ JAMAIS AU PREMIER APPUI.** Un écran de conséquences (ce qui
part, ce qui reste, la date), puis la preuve d'identité. Deux gestes pour une
action qu'aucun bouton ne défera.

| Refus | Quand | Ce que l'écran fait |
|---|---|---|
| `401 invalid_credentials` | mot de passe faux | souligner le champ, ne pas quitter l'écran |
| `401 otp_invalid` · `401 otp_expired` | code faux ou périmé | redemander un code |
| `409 wallet_not_empty` | solde, jetons ou dette (`meta.reason`) | envoyer vers le solde, ou le support |
| `409 deletion_already_requested` | demande déjà en cours | afficher `erase_at` et proposer le support pour annuler |
| `403 erasure_not_self_serve` | compte d'agent, de marchand ou de direction | renvoyer vers le support (il porte des versements à solder) |
| `403 account_closed` | à la connexion, au rafraîchissement ou à l'inscription | « ce compte a été supprimé » + « se réinscrire » ou support |

---

## 🎯 LES OBJECTIFS À ATTEINDRE — un bonus à gagner (v4.59.0)

```
GET /me/challenges   → { "items": [ … ] }
```

> ⚠️ **AU SOCLE** (`…/api/v1/me/challenges`, sans `/vtc` ni `/food`), et **une
> seule route pour tous les rôles** : le public vient du JETON. Il n'y a pas de
> paramètre à passer — et c'est voulu, puisqu'il déciderait de ce qu'on peut
> gagner.

```jsonc
{ "items": [
  { "id": "…",
    "title": "5 jours de travail cette semaine",
    "description": "…",
    "metric_label": "Jours travaillés",
    "target": 5,
    "money": false,          // true ⇒ la cible est un MONTANT, pas un compte
    "reward_xof": 5000,
    "value": 3,              // où vous en êtes
    "percent": 60,           // BORNÉ À 100
    "ends_at": "2026-10-19T00:00:00Z",
    "reached": false,
    "paid_xof": 0,
    "pending": false } ] }
```

### Ce que l'écran doit faire, et ce qu'il ne doit pas

⚠️ **AFFICHEZ `percent` TEL QUEL — IL EST DÉJÀ BORNÉ À 100.** Quelqu'un qui a
fait 7 courses sur un objectif de 5 est à 100 %, pas à 140 % : une barre qui
dépasse a l'air d'un bug, et chaque application l'aurait bornée à sa façon.

⚠️ **`value` ET `target` ENSEMBLE, JAMAIS LE POURCENTAGE SEUL.** « 60 % » ne dit
pas quoi faire ; « 3 sur 5 » dit qu'il en reste deux. C'est la même règle que le
taux d'acceptation (v4.55.0), et pour la même raison : un chiffre dont on juge
son avancement doit porter ce qui le compose.

⚠️ **`money: true` CHANGE L'AFFICHAGE** : « 15 000 F sur 20 000 F », et non
« 15 000 fois sur 20 000 ». Le serveur le dit pour que vous n'ayez pas à deviner
d'après le nom de la mesure.

⚠️ **`ends_at` EST CE QUI FAIT AGIR.** « 5 jours de travail » sans échéance n'est
pas un objectif. Affichez le temps restant, et laissez-le visible quand il
devient court — c'est l'information qui décide de sortir travailler ce soir.

⚠️ **`reached: true` S'AFFICHE AVANT `paid_xof`.** Ce sont deux moments : le
franchissement ouvre le droit, le versement suit. Attendre l'argent pour
annoncer la victoire ferait douter quelqu'un qui a compté ses courses lui-même.
Quand `pending: true`, dites-le en clair — **« bonus en cours de versement »** —
parce que le silence se lit comme un refus.

### 🔔 La notification — `challenge_reached`

```jsonc
{ "key": "challenge_reached",
  "title": "Objectif atteint — 5 000 F",
  "body":  "5 jours de travail cette semaine. Le bonus est sur votre solde.",
  "data":  { "type": "challenge", "challenge_id": "…" } }
```

⚠️ **ROUTEZ-LA VERS L'ÉCRAN DES OBJECTIFS**, pas vers l'accueil. Et **catégorie
`support`, donc NON COUPABLE** : quelqu'un qui aurait coupé les offres
commerciales doit tout de même apprendre qu'il vient d'être payé. Un bonus versé
que personne ne sait n'a motivé personne — et c'est tout ce qu'un objectif
cherche à faire.

### Ce que vous ne recevez PAS, et pourquoi

⚠️ **NI LE BUDGET, NI LE NOMBRE DE GAGNANTS, NI LES PLACES RESTANTES.** Ce que
l'entreprise a provisionné n'est pas l'affaire de quelqu'un qui joue : afficher
« 240 000 F d'enveloppe » invite à calculer combien d'autres ont déjà gagné, et
transforme un objectif en **course aux places** — où la moitié des gens
abandonnent en se croyant trop tard. N'essayez pas de les récupérer : aucune
route ne les sert à une application.

### Les règles qui vous concernent sans être visibles

⚠️ **UN OBJECTIF SE GAGNE UNE SEULE FOIS**, même si vous dépassez largement la
cible. Ce n'est pas un tarif : « 3 courses = 5 000 F » répété serait une prime au
volume. N'affichez donc pas « 2 fois gagné ».

⚠️ **UNE COURSE COMPTE À SON RÈGLEMENT, PAS À SON ACCEPTATION.** Une course
acceptée puis annulée ne fait pas avancer l'objectif — sinon il se gagnerait en
acceptant puis annulant vingt fois. Si votre écran montre l'avancement en temps
réel, attendez la fin de la course pour l'incrémenter, ou relisez la route.

⚠️ **UN OBJECTIF PÉRIODIQUE REMET SON COMPTEUR À ZÉRO.** « 20 courses cette
semaine » est un objectif NEUF chaque lundi, avec son propre identifiant : ne
gardez pas l'avancement de la semaine passée en cache, et ne supposez pas que
l'`id` reste le même d'une période à l'autre.

⚠️ **ET LA SEMAINE COMMENCE LE LUNDI**, pas le jour du lancement : si votre écran
affiche « cette semaine », comptez du lundi au dimanche comme le serveur le fait,
sinon vos deux nombres divergeront sans que personne ne comprenne pourquoi.

---

> ⚠️ **VOS OBJECTIFS PORTENT SUR CE QUE VOUS PRENEZ, PAS SUR CE QUE VOUS
> CONDUISEZ** : courses prises, commandes passées, montant dépensé. Et le montant
> compté est le **prix final** — une course dont le trajet a changé en route
> coûte autre chose que son devis.

> ⚠️ **UN OBJECTIF DE CLIENT MÊLE LES DEUX MÉTIERS.** « 5 commandes ce mois-ci »
> compte vos commandes de livraison ; « dépensez 20 000 F » compte les deux.
> N'essayez pas de deviner le métier d'après la mesure : `metric_label` le dit.

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

Envoyer sa position à un proche, et prévenir un contact choisi : non commencé.

> ⚠️ **À NE PAS CONFONDRE AVEC LE BOUTON D'ALERTE** (v4.56.0, section 🆘), qui
> existe et qui prévient **l'exploitation** — pas un proche. Les deux sont
> utiles et ne se remplacent pas : l'exploitation peut envoyer quelqu'un, un
> proche peut venir.

> ⚠️ **À ne pas confondre avec la course PARTAGÉE** (§4 quater, v4.42.0), qui
> est un mode de course — partager la **voiture** avec un autre passager.
> Envoyer sa position à un proche reste à faire.

### 🟡 Le prix « à partir de » sur l'écran d'accueil

`GET /classes` ne rend aucun prix, et c'est délibéré. Si la maquette en affiche
un, il faut demander un devis — donc connaître le trajet.
