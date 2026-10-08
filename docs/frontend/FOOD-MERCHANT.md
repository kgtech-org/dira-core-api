# App / console MARCHAND — LIVRAISON — contrat d'API

> **Version 4.48.0** · 8 octobre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Livraison : `https://api-staging.dira.llc/api/v1/food`


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

Une **enseigne** (`merchant`) possède plusieurs **points de vente** (`store`). Le **catalogue est au niveau de l'enseigne** ; prix et disponibilité se **surchargent** par point de vente.

> C'est la distinction qui structure tout le reste. Un plat existe une fois pour la marque ; chaque boutique décide de son prix et de sa disponibilité. Un **portefeuille de jetons par point de vente**, pas un par enseigne.

Un sélecteur de boutique en tête d'écran fixe le point de vente courant : tableau de bord, portefeuille et carte en dépendent tous.

### Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` — le même jeton pour le socle et pour la livraison |
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

### 🧵 LE FIL D'UNE REQUÊTE — `X-Request-ID` (v4.34.0)

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

⚠️ **« J'ai validé et rien ne s'est passé » se tranche avec ce fil.** Valider une
préparation ouvre l'appel des livreurs dans un AUTRE service que celui que la
console a appelé : sans fil commun, la question « le clic est-il arrivé ? » et
la question « l'appel a-t-il été ouvert ? » vivent dans deux journaux qu'il faut
recouper à la main.

⚠️ **Le fil n'est pas une référence métier.** Il nomme un APPEL, pas une
commande : pour désigner la commande, c'est son `id` qu'on cite.

---

### 🗺️ LE FOND DE CARTE — `maps` (v4.40.0)

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

> 🔑 **LA PORTE PAR CODE N'EST PAS POUR VOUS (v4.46.0).** Les clients
> s'inscrivent et se connectent désormais avec leur téléphone et six chiffres
> (`POST /auth/otp`, `POST /auth/otp/verify`). **Votre application garde
> `POST /auth/login`** : un compte qui n'est pas client reçoit
> `403 otp_not_available` et aucun code n'est envoyé. Ce n'est pas un oubli —
> six chiffres remplacent un mot de passe, et un numéro de téléphone s'affiche
> sur une plaque, se donne à un passager, se lit dans un annuaire. Protéger
> avec cela un compte qui tient un portefeuille et une dette serait le
> protéger avec rien.

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

## 🗑️ SUPPRIMER UN COMPTE — pas depuis cette application (v4.47.0)

`DELETE /me` existe au socle, mais il répond **`403 erasure_not_self_serve`** à
un compte de marchand. **N'affichez pas de bouton « supprimer mon compte » qui mène à
un refus** : mettez « **Fermer mon compte — écrire au support** ».

⚠️ **POURQUOI CE COMPTE N'EST PAS LIBRE DE PARTIR.** Il porte des
**versements**, parfois une **dette**, le produit de ses ventes en attente de retrait et un solde de jetons — et tout cela vit dans la
verticale, pas dans le compte. Le support solde d'abord et supprime ensuite, le
grand livre sous les yeux. Un bouton dans l'application laisserait une dette
sans débiteur et des versements sans destinataire.

⚠️ **CE QU'IL FAUT QUAND MÊME TRAITER : `403 account_closed`.** Quand le support
ferme un compte, **les sessions tombent immédiatement** et la connexion le refuse
ensuite — par mot de passe **comme par code**. Traitez-le comme une fin de
session **définitive** : videz le jeton et le stockage sécurisé, fermez le socket des commandes, et
affichez le message tel quel plutôt que de renvoyer vers « mot de passe
oublié ». Ce n'est pas `account_suspended` : une suspension se lève, une
fermeture ne se lève que par le support, et seulement pendant trente jours.

⚠️ **TRENTE JOURS PLUS TARD, L'IDENTITÉ PART** : le nom devient « Compte
supprimé » partout où les commandes passées le nommaient encore. Les commandes, elles,
**restent** — ce sont des écritures comptables, avec leur prix, leur commission
et leur date.

---

## 2. Enseigne et points de vente

```
POST   /merchants                    créer son enseigne
GET    /me/merchant                  la sienne
GET    /merchants/{id}
PATCH  /merchants/{id}
POST   /merchants/{id}/stores        ajouter un point de vente (+ portefeuille)
GET    /stores/{id} · PATCH /stores/{id}
PATCH  /stores/{id}/availability     ouvrir / fermer
GET    /tags
```

### Ouvrir et fermer

```
PATCH /stores/{id}/availability   { "open": false, "reason": "coupure d'eau" }
```

- **`open` est obligatoire** : « fermer » et « rouvrir » sont deux gestes opposés, et un champ facultatif ferait dépendre le sens de l'appel d'une valeur par défaut que personne ne lit.
- `reason` **s'affiche au client** : « fermé » sans motif laisse croire à une panne, et le client cherche ailleurs. Il est **ignoré à la réouverture** — garder un motif sur une boutique ouverte finirait par s'afficher.
- La réponse porte `open` (booléen) et `closed_reason`.

