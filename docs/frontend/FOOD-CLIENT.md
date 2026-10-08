# App CLIENT — LIVRAISON — contrat d'API

> **Version 4.50.0** · 8 octobre 2026
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
| Fil | `X-Request-ID` — **sur chaque réponse** ; à l'aller, facultatif mais recommandé (voir la section *Le fil d'une requête*) |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ; le code est le contrat.

**Un `422 validation_failed` nomme ses champs — v3.0.0.** `fields` liste les **clés JSON** en cause : soulignez **ces** cases, pas une bannière sous tout le formulaire. `reason` précise, quand ce n'est pas la valeur d'un champ : `unknown_field` (une clé que la route ne connaît pas — **refusée, pas ignorée**, son nom est dans `fields` ; c'est un bug de l'application) ou `invalid_json`.

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

## 2. Compte — **SOCLE** (base `…/api/v1/`, sans `/food`)

```
POST   /auth/register        { phone (E.164, avec le +), name, password, role: "client", email?, first_name?, last_name? }
POST   /auth/login           { phone | email, password, app: "client" }   ⚠️ v4.26.0
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

⚠️ **Ce qui se passait sans lui** : un livreur ou un marchand se connectait ici, et rien ne
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

Affichez « Ce compte est un compte livreur. Ouvrez l'application Dira
Livreur. » — **jamais « identifiants invalides »** : le mot de passe était
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
livreurs**, jamais un client.

⚠️ **N'ENVOYEZ PAS `device_id`, ET NE CODEZ RIEN POUR CETTE RÈGLE.** Un client a le droit d'être connecté sur sa tablette **et** son téléphone, et de suivre sa commande depuis les deux.
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
véhicules. Rien de tel chez un client, qui ne pousse aucune position — et l'appliquer « par prudence » à un public qui
n'a pas ce problème coûterait des déconnexions quotidiennes pour rien.


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

> ⚠️ **UNE PROMOTION PEUT S'ARRÊTER EN COURS DE JOURNÉE (v4.17.0).** Chaque
> opération a une **enveloppe** — ce que la plateforme s'engage à dépenser —
> et un nombre d'utilisations, parfois **par personne**. Quand c'est
> consommé, l'offre s'arrête.
>
> **Vous n'avez rien à calculer.** Le prix remisé et `original_price` du menu
> tiennent déjà compte de tout cela : ce sont les **mêmes** règles qui
> tarifent le menu et la commande, donc le menu ne promet jamais une remise
> que la commande refuserait. Un client qui a épuisé son droit voit
> simplement le prix plein, sans explication — et c'est voulu : « vous avez
> déjà utilisé cette offre » sur une carte de restaurant ne rend service à
> personne.
>
> Conséquence à prévoir dans l'interface : **un prix mis en cache vieillit**.
> Relisez le menu à l'ouverture de l'écran de commande plutôt que de
> réafficher celui d'il y a une heure, et fiez-vous au **récapitulatif de
> commande** pour le total.

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
GET /orders?before=2026-10-05T00:00:00Z   # bornée à ce qui est né AVANT (v4.43.0)
GET /orders/{id}
POST /orders/{id}/cancel
```

⏳ **`before` BORNE L'HISTORIQUE À UNE FENÊTRE DE DATE (v4.43.0)** — un instant
RFC 3339, **strictement** avant. Il se combine avec `?status=` et `?cursor=`
sans se gêner. ⚠️ **Une date illisible est REFUSÉE** (`422`,
`fields: ["before"]`), pas ignorée : l'ignorer servirait silencieusement
l'historique entier là où vous demandiez une tranche, et une application qui
pagine ne s'en apercevrait jamais.

Il existe pour **l'application cliente unifiée** (`DIRA-CLIENT.md`), qui mêle
les commandes et les courses semaine par semaine : sans borne de date, elle
repartait du haut à chaque page. Vous n'en avez pas besoin pour un historique
ordinaire — `cursor` suffit.

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