> ⚠️ **`open` (marchand) et `status` (administration) sont deux axes SÉPARÉS.** Le premier est « je ferme aujourd'hui » ; le second est `pending` / `active` / `suspended` et n'appartient qu'à l'administration. Un marchand qui pourrait écrire `status` lèverait sa propre suspension. Une boutique n'est servable que si elle est `active` **et** ouverte.

Un point de vente fermé **refuse les commandes** : `POST /orders` échoue si l'une des boutiques du panier est fermée. C'est vérifié une fois par point de vente, pas par ligne.

---

## 3. Catalogue

```
POST   /dishes                       créer un plat (niveau ENSEIGNE)
PATCH  /dishes/{id} · DELETE /dishes/{id}
GET    /dishes/{id}
GET    /dish-categories
PUT    /stores/{id}/dishes/{dish_id} surcharge PRIX / DISPONIBILITÉ
GET    /stores/{id}/menu             la carte telle que le client la voit
```

Un plat porte : `name`, `description`, `base_price`, `category_id`, `images`, `calories`, `ingredients`, **`allergens`**, `variants`, `option_group_ids`.

### Tags

Un plat porte des `tags` — **les mêmes slugs** qui étiquettent votre enseigne et vos points de vente (`GET /tags`). Cinq au maximum.

> ⚠️ **Le tag n'est pas la catégorie.** `category_id` dit *ce que c'est* et il n'y en a qu'un : c'est la structure de votre carte. Les tags disent *à quoi ça se rattache*, et il y en a plusieurs : c'est ce sur quoi le client cherche.

> **Ils ne sont pas hérités de la boutique.** Un restaurant italien vend du café — hériter classerait ce café « italien » dans toutes les recherches. Étiquetez plat par plat.

Un slug hors vocabulaire est **refusé** (`422`), jamais ignoré. Champ `tags` **absent** = inchangé ; tableau **vide** = étiquettes retirées.

Ils alimentent `GET /stores/{id}/menu?tags=` — le filtre de carte côté client.

### Allergènes

> ⚠️ **Déclarés, jamais déduits des ingrédients.** « Sauce d'arachide » se devine, « pâte de sésame » beaucoup moins, et une allergie ne se joue pas sur une heuristique de chaîne de caractères. L'application cliente croise ces valeurs avec le profil nutrition et **avertit** à l'intersection.

> Laisser le champ vide veut dire « **non renseigné** », pas « sans allergène ». L'app cliente distingue les deux — mais un catalogue non renseigné ne protège personne. Poussez à le remplir.

### Variantes

L'**échelle de prix** d'un plat. Vide : le plat se vend au `base_price`. Non vide : `base_price` **n'est plus utilisé** pour la tarification, et le client **doit** choisir.

```jsonc
"variants": [ { "name": "Petit", "price": 2000, "sort_order": 1 },
              { "name": "Grand", "price": 3000, "sort_order": 2 } ]
```

### Groupes d'options

Une **bibliothèque au niveau de l'enseigne**, rattachée aux plats par **référence**.

```
GET    /me/option-groups · GET /merchants/{id}/option-groups
POST   /option-groups        { name, min_select, max_select, options: [{name, price}] }
PATCH  /option-groups/{id}
DELETE /option-groups/{id}
```

> **Référence et non copie** : une option ajoutée au groupe apparaît aussitôt sur tous les plats qui le portent. Corriger un prix d'alloco une fois plutôt que sur trente plats.

⚠️ **Supprimer un groupe le détache d'abord de tous les plats.** Sans ce détachement, un plat référencerait un groupe disparu et perdrait ses choix **en silence** — le client commanderait un plat sans son accompagnement obligatoire.

`min_select` / `max_select` bornent chaque groupe. Le serveur vérifie les bornes à la commande.

### Surcharge par point de vente

```
PUT /stores/{id}/dishes/{dish_id}   { price_override?, available }
```

`price_override` absent ⇒ le `base_price` de l'enseigne. C'est le prix **effectif** qui apparaît sur la carte et que le client paiera.

---

## 4. Commandes

### Comment une commande arrive ici — v1.4.0

Deux chemins, et le marchand ne les distingue pas :

- le client a **choisi ce comptoir** — son choix fait foi, rien ne le remplace ;
- il a commandé **l'enseigne**, et le serveur a retenu **ce point de vente parce qu'il est le plus proche de l'adresse de livraison** et qu'il avait le plat.

> ⚠️ **Il n'y a toujours aucun appel, ni compte à rebours.** Le point de vente est choisi à la création de la commande, pas proposé : le prix et les frais de livraison en dépendent, et doivent être connus **avant** le paiement. Un écran « Vous êtes le point de vente sélectionné — 30 s » promettrait une décision que le marchand n'a pas à prendre.

> La seule réponse possible reste **accepter en préparant**, ou **refuser** — et un refus annule la commande entière, il ne la passe pas à la boutique voisine. Le ré-acheminer changerait le montant après que le client a payé, puisque les prix se surchargent par point de vente.


```
GET   /stores/{id}/orders?status=paid,preparing&limit=&cursor=
PATCH /stores/{id}/orders/{order_id}/status   { status: "preparing" | "ready" }
```

`?status=` porte les onglets « Nouvelle / En préparation / Avec le livreur / Terminée ». Plusieurs statuts **élargissent**, et le filtre est appliqué par la **base** : filtrer une page déjà paginée donnerait des pages courtes et un curseur qui saute.

`?q=` cherche dans les **noms de plats** de la commande — « le client qui avait pris du poulet ». Un identifiant complet retrouve la commande exacte.

**La nouvelle est en tête** (v4.12.1) : triée par date de création, la plus récente d'abord — c'est ce que cette spec promettait, et jusqu'à la v4.12.0 le serveur rendait l'inverse. `?cursor=` (l'identifiant de la dernière ligne reçue) descend vers les plus anciennes.

Seuls `preparing` et `ready` sont à la main du marchand. Le reste suit le livreur :
`accepted` (il part au restaurant) → `picking_up` (il retire) → `in_transit`
→ `completed`. **v4.0.0** : ces quatre mots sont ceux du vocabulaire commun de
la plateforme — `assigned`, `delivering`, `delivered` n'existent plus, et
`?status=delivered` répond `422`. Onglets suggérés : Nouvelle = `paid` ·
En préparation = `preparing,ready` · Avec le livreur =
`accepted,picking_up,in_transit` · Terminée = `completed,cancelled`.

### Être prévenu d'un changement — socket, push, `GET` (v4.0.0)

```
wss://api-staging.dira.llc/api/v1/food/ws/orders?token=<access_token>

{ "type": "hello", "role": "merchant" }
{ "type": "order_created", "order_id": "…", "status": "pending_payment", "total": 5200, "ts": … }
{ "type": "order_status",  "order_id": "…", "from": "ready", "status": "accepted", "ts": … }
```

Un marchand ne reçoit que les commandes **de ses points de vente**. La trame
ne porte pas la commande : `order_created` → `GET /stores/{id}/orders` (la
nouvelle est en tête) ; `order_status` → relire la commande, ou la liste de
l'onglet. **Une annulation par le client arrive ici aussi**
(`status: cancelled`) — avant la v4.0.0, elle ne se voyait qu'en
rafraîchissant. Le push `merchant_new_order` (ci-dessous) fait la même chose
quand l'application est fermée.

**Le flux, dans l'ordre — un signal, un `GET` :**

1. **À l'ouverture d'un écran** : `GET /stores/{id}/orders/{order_id}`. C'est l'état de référence —
   jamais ce que dit le socket.
2. **Socket ouvert** : sur une trame d'état, comparez à ce que vous affichez ;
   si ça diffère, `GET /stores/{id}/orders/{order_id}` et redessinez. La trame porte le statut : vous
   pouvez changer le badge **avant** la réponse. Une trame qui « recule »
   (un `from` qui n'est pas votre état) signale une trame manquée — relisez.
3. **Push reçu** (application en arrière-plan) : `data.type` dit quoi ouvrir,
   l'identifiant sur quoi, `data.status` ce qui a changé. Même geste : ouvrir
   l'écran, `GET /stores/{id}/orders/{order_id}`.
4. **Reconnexion** du socket (back-off 1 s → 2 s → 4 s … 30 s) :
   `GET /stores/{id}/orders/{order_id}` **immédiatement**, avant d'appliquer la moindre trame — tout
   ce qui s'est passé pendant la coupure n'est que dans la base.
5. **Sans socket** (refusé, réseau captif, batterie) : **sondez** `GET /stores/{id}/orders/{order_id}`
   toutes les **10 s** tant que l'opération n'est ni `completed` ni
   `cancelled`, en comparant `updated_at` ; passez à 30 s au bout de cinq
   minutes sans changement. Ne sondez **jamais** une opération terminée.
6. **La liste** : `order_created` → `GET /stores/{id}/orders?status=paid`
   (la nouvelle est en tête) plutôt qu'une commande à la fois ; le socket
   dit « il y a du nouveau », la liste dit quoi.

**Ce qu'aucun canal ne garantit** : l'ordre, l'unicité, la livraison. Deux
trames pour le même passage (socket **et** push) sont normales — le second
`GET` répond la même chose. Une application qui ferait du socket sa source
de vérité verrait, un jour, une course « en route » qu'un `GET` dit terminée.

### 🔔 La nouvelle commande en TEMPS RÉEL — le pop-up de 5 s (v4.6.0)

C'est l'écran de la maquette mobile : une carte **« Nouvelle commande »**
qui **surgit en haut de n'importe quel écran** de l'application, reste
**5 secondes**, puis se range dans l'onglet *Nouvelle*. Voici ce qu'il faut
faire, dans l'ordre — et ce qu'il ne faut pas faire.

**1. Un seul socket, ouvert POUR TOUTE L'APPLICATION.** Ouvrez
`wss://…/api/v1/food/ws/orders?token=` **dès la connexion réussie**
(`POST /auth/login` ou `/auth/refresh`) et gardez-le tant que l'application
est au premier plan — pas seulement sur l'écran des commandes : la cuisine
regarde son catalogue quand la commande tombe. Un socket par application,
jamais un par écran (un réseau mobile ne pardonne pas trois connexions).
Réouvrez-le à la reprise au premier plan et à chaque nouveau jeton
(`/auth/refresh` toutes les 15 min : le jeton du socket expire avec l'accès,
le serveur ferme — reconnectez avec le neuf).

**2. La trame qui fait surgir la carte : `status: "paid"`.** Pas
`order_created` en soi — une commande naît `pending_payment` quand le client
paie par mobile money, et il n'y a rien à préparer tant que l'argent n'est
pas arrivé. Deux trames disent « c'est à vous » :

```
{ "type": "order_created", "order_id": "…", "status": "paid",
  "store_ids": ["…"], "total": 5200, "ts": … }                      ← espèces, solde Dira
{ "type": "order_status",  "order_id": "…", "from": "pending_payment",
  "status": "paid", "store_ids": ["…"], "total": 5200, "ts": … }    ← mobile money confirmé
```

Règle : **`status === "paid"` ⇒ pop-up**, quel que soit `type`. Tout autre
`order_created` (`pending_payment`) se **mémorise sans rien afficher** ; le
`paid` qui suit fera surgir la carte. Une trame `paid` déjà vue (même
`order_id`) ne resurgit pas — socket et push peuvent la porter tous les deux.

**3. Ce que la carte montre, et d'où ça vient.** La trame porte de quoi
**dessiner immédiatement** : `store_ids` → le nom du point de vente (vous
avez `GET /me/merchant` en cache), `total` → le montant, `ts` → « à
l'instant ». Affichez la carte **avec ça, tout de suite**, puis
`GET /stores/{store_id}/orders/{order_id}` pour compléter : nombre
d'articles, premier plat, adresse. Si le `GET` tarde, la carte reste avec
ce qu'elle a ; s'il répond `404` (commande annulée entre-temps), retirez-la.
`store_ids` peut en porter plusieurs (commande multi-comptoirs) : la carte
nomme **ceux qui sont à vous** — les vôtres seulement sont dans la trame.

**4. Le comportement de la carte — exactement celui de la maquette :**

- **surgit** par le haut, par-dessus l'écran courant, sans le fermer ;
- **son + vibration** à l'apparition (c'est une commande payée : le
  client attend, la sonnerie est méritée — contrairement à `pending_payment`) ;
- **reste 5 s**, avec la barre de progression qui se vide, puis se **replie
  seule** ; toucher la carte l'ouvre (`GET`, écran de la commande, boutons
  *Préparer* / *Refuser*) ; la glisser vers le haut la ferme avant les 5 s ;