> ⚠️ **`picking_up` PEUT DURER, et c'est normal (v4.19.0).** Quand votre
> commande vient de **plusieurs enseignes**, le livreur passe chez chacune
> avant de partir. `delivery.pickups_count` dit **combien** de collectes la
> course demande ; tant qu'elles ne sont pas toutes faites, la commande
> reste `picking_up`.
>
> ⚠️ **Absent = on ne sait pas** (commande d'avant ce compteur), jamais
> « aucune » : n'écrivez alors rien plutôt que « chez 0 enseigne ».
>
> Écrivez-le : « il récupère votre commande chez 3 enseignes » vaut mieux
> qu'une barre qui n'avance pas. Et dessinez **chaque** point de collecte
> avec le pin numéroté du **marchand** à son rang (§ Les marqueurs de carte)
> — trois pastilles identiques sur la carte ne se lisent pas.

---

## 5. Suivi de livraison — LE LIVREUR EN DIRECT, dès qu'il accepte

### Le moment exact : `accepted`

Tant que la commande cherche un livreur (`ready` côté commande, `searching`
côté course), **personne n'a de position à montrer** : n'ouvrez pas de
socket, n'affichez pas de carte vide avec une moto immobile. Affichez « nous
cherchons un livreur ».

Le passage à **`accepted`** est le signal, et il vous arrive de trois
façons — prenez la première qui vient :

| Chemin | Quand |
|---|---|
| push `order_assigned` (`data.order_id`) | application en arrière-plan |
| socket des commandes, trame d'état | application ouverte |
| `GET /orders/{id}` à l'ouverture de l'écran | toujours, c'est l'état de référence |

**À ce moment, relisez `GET /orders/{id}`** : la commande porte alors tout ce
qu'il faut.

```jsonc
{ "id": "6aa…", "status": "accepted",
  "delivery": {
    "id": "6ab…",                       // ← la MISSION du suivi
    "tracking_mission_id": "6ab…",      // présent quand il diffère de `id`
    "status": "accepted",
    "planned_route": [[1.2255, 6.1319], [1.2301, 6.1402]],
    "planned_distance_m": 3400, "planned_duration_s": 720,
    "courier": { "name": "Koffi M.", "phone": "+228…", "vehicle": "Haojue HJ125 · TG-3421-AB",
                 "rating_avg": 4.8, "rating_count": 126 } } }
```

`delivery.courier` n'apparaît **qu'une fois la course attribuée** : c'est
exactement ce qu'il faut afficher en tête de l'écran de suivi — **qui
vient**, dans **quoi**, avec quelle note, et le bouton d'appel.

### Ouvrir le socket du suivi

```
wss://tracking-staging.dira.llc/track/subscribe/{mission_id}?token=<access_token>
```

- **`mission_id`** = `delivery.tracking_mission_id` s'il est là, **sinon**
  `delivery.id`. C'est la clé de jointure entre l'API et le suivi.
- ⚠️ **Le jeton est OBLIGATOIRE** et c'est **votre jeton d'accès habituel**
  (le même que pour l'API). Deux façons de le donner : l'en-tête
  `Authorization: Bearer …` quand votre client WebSocket le permet, **sinon
  `?token=`** dans l'URL — c'est le cas des WebSocket de navigateur, qui
  n'acceptent aucun en-tête. Sans jeton : `401`, et vous croirez à une panne
  de réseau.
- ⚠️ **La base d'URL du suivi est distincte de celle de l'API.** Deux
  variables d'environnement, et le suivi ne passe **pas** par la passerelle.
- Le jeton **expire** (§1 bis) : à la rotation, **rouvrez le socket** avec
  le nouveau. Un socket ouvert ne se ré-authentifie pas tout seul.

```jsonc
{ "type": "hello",    "mission_id": "6ab…", "vehicle_id": "…", "vehicle_type": "moto",
  "plate": "TG-3421-AB", "lng": 1.2255, "lat": 6.1319, "heading": 122.5,
  "heading_source": "gps", "speed": 8.3, "ts": 1757… }          ⚠️ v4.28.0
{ "type": "position", "vehicle_id": "…", "vehicle_type": "moto", "plate": "TG-3421-AB",
  "lng": 1.2255, "lat": 6.1319, "heading": 122.5, "heading_source": "gps",
  "speed": 8.3, "ts": 1757… }
{ "type": "status",   "status": "in_transit", "ts": 1757… }
```

### 🛰️ L'ACCUEIL PORTE LA DERNIÈRE POSITION CONNUE (v4.28.0)

**`hello` arrive désormais avec la position du véhicule**, aux mêmes champs
qu'une trame `position`. **Dessinez la moto dès l'accueil.**

⚠️ **Ce que ça répare.** L'accueil ne disait que « bonjour », et il fallait
attendre la trame suivante — jusqu'à plusieurs secondes. À chaque
reconnexion (réseau retrouvé, application revenue au premier plan, jeton
rafraîchi), le client voyait **sa carte se vider puis se remplir**.

- **`ts` est l'horodatage du DERNIER point reçu**, pas l'instant de la
  connexion : il peut dater de quelques dizaines de secondes. À vous de dire
  « il y a 12 s » si vous le jugez utile ; la plateforme ne l'invente pas.
- **Un `hello` NU — sans `lng`/`lat` — reste possible** (livreur pas encore
  attribué, téléphone silencieux, suivi indisponible) : comportez-vous alors
  comme avant. **Ne dessinez jamais un `hello` sans coordonnées** comme s'il
  en avait.

### 🔁 LA RECONNEXION EST OBLIGATOIRE, PAS OPTIONNELLE (v4.28.0)

Tant qu'une livraison est en cours, l'application **doit** rouvrir le socket
toute seule. Un socket tombé sans reconnexion laisse une moto figée sur la
carte, et rien à l'écran ne dit que l'information a cessé d'arriver.

| Événement | Ce que l'application fait |
|---|---|
| Socket fermé, quelle qu'en soit la cause | rouvrir, back-off **1 s, 2 s, 4 s … 30 s** |
| Retour au **premier plan** | rouvrir **immédiatement**, sans attendre le back-off |
| Réseau retrouvé | rouvrir immédiatement |
| Code **4401 `token_expired`** | rafraîchir le jeton **puis** rouvrir — jamais avec le même |
| **403** à la poignée de main | s'arrêter : ce rôle ne suit pas cette course |
| Livraison terminée ou annulée | fermer, et ne plus rouvrir |

À chaque réouverture, relisez aussi la commande : le socket rend la position,
mais l'état a pu changer pendant la coupure.

### Dessiner

- **La première position peut tarder** *(moins souvent depuis la v4.28.0 :
  l'accueil la porte quand elle est connue)* : le téléphone du livreur émet
  toutes les quelques secondes, et il vient peut-être de démarrer. En
  attendant, centrez sur la **boutique** et tracez `planned_route` — le
  client voit le chemin prévu avant de voir la moto.
- **Interpolez** entre deux trames, sinon le marqueur saute. `heading`
  oriente la flèche ; `heading_source` dit si le cap est mesuré (`gps`) ou
  déduit — un cap déduit à l'arrêt tourne dans le vide, ne l'animez pas.

> ### 🧭 ⚠️ LES DEUX PINS D'UN VÉHICULE, ET LEUR ORIENTATION (v4.20.0)
>
> **Un véhicule a DEUX images de marqueur, pas une.** Elles viennent de le genre `courier` — `GET /map-markers` :
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
- **Les marqueurs** : le livreur est `courier`, la boutique `merchant`,
  votre adresse `client` (§ Les marqueurs de carte, ci-dessous).
- **Un silence n'est pas une disparition.** Batterie, tunnel, application
  fermée : gardez la **dernière position connue** avec son heure (« il y a
  2 min ») et grisez-la au-delà d'une minute. Ne faites **jamais** revenir
  le marqueur à la boutique parce que le flux s'est tu.
- **Pas d'ETA inventée.** `planned_duration_s` est la durée prévue de la
  tournée, pas un temps restant. Si vous affichez un « dans ~X min »,
  dites-le comme une estimation, et ne le faites pas reculer à chaque trame.

### La trame `status`, et ce qu'elle vaut

Elle arrive à **chaque changement d'état** (`accepted`, `picking_up`,
`in_transit`, `completed`, `cancelled`) — les mots du vocabulaire commun
(§ en-tête). **Elle ne porte que le mot** : changez le badge tout de suite
si vous voulez, puis **relisez `GET /orders/{id}`** et redessinez. Une trame
perdue ne casse alors rien.

Ce que le client doit lire à chaque étape :

| État | Ce qu'on montre |
|---|---|
| `accepted` | « Koffi vient chercher votre commande » — carte, livreur, route prévue |
| `picking_up` | « Il est à la boutique » |
| `in_transit` | « En route vers vous » — c'est là que la carte compte le plus |
| `completed` | la carte se fige, on propose la note (§7) |
| `cancelled` | on ferme le socket et on explique — `cancelled_reason` |

### Tenir la connexion

- **Reconnexion avec back-off** (1 s, 2 s, 4 s… plafonné à 30 s), et **pas
  de boucle serrée** : un socket refusé en `401` ne s'arrangera pas en
  réessayant vite — rafraîchissez le jeton d'abord.
- **En arrière-plan, fermez le socket** ; au retour au premier plan,
  **relisez `GET /orders/{id}` PUIS rouvrez**. Un socket maintenu en
  arrière-plan vide la batterie pour des positions que personne ne regarde.
- **Repli sans socket** (réseau captif, socket refusé) : sondez
  `GET /deliveries/{id}` toutes les **10 à 15 secondes**, pas plus souvent.
  Dites-le à l'écran (« suivi allégé ») plutôt que de laisser croire au
  direct.
- **À `completed` ou `cancelled` : fermez.** Un socket laissé ouvert sur une
  mission terminée ne reçoit plus rien et empêche le téléphone de dormir.

---

Sur la carte du suivi : le livreur est `courier`, la boutique `merchant`, votre adresse `client`.

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

**Sur VOS cartes.** Votre adresse de livraison se dessine avec le pin
**principal** du `client` — et non celui de destination : quelqu'un y attend,
c'est vous. Le pin de destination sert aux courses, où l'arrivée est un lieu
et personne ne s'y trouve encore. Quand votre commande est collectée chez **plusieurs
enseignes**, chaque point de collecte prend un pin numéroté du **marchand**,
dans l'ordre où le livreur y passe — c'est ce qui permet de suivre sa
tournée au lieu de voir trois pastilles identiques.

## 🔒 CE QUE VOUS AVEZ LE DROIT D'AFFICHER DE L'AUTRE PARTIE (v4.49.0)

**C'est l'exploitation qui décide, pays par pays et métier par métier** —
Paramètres › Confidentialité de la console. Vous n'avez rien à arbitrer, et
surtout rien à deviner : **le serveur n'envoie pas ce qu'on n'a pas le droit de
montrer.**

`GET /deliveries/{id}` → `courier` porte ce qui est ouvert de votre livreur :

```jsonc
{ "courier": {
    "name": "Mamadou D.",          // déjà masqué au niveau choisi, ou absent
    "first_name": "…", "last_name": "…",   // absents sauf si ouverts
    "avatar_url": "…", "gender": "…",      // absents sauf si ouverts
    "phone": "+228…",              // ⚠️ présent dès qu'il est AFFICHABLE **ou** COMPOSABLE
    "show_phone": true,            // le numéro peut-il être écrit à l'écran ?
    "direct_call": true,           // un bouton d'appel peut-il être proposé ?
    "in_app_alert": false,         // peut-on faire sonner son application ?
    "rating_avg": 4.6, "rating_count": 212,  // absents si la note est fermée
    "vehicle": "Moto · TG-4417" } }           // ⚠️ TOUJOURS servi — voir ci-dessous
```

⚠️ **LE VÉHICULE N'EST JAMAIS FERMÉ**, et c'est voulu : une plaque et un type ne
nomment personne, et c'est ce que vous guettez dans la rue. C'est ce qui reste
quand tout le reste est fermé — montrez-le.

⚠️ **UN CHAMP ABSENT N'EST PAS UNE PANNE.** C'est un champ que l'exploitation
n'a pas ouvert dans ce pays. Affichez votre propre libellé — « votre livreur » —
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
POST /ai/voice        multipart : file=<audio>, language=<code facultatif>
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
| `401 otp_invalid` · `401 otp_expired` | code faux ou périmé — garder la saisie, ou proposer d'en redemander un |
| `401 otp_too_many_attempts` | le code est **mort** : revenir à l'écran du numéro |
| `429 otp_too_soon` · `429 otp_too_many_requests` | « renvoyer » grisé jusqu'à `resend_after` ; au plafond horaire, proposer le support |
| `503 otp_delivery_failed` | rien n'a été consommé : redemander tout de suite est légitime |
| `403 otp_not_available` | ce compte se connecte par mot de passe |
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
| `account_closed` | **403** | ce compte a été **supprimé** — à la connexion comme à l'inscription : « se réinscrire », ou le support si c'est une erreur |
| `wallet_not_empty` | 409 | suppression de compte refusée : solde, jetons ou dette (`meta.reason`) → vider, ou demander un remboursement |
| `deletion_already_requested` | 409 | suppression déjà demandée — afficher `erase_at`, proposer le support pour annuler |
| `erasure_not_self_serve` | 403 | ce type de compte se supprime par le **support** |
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