- une **deuxième** commande pendant les 5 s : la carte **se remplace**
  (nouvelle en avant, pastille « +1 en attente ») — jamais deux cartes
  empilées, jamais une carte qui dure plus de 5 s parce qu'il en arrive
  d'autres ;
- **rien n'est perdu quand elle se replie** : l'onglet *Nouvelle* porte un
  **badge** avec le nombre de commandes `paid` non ouvertes, et la liste
  s'est déjà rafraîchie (`GET /stores/{id}/orders?status=paid`, la
  nouvelle en tête) ;
- la carte **ne demande aucune décision** : pas de « Prendre / Passer », pas
  de compte à rebours (§ 8 bis) — la commande est déjà la vôtre.

**5. À la reconnexion, PAS de pop-up.** Tout ce qui est tombé pendant une
coupure est dans la base, pas dans une trame : `GET /stores/{id}/orders?status=paid`
d'abord, badge sur l'onglet, et la carte ne surgit que pour ce qui arrive
**après** la reconnexion. Faire surgir cinq cartes à la suite au retour du
réseau apprend à fermer les cartes sans les lire.

**6. Application fermée ou en arrière-plan :** c'est le push
`merchant_new_order` (ci-dessous) qui sonne, par le système. Ne dessinez pas
la carte depuis un push : ouvrez l'écran de la commande. Et une commande
reçue par push **et** par socket au retour au premier plan ne surgit qu'une
fois — c'est la règle du `order_id` déjà vu.

**Aucun changement d'API** : les trames, les routes et le push sont ceux
décrits au-dessus. Cette section dit comment les **tenir** pour que la
maquette soit vraie — un socket ouvert partout, `paid` comme seul signal,
5 secondes, jamais deux cartes.

**Les neuf statuts d'une commande**, dans l'ordre — les quatre derniers sont
ceux de la course de livraison, mot pour mot, et ceux d'une course VTC :

| Statut | Qui l'écrit | Ce que le marchand fait |
|---|---|---|
| `pending_payment` | le paiement | rien — n'est pas encore une commande |
| `paid` | le paiement | **la préparer** (`preparing`) ou la refuser |
| `preparing` | vous | la finir (`ready`) |
| `ready` | vous | attendre le livreur — la course est proposée aux livreurs à cet instant |
| `accepted` | le livreur | un livreur arrive |
| `picking_up` | le livreur | il retire |
| `in_transit` | le livreur | en route vers le client |
| `completed` | le livreur | livrée — la vente est acquise |
| `cancelled` | le client, vous (refus), l'exploitation | rien à préparer, ou arrêter |


### ⚠️ Aucun livreur en 15 minutes : la course expire, le marchand relance — v3.7.0

Dès que la commande passe à `ready`, la plateforme appelle des livreurs. Si
**personne ne l'a prise au bout de 15 minutes**, la course **expire** : elle
est retirée de la liste des livreurs (plus personne ne peut la prendre), et
le marchand reçoit la notification **`delivery_expired`** —
« Commande [order_ref] : aucun livreur en 15 min. La recherche est arrêtée —
relancez-la depuis la commande quand vous êtes prêt. » Data :
`{ "type": "delivery_expired", "order_id", "store_id" }`.

La commande n'est **pas annulée** : le repas est prêt, payé, et seul le
marchand sait s'il tient encore. À lui de relancer :

```
POST /stores/{id}/orders/{order_id}/relaunch
→ 200 Order            un nouvel appel part, le délai de 15 min repart de zéro
→ 409 order_not_ready  la commande n'est pas (ou plus) `ready`
→ 409 search_running   un appel court encore : rien à relancer
→ 409 delivery_taken   un livreur l'a prise entre-temps
```

Sur la commande prête, afficher l'état de la recherche et le bouton
**« Relancer la recherche »** quand la notification est arrivée (ou quand
`GET /stores/{id}/orders` la montre `ready` depuis plus de 15 min sans
livreur). Un marchand qui préfère annuler passe par **Refuser** — possible
jusqu'à `ready` inclus.

### Refuser — v1.3.0

```
POST /stores/{id}/orders/{order_id}/refuse   { reason?: "…" }
```

Possible **jusqu'à `ready` inclus**. Au-delà, un livreur a pris la course et roule vers le comptoir : le laisser arriver devant une commande refusée lui coûte un trajet et un jeton. L'API répond alors `409 order_not_refusable` — **grisez le bouton dès que la commande est partie**, plutôt que de laisser le marchand découvrir le refus du refus.

Le motif est **facultatif** : exiger une justification ferait taper n'importe quoi.

> ⚠️ **Le refus annule la commande ENTIÈRE**, même quand elle traverse plusieurs boutiques. C'est une décision produit assumée, et elle doit être **écrite dans la confirmation** : « Cette commande sera annulée en entier, y compris pour les autres boutiques. » Un marchand qui l'apprend après coup croira à un bug.

> **Le client est remboursé sur son portefeuille Dira** — v1.6.0 — quel que soit le moyen de paiement. En espèces, rien à rendre : personne n'a encaissé.
>
> ⚠️ **La somme ne repart PAS vers l'opérateur mobile money.** Elle arrive sur le solde Dira du client. Dites-le dans la confirmation : un marchand qui annonce au client « vous serez remboursé » sans préciser où crée un appel au support.

### Ce qui se vend — v1.3.0

```
GET /stores/{id}/sales?days=30&limit=20
```

Classé sur les **articles** vendus : trois riz gras sur une commande sont trois plats vendus. `orders` compte les commandes **distinctes** — affichez les deux, c'est ce qui distingue un plat que tout le monde prend d'un plat qu'un seul client commande par dix.

Les commandes **annulées** et **en attente de paiement** sont exclues. Fenêtre par défaut **30 jours**, l'horizon sur lequel on décide quoi garder à la carte.

### Être prévenu

Le gabarit **`merchant_new_order`** part au propriétaire de l'enseigne quand une commande passe à `paid` — **pas à sa création** : tant qu'elle attend un paiement, il n'y a rien à préparer, et sonner pour rien apprend à ignorer la sonnerie.

Chaque boutique reçoit **ce qui la concerne** : nombre d'articles et montant **de ses lignes**. Une commande peut en traverser trois.

### ⚠️ Le moment où l'argent se joue

La réponse porte, **pour ce point de vente uniquement** :

| Champ | Ce que c'est |
|---|---|
| `store_amount` | ce qui revient à cette boutique sur cette commande |
| `cash_to_collect` | ce que le marchand doit **encaisser du livreur**, en espèces — **zéro si prépayée** |

> **Passer une commande à `ready` déclenche la recherche d'un livreur.** C'est le bon moment : plus tôt il attendrait au comptoir, plus tard le repas refroidirait.

> ⚠️ **En espèces, c'est au marchand de s'assurer d'avoir reçu l'argent avant de remettre la commande.** Le livreur avance la somme de la main à la main. Personne ne l'atteste côté serveur : affichez `cash_to_collect` **au moment de la validation**, pas après le retrait, où l'information ne rattrape plus rien.

En **prépayé** (mobile money ou solde Dira), le portefeuille de la boutique est crédité **au retrait** par le livreur — pas à la commande : tant que rien n'est retiré, rien n'a été vendu.

---

## 4 bis. Le personnel — v1.3.0

```
GET    /me/staff
POST   /me/staff       { phone, name, password?, role, store_id? }
PATCH  /me/staff/{id}  { role?, store_id? }
DELETE /me/staff/{id}
```

**Réservé au propriétaire de l'enseigne** — les quatre routes répondent `403` à tout autre, y compris à un gérant. Quelqu'un qui peut se donner des droits n'a plus un rôle, il a tous les rôles : **n'affichez le menu Personnel qu'au propriétaire**.

Un membre est un **compte** : il se connecte avec son propre téléphone et son propre mot de passe. Ajouter un membre **ouvre son compte** s'il n'existe pas — `password` devient requis, le propriétaire le communique de vive voix, le membre le change ensuite. Un téléphone qui appartient à un compte **non marchand** est refusé.

| Rôle | Ce qu'il peut |
|---|---|
| propriétaire | tout — il n'apparaît **pas** dans la liste |
| `manager` | commandes, carte, portefeuille |
| `staff` | **les commandes, et rien d'autre** |

`store_id` **absent** = toute l'enseigne, le cas d'un gérant qui tourne. Renseigné, le membre **n'agit que là** : sans cela, la personne au comptoir de Tokoin verrait les commandes de Bè, et les ferait avancer par erreur.

> **Lisez `capabilities`, ne recodez pas la table des rôles.** Chaque membre porte ce qu'il peut (`orders`, `menu`, `wallet`). Deux tables divergeraient, et l'écran montrerait des boutons que l'API refuse.

> Retirer un membre lui retire ses **droits** — **le compte survit**. Dites-le dans la confirmation : « Cette personne perdra l'accès à votre enseigne. Son compte reste ouvert. » Le supprimer effacerait l'historique de ce qu'elle a fait.

---

## 5. Portefeuille de jetons

**Servi par le SOCLE** — base `…/api/v1/`, sans `/food`.

```
GET  /wallet?store_id={id}
GET  /wallet/transactions?store_id={id}
POST /wallet/purchase          { tokens }
```

> **Un portefeuille par POINT DE VENTE**, pas par enseigne. `store_id` est **obligatoire** pour un marchand : sans lui, `422`.

Deux soldes : `balance` en **jetons**, `balance_xof` en **argent** (le produit des ventes prépayées). **Ne les additionnez jamais**, et n'affichez jamais « solde » sans dire lequel.

Lisez le solde **avant** toute action payante et grisez ce qui n'est pas finançable : `402 insufficient_tokens` au clic est une mauvaise expérience évitable.

La recharge ne crédite qu'**après confirmation** du prestataire (`GET /payments/{id}`) — jamais sur la réponse HTTP de l'initiation.

> ## ⚠️ v2.0.0 — DEUX ROUTES SONT MOMENTANÉMENT ABSENTES
>
> ```
> POST /stores/{id}/dishes/{dish_id}/boost     propulser un plat
> POST /stores/{id}/options                    acheter une option
> ```
>
> **Ces deux routes ne sont servies nulle part pour l'instant.** Elles dépensent
> un portefeuille — désormais au socle — sur des objets de la livraison : un
> plat, un point de vente, une enseigne. Le socle ne connaît ni l'un ni l'autre,
> et la livraison ne tient plus le portefeuille.
>
> Le découpage est identifié — le grand livre au socle, la propulsion à la
> livraison — et pas encore fait. **N'affichez pas ces boutons** tant que cette
> spec ne dit pas le contraire ; les câbler sur l'ancienne base rendrait `404`.
>
> Le reste du portefeuille — solde, historique, recharge — fonctionne
> normalement.

---

## 6. Vidéos et feed

```
POST   /stores/{id}/videos
GET    /stores/{id}/videos
DELETE /stores/{id}/videos/{video_id}
PATCH  /stores/{id}/videos/{video_id}/schedule   { published_at }
```

La **génération de vidéo par IA** débite le portefeuille **de ce point de vente**.

Une vidéo **fournie** se téléverse d'abord — `POST /api/v1/uploads?kind=feed&entity={store_id}`, au **socle** (v3.0.0) — puis se déclare avec `source: "upload"`, `video_url` et le `duration_seconds` rendu par l'envoi. `feed` est le seul `kind` qui accepte une vidéo (mp4, webm, mov ; ≤ 60 MiB **et ≤ 60 s**, lus dans le fichier : `422 video_too_long` ou `video_unreadable` sinon). Le serveur réencode ensuite en arrière-plan et pose la vignette.

**Publication programmée** : `published_at` porte une date **et une heure**. Une vidéo dont `published_at` est dans le futur n'apparaît pas dans le feed — elle est **filtrée**, pas classée en dernier.

Le feed public et ses interactions :

```
GET  /feed · POST /feed/{id}/view · /click · /share
GET  /feed/{id}/comments · POST /feed/{id}/comments · DELETE /feed/{id}/comments/{comment_id}
PUT  /feed/{id}/reaction · GET /feed/{id}/reaction
```

---

## 7. Avis reçus

```
GET /stores/{id}/ratings     (public)
GET /dishes/{id}/ratings     (public)
```

- Le **point de vente** est noté, pas l'enseigne : une commande peut traverser deux boutiques d'une même marque, et le client a vécu l'une et pas l'autre.
- Les **plats** se notent indépendamment de qui les a faits : un plat raté chez un bon restaurant se dit.
- **La moyenne ne s'affiche jamais sans son nombre d'avis.** Zéro avis n'est pas une mauvaise note.
- ⚠️ **Le marchand ne peut pas répondre** à un avis.

---

## 8. Compte, fichiers, support

```
POST /auth/login { phone | email, password, app: "merchant" }   (SOCLE) ⚠️ v4.26.0
GET /me · PATCH /me · PATCH /me/preferences        (SOCLE, sans /food)
POST /uploads?kind=dish&entity={id}                (SOCLE, sans /food)
POST /me/devices · GET /me/notifications           (SOCLE, sans /food)
POST /tickets · GET /tickets · GET /tickets/{id} · POST /tickets/{id}/messages · POST /bug-reports
```

**Le support (v4.9.0)** : `POST /tickets { category, message }`, `category`
∈ `tokens` (jetons, propulsions) · `payment` · `account` · `order`
(`order_id` d'une commande passée chez vous — `403` sinon) · `behaviour` ·
`other`. Le ticket rendu porte `reference` (`TCK-000123`, à afficher),
`status` (`open` · `in_progress` · `waiting` · `resolved` · `closed`) et un
fil `messages[]` ; le support répond (**`ticket_reply`**) et clôt
(**`ticket_resolved`**) — catégorie `support`, non coupable. `lost_item`
n'est pas pour vous : c'est le parcours client ↔ livreur.

⚠️ **`POST /uploads` est au socle — v3.0.0** : `…/api/v1/uploads`, plus `/food/uploads` (404). Multipart, champ `file`, le **type déclaré** de la part fait foi.

`kind` ∈ `dish` · `store` · `brand` · `vehicle` · `avatar` · `feed` · `banner` · `class` (pictogrammes des modes VTC, console) · `equipment` (photos du matériel, console). Images (jpeg, png, webp, svg) ≤ 5 MiB pour tout `kind` ; `feed` accepte **aussi** des vidéos ≤ 60 MiB — une vidéo pèse bien plus qu'une photo de plat, et rien d'autre ne profite de ce plafond. Au-delà de 64 MiB, la passerelle répond `413 payload_too_large`.

Téléversez **d'abord**, rattachez l'URL ensuite : une image qui échoue ne doit pas faire perdre la saisie.

### 🚪 DIRE QUELLE APPLICATION SE CONNECTE — `app` (v4.26.0)

`POST /auth/login` accepte un champ `app`. **Envoyez `app: "merchant"` à chaque
connexion**, à côté du téléphone et du mot de passe :

```
POST /auth/login   { phone | email, password, app: "merchant" }
```

⚠️ **Ce qui se passait sans lui** : un client ou un livreur se connectait ici, et rien ne
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

Affichez « Ce compte est un compte client. Ouvrez l'application Dira
(client). » — **jamais « identifiants invalides »** : le mot de passe était
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

La famille `merchant`, elle, reste tenue à l'écart des autres, comme avant.

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
livreurs**, jamais un marchand.

⚠️ **N'ENVOYEZ PAS `device_id`, ET NE CODEZ RIEN POUR CETTE RÈGLE.** Une enseigne tient sa caisse sur un écran, sa cuisine sur un autre, et son gérant regarde depuis son téléphone — trois sessions du même compte, toutes légitimes.
Le champ est ignoré pour votre public, le jeton émis ne nomme aucun appareil, et
**aucune** de vos sessions ne peut être fermée par une connexion ailleurs.

Concrètement :

- Vous **ne recevrez jamais** `401 session_superseded`, ni sur
  `POST /auth/refresh`, ni sur un appel authentifié : inutile de le traiter.
- Se connecter sur un second appareil **n'invalide pas** le premier : les trois jetons de rafraîchissement continuent de vivre côte à côte, chacun avec sa propre rotation.
- La réponse de `POST /auth/login` ne porte **pas** d'objet `session` pour vous.

**Pourquoi c'est écrit ici alors que ça ne vous concerne pas.** La règle existe parce que deux
téléphones connectés sur un compte de chauffeur poussent **deux flux de
positions pour un seul véhicule**, et que le vivier d'appel le lit comme deux
véhicules. Rien de tel chez un marchand, dont aucun écran ne pousse de position — et l'appliquer « par prudence » à un public qui
n'a pas ce problème coûterait des déconnexions quotidiennes pour rien.


---

## 8 bis. ⚠️ Ce que la maquette demande et que l'API ne sert pas

Relevé sur la maquette du **10 septembre 2026**, qui ajoute deux écrans de personnel (`m_staff`, `m_assign`).

### 🟠 Le personnel — quatre rôles dessinés, deux servis

La maquette porte `roleManager`, `roleKitchen`, `roleTill` et `roleDriver`. L'API en connaît **deux** :

| Maquette | API | Remarque |
|---|---|---|
| `roleManager` | `manager` | commandes, carte, portefeuille |
| `roleKitchen` | `staff` | ⚠️ **mêmes droits** que la caisse |
| `roleTill` | `staff` | ⚠️ **mêmes droits** que la cuisine |
| `roleDriver` | ❌ **rien** | un livreur n'appartient à aucune enseigne |

> **Cuisine et caisse ont exactement les mêmes droits** : voir les commandes et les faire avancer. Vous pouvez garder deux libellés à l'écran — c'est utile à qui lit la liste — mais **envoyez `staff` pour les deux**, et n'en déduisez aucune différence de permissions. Deux rôles qui accordent la même chose sont un libellé, pas une autorisation.
>
> ⚠️ **`roleDriver` n'a rien derrière, et c'est structurel.** Sur cette plateforme, un livreur est **indépendant** : il achète ses jetons, choisit ses courses, et n'appartient à aucune enseigne. Un livreur salarié d'un marchand ne se représente pas — le modèle économique s'y oppose. **Retirez ce rôle de l'écran**, ou posez la question comme une décision produit.

### ❌ Le téléphone d'un membre

La liste dessinée montre un numéro par membre. `GET /me/staff` rend `name`, `role`, `store_id`, `capabilities` — **pas de téléphone**.

> Le téléphone sert à **ouvrir** le compte (`POST /me/staff`), il n'est pas rendu ensuite. C'est délibéré : la liste du personnel n'est pas un annuaire, et un propriétaire qui la consulte cherche des **droits**, pas des contacts. Si l'écran en a réellement besoin, c'est un ajout à demander — pas un champ à deviner.

Sur une carte de commande : le livreur est `courier`, votre point de vente `merchant`, l'adresse de remise `client`.

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

**Sur VOS cartes.** Votre point de vente se dessine avec le pin principal du
**marchand**. Quand une commande est collectée chez plusieurs enseignes, le
livreur voit des pins numérotés — **votre rang dans sa tournée** —, et c'est
ce rang qui explique l'heure à laquelle il arrive chez vous.

### ❌ Le nom du livreur sur la carte de commande

La carte dessinée annonce « Livreur affecté · *Mamadou D.* ». L'API ne rend **aucune identité de livreur** au marchand.

> Le marchand a besoin de savoir **qu'un livreur vient** — ce que le statut `accepted` dit déjà — plus que de savoir **qui**. Affichez « Livreur affecté » sans nom tant que ce champ n'existe pas.

### ❌ La progression de préparation

La carte dessinée porte une barre de progression et un temps restant (« prêt dans 6 min »). Aucune commande ne porte de **durée de préparation**, ni prévue ni écoulée.

> Rien n'oblige non plus un marchand à en annoncer une. C'est une décision produit avant d'être un champ : faut-il demander au marchand « combien de temps ? » à l'acceptation, au risque qu'il tape toujours la même valeur ?

### ❌ Le compte à rebours de 30 s — toujours rien derrière

Inchangé depuis la v1.3.0, et toujours présent dans le prototype (`urgentCd: 30`).

> **La sélection du point de vente se fait à la CRÉATION de la commande**, parce que le prix et les frais de livraison en dépendent et doivent être connus avant le paiement. Le marchand n'a donc **aucun délai** pour « prendre » une commande : elle est déjà la sienne.
>
> Depuis la v1.4.0, le sous-titre « Vous êtes le point de vente sélectionné » est **vrai** — c'est bien le serveur qui a retenu ce comptoir, pour sa proximité avec l'adresse de livraison. **Gardez le sous-titre, retirez le chronomètre.**

---

## 9. Codes d'erreur à traiter nommément

| Code | HTTP | Conduite |
|---|---|---|
| `missing_token` · `invalid_token` | 401 | refresh, puis déconnexion au second échec |
| `forbidden` | 403 | la boutique n'appartient pas à ce compte |
| `insufficient_tokens` | **402** | proposer la recharge du portefeuille **de cette boutique** |
| `*_not_found` | 404 | `store_`, `dish_`, `merchant_`, `order_` |
| `invalid_transition` | 409 | transition de statut interdite |
| `validation_failed` | 422 | **`fields`** nomme les clés JSON fautives (`store_id`, bornes d'un groupe d'options, prix ≤ 0) ; `reason: unknown_field` = bug de l'app |
| `video_too_long` · `video_unreadable` | 422 | vidéo de feed > 60 s, ou conteneur illisible → réexporter en MP4 (H.264) |
| `payload_too_large` | **413** | la passerelle : corps > 64 MiB — vérifier le poids **avant** d'envoyer |
| `storage_unavailable` | 503 | le stockage de fichiers n'a pas démarré → réessayer, ne pas perdre la saisie |
| `account_closed` | **403** | le support a **fermé** ce compte — fin de session définitive : vider le jeton, afficher le message tel quel |
| `erasure_not_self_serve` | 403 | `DELETE /me` n'est pas pour un marchand → renvoyer vers le support |

---

## 10. Points ouverts

1. **Aucune note d'enseigne** n'est calculée : seuls les points de vente en portent une.
2. **Aucune modération** des commentaires du feed ni des avis côté marchand.
3. **Pas d'horaires structurés** : `is_open` est un booléen, pas un calendrier d'ouverture.
4. **Aucune gestion de stock** : `available` est binaire, sans quantité.
5. **Le catalogue est plat** : pas de menus composés ni de formules.
