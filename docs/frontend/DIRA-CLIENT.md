# App CLIENT UNIFIÉE — LIVRAISON **et** COURSES — contrat d'API

> **Version 4.52.0** · 8 octobre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Livraison : `…/api/v1/food` · Courses : `…/api/v1/vtc` · Combiné : `…/api/v1/analytics` · Suivi : `wss://tracking-staging.dira.llc`

---

> ## Ce que cette application est
>
> **Une seule application pour un client Dira**, qui commande à manger **et**
> prend des taxis. Un compte, un portefeuille, une boîte de notifications, un
> historique — et deux métiers à l'intérieur.
>
> Jusqu'ici c'étaient deux applications (`VTC-CLIENT`, `FOOD-CLIENT`), parce que
> le serveur sert deux API. Ce document décrit **l'application qui les réunit** :
> ce qui est partagé, ce qui est nouveau parce qu'on les réunit, et ce qui ne
> change pas.
>
> ### ⚠️ Ce que ce document contient, et ce qu'il ne contient pas
>
> | | |
> |---|---|
> | **En entier, ici** | les bases d'URL, les conventions, le pays, le fil de requête, le fond de carte, l'identité et l'argent, **l'assistant unifié**, **le fil d'activité**, les deux canaux temps réel, la boîte de notifications des deux métiers, les codes d'erreur |
> | **Inchangé, dans la spec du métier** | commander un repas (catalogue, panier, livraison) → `FOOD-CLIENT` · commander une course (devis, modes, suivi du chauffeur) → `VTC-CLIENT` |
>
> ⚠️ **ET C'EST UN CHOIX, PAS UN OUBLI.** Recopier ici les deux mille cinq cents
> lignes des deux parcours métier aurait fait **deux sources de vérité pour les
> mêmes routes** — et elles auraient divergé au premier changement, sans que
> personne ne sache laquelle croire. Le §10 donne l'inventaire **route par
> route** de ce qui vit où, pour qu'on n'ait jamais à chercher.
>
> Tout ce qui est **transverse ou combiné** est écrit ici **en entier** : c'est
> précisément ce qu'une application unifiée doit tenir, et ce qu'aucune des deux
> specs métier ne peut dire à votre place.

---

## 1. ⚠️ QUATRE BASES D'URL, et se tromper est SILENCIEUX

| Ce que vous appelez | Service | Base |
|---|---|---|
| connexion, profil, adresses, **portefeuille**, paiements, notifications, fichiers, pays | **socle** | `…/api/v1/…` |
| catalogue, panier, commandes, nutrition, tombola | **livraison** | `…/api/v1/food/…` |
| devis, courses, classes de véhicule, abonnements | **courses** | `…/api/v1/vtc/…` |
| **l'assistant unifié et le fil d'activité** (§8, §9) | **combiné** | `…/api/v1/analytics/…` |

**Le jeton est le même partout** : même secret, même session. On ne se connecte
pas quatre fois.

⚠️ **UN APPEL SUR LA MAUVAISE BASE COMPILE, PASSE LES TESTS, ET REND `404`
DEVANT UN CLIENT.** `GET /api/v1/orders` n'existe pas ; c'est
`GET /api/v1/food/orders`. Prévoyez **quatre** variables de configuration, et
une règle vérifiable à la construction : d'où vient cette route ? Une
application qui dérive la base d'une table de chemins finira par en oublier un.

⚠️ **POURQUOI LE COMBINÉ EST SOUS `/analytics`.** C'est le service qui détient
le modèle de langage de la plateforme **et** qui interroge déjà les deux
métiers ; le socle, lui, a pour règle de ne rien demander à une verticale. Le
nom du préfixe décrit le service, pas votre écran : lisez-le comme « le service
qui compose ».

---

## 2. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | `?limit=20&cursor=<id>` → `{ "items": [...], "next": "…" }` |
| Montants | **entiers**, dans la monnaie du pays. Jamais de flottant. |
| Dates | ISO 8601 UTC. Une **date seule** s'écrit `YYYY-MM-DD`. |
| Langue | `Accept-Language` |
| Pays | `X-Dira-Country: TG` — sur chaque requête (§4) |
| Fil | `X-Request-ID` — sur chaque réponse (§3) |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ;
le code est le contrat.

**Un `422 validation_failed` nomme ses champs.** `fields` liste les **clés
JSON** en cause : soulignez **ces** cases, pas une bannière sous tout le
formulaire. `reason` précise quand ce n'est pas la valeur d'un champ :
`unknown_field` (une clé que la route ne connaît pas — **refusée, pas
ignorée** : c'est un bug de l'application) ou `invalid_json`.

⚠️ **L'ENVELOPPE NE REND QUE `code`, `message`, `fields` ET `reason` — il n'y a
pas de `meta` sur le fil.** Quand un refus porte des chiffres (« ce trajet fait
2 100 m, la course partagée commence à 3 000 m »), ils sont **dans la phrase**,
déjà traduite. Affichez-la telle quelle, ou recomposez-la depuis les réglages
que vous avez déjà lus.

---

## 3. 🧵 LE FIL D'UNE REQUÊTE — `X-Request-ID`

Chaque réponse porte `X-Request-ID`. À l'aller il est facultatif : si vous en
envoyez un, il est **repris** ; sinon le serveur en fabrique un.

**Ce qu'on vous demande : le montrer, et le joindre.** Un identifiant affiché
en petit sur un écran d'erreur, et joint à un rapport de bogue, permet de
retrouver la requête exacte dans les journaux des **quatre** services —
y compris les appels que le service combiné a faits aux deux métiers en votre
nom.

⚠️ **CELA COMPTE DOUBLE POUR UNE APPLICATION UNIFIÉE.** Un « ça ne marche
pas » sur l'assistant peut venir de trois services ; sans le fil, il faut
chercher dans trois journaux sans savoir dans lequel regarder.

---

## 4. ⚠️ Le PAYS — `X-Dira-Country`

Chaque compte, commande et course porte un pays. **Envoyez l'en-tête sur
chaque requête** ; la réponse porte le pays retenu.

**Les pays ouverts** : `GET /countries` (socle, public). Chaque entrée porte
`code`, `name`, `currency`, `phone_prefix`, `locale`, `timezone`, `basemap`
(§5) et `testing` (un pays d'essai, à ne pas montrer).

⚠️ **LA MONNAIE VIENT DU PAYS, PAS DU COMPTE.** Un montant est un entier dans
la monnaie de son pays : un Togolais qui commande à Dakar voit des francs CFA
du Sénégal, et à Conakry des francs guinéens. Une application qui formaterait
avec la monnaie du **compte** écrirait le bon nombre derrière le mauvais
symbole.

⚠️ **UNE OPÉRATION SE FAIT LÀ OÙ ELLE SE FAIT.** Le pays d'une course vient de
son **point de départ**, celui d'une commande de l'enseigne. Un client en
voyage commande normalement — et son numéro garde son indicatif.

⚠️ **LE PORTEFEUILLE NE TRAVERSE PAS UNE MONNAIE.** Un solde est un entier dans
la monnaie de son pays : payer au solde une course facturée en francs guinéens
depuis un solde en francs CFA est **refusé**. Les espèces et le paiement en
ligne, eux, restent ouverts. Entre deux pays de même monnaie (Togo et Sénégal,
Gabon et Tchad), le solde marche comme chez soi.

---

## 5. 🗺️ LE FOND DE CARTE — `basemap`

Les deux métiers affichent des cartes. Le serveur ne répond qu'à **une**
question : dans ce pays, quel fond montre-t-on d'abord ?

```jsonc
// à la connexion, à l'inscription, au rafraîchissement
"maps": { "basemap": "dira" | "google" }
// et dans le catalogue PUBLIC des pays
GET /countries → items[].basemap
```

**La règle du fond effectif, dans cet ordre** : le **choix de la personne**,
sinon le `basemap` du pays où elle **opère**, sinon `dira`.

⚠️ **LISEZ LES DEUX ENDROITS, ET IL FAUT LES DEUX.** Le bloc `maps` de
l'authentification voyage avec le jeton : il dit le fond du pays où l'on s'est
**connecté**, une fois. Une application qui ne lirait que lui garderait le fond
de la veille après un changement de pays.

⚠️ **AUCUNE CLÉ GOOGLE NE VIENT DU SERVEUR.** Une clé se restreint par ce qui
l'utilise (empreinte SHA-1 + paquet sur Android, bundle sur iOS) : elle
appartient à l'application. Proposez **toujours les deux** fonds.

⚠️ **LE FOND CHANGE ⇒ LA CARTE DOIT RENAÎTRE.** Les SDK lisent leur style à la
création ; changer la valeur sans reconstruire la vue ne se voit pas. C'est le
défaut qu'a eu la console : le logo d'un fournisseur posé sur les tuiles de
l'autre.

---

## 5 bis. 🔒 LE VERROU DE L'APPLICATION — `app_lock` (v4.46.0)

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

## 6. L'identité et l'argent — **PARTAGÉS** (socle, sans préfixe)

C'est tout l'intérêt d'une application unifiée : **une** inscription, **un**
solde, **une** boîte.

```
POST /auth/otp · POST /auth/otp/verify          ← la porte par CODE (v4.46.0)
POST /auth/register · POST /auth/login · POST /auth/refresh · POST /auth/logout
GET /me · GET /me/addresses · POST /me/devices
GET /wallet · GET /wallet/transactions
POST /payments/initiate · GET /payments/{id} · GET /payments/providers
GET /notifications · GET /countries · POST /uploads
GET /me/referral                                ← mon code de PARRAINAGE (v4.51.0)
```

### 🆕 6 bis. S'INSCRIRE ET SE CONNECTER PAR CODE — le téléphone, et rien d'autre (v4.46.0)

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

---

## 🗑️ 6 ter. SUPPRIMER SON COMPTE — et ce qui reste des courses passées (v4.47.0)

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

---

## 6 quater. 🎟️ LES CODES PROMO ET LE PARRAINAGE — un champ, deux métiers (v4.51.0)

**Un code est au SOCLE, et il vaut des deux côtés.** Le même mot ne peut pas
valoir une chose sur une course et une autre sur une commande : une enveloppe,
un suivi, une vérité. C'est exactement ce qu'une application unifiée doit
montrer — **un seul champ « code promo »**, placé au même endroit dans les deux
tunnels.

| Où on le saisit | Ce qu'on envoie | Ce qui revient |
|---|---|---|
| devis d'une course | `POST /rides/quote { …, "code": "DIRA2000" }` | `promo_code`, `promo_code_xof` **par classe**, `promo_code_ignored` |
| commande de repas | `POST /orders { …, "code": "DIRA2000" }` | `promo_code`, `promo_code_xof`, `total` déjà remisé |

> ⚠️ **LE PRIX SERVI EST DÉJÀ REMISÉ** des deux côtés — `fare_xof`, `total`. Ne
> soustrayez **rien** : le montant du code est là pour être **affiché**.

> ⚠️ **MAIS LES DEUX MÉTIERS NE REFUSENT PAS AU MÊME MOMENT, et l'écran doit en
> tenir compte.** Sur une course, le refus arrive **au devis** : le passager
> corrige son code avant de commander, rien n'est perdu. Sur une commande, il
> arrive **à la création** — la commande n'est pas créée. Un panier composé
> pendant dix minutes ne doit pas disparaître sur une faute de frappe : gardez-le
> à l'écran, et laissez réessayer ou commander sans code.

> ⚠️ **UN CODE PEUT VALOIR POUR UN SEUL MÉTIER.** `promo_code_wrong_service`
> arrive avec `meta.valid_for` : dites **lequel** (« ce code est pour les
> commandes »), et, puisque vous portez les deux onglets, proposez d'y aller.
> C'est l'avantage que seule une application unifiée peut offrir ; ne le gaspillez
> pas en affichant « code invalide ».

> ⚠️ **`promo_code_ignored` N'EXISTE QUE SUR UNE COURSE** : il dit qu'un code
> valable a été battu par une offre automatique. Ne le traitez pas comme un refus
> — « gardez-le, une meilleure offre s'applique déjà ». Côté commande, ce cas
> n'existe pas.

**Le parrainage**, lui, est un écran unique du compte — il ne dépend d'aucun
métier :

```
GET /me/referral
→ { "enabled": true, "code": "AWA7K2M",
    "invitee_xof": 1000, "sponsor_xof": 500, "max_sponsored": 10, "uses": 3 }
```

> ⚠️ **`enabled` EST EXPLICITE : NE LE DÉDUISEZ PAS DE L'ABSENCE DE CODE.** Le
> parrainage se règle **pays par pays** et il est **éteint par défaut** : à faux,
> **cachez l'entrée** « inviter un ami » plutôt que d'afficher un écran vide.

> ⚠️ **DEUX MONTANTS, DEUX BÉNÉFICIAIRES.** `invitee_xof` est une **remise** pour
> celui qui arrive, `sponsor_xof` un **crédit** pour celui qui invite. « Gagnez
> 1 500 F » fait attendre 1 500 F à tout le monde et produit deux déceptions.

> ⚠️ **LE PARRAIN EST PAYÉ QUAND LA REMISE EST CONSOMMÉE** — course terminée,
> commande livrée —, jamais à la saisie. Dites-le : sinon quelqu'un qui voit son
> filleul commander et son solde ne pas bouger croira que rien ne marche. Et
> comme le filleul peut dépenser son code **dans l'un ou l'autre métier**, le
> crédit peut arriver d'un onglet que le parrain ne regardait pas — votre fil
> d'activité (§9) est le bon endroit pour le dire.

Détail des neuf refus et de leurs suites : `VTC-CLIENT` §3 et `FOOD-CLIENT` §4.

## 7. ⚠️ La navigation : un onglet par métier, et ce que ça change

Trois décisions qui n'existaient pas quand il y avait deux applications :

1. **L'onglet est un CONTEXTE, et il vaut de l'or.** Quand la personne vient de
   toucher « manger », dites-le à l'assistant (`vertical: "food"`, §8) : c'est
   le meilleur signal qui existe, il est gratuit, et il évite une question.
2. **Le pays suit l'OPÉRATION, pas l'onglet.** Un client à Dakar commande un
   repas dakarois et une course dakaroise ; ne gardez pas un pays par onglet.
3. **L'accueil n'a pas d'onglet.** Sur un écran où les deux métiers cohabitent,
   **n'envoyez pas** `vertical` : c'est au serveur de trancher.

---

## 8. 🆕 L'ASSISTANT UNIFIÉ — un champ de saisie, deux métiers (v4.43.0)

```
POST /analytics/ai/chat
{ "message": "emmène-moi à l'aéroport",
  "origin": [9.45, 0.41],        // facultatif — la position du téléphone
  "vertical": "vtc" | "food" }   // facultatif — voir plus bas
  →
{ "vertical": "vtc",             // QUI a répondu
  "reply": "Voici deux options depuis votre position.",
  "plan": { … },                 // le plan du métier, VERBATIM
  "routed_by": "lexical" }
```

« J'ai faim » et « emmène-moi à l'aéroport » arrivent par le **même** champ. Le
serveur tranche de quel métier il s'agit, délègue à l'assistant de ce métier-là,
et vous rend sa réponse **telle quelle**.

### `plan` est celui du métier qui a répondu

⚠️ **DEUX FORMES, ET `vertical` DIT LAQUELLE.** Nous ne retouchons pas le plan :
le réécrire aurait fait deux sources de vérité pour un prix.

```jsonc
// vertical: "food" — des lignes VÉRIFIÉES contre le catalogue
"plan": { "items": [ { "dish_id": "…", "store_id": "…", "name": "Poulet braisé",
                       "qty": 2, "price": 2700 } ],
          "unresolved": ["pastels"] }

// vertical: "vtc" — des lieux trouvés et des devis calculés
"plan": { "stops": [ { "kind": "pickup", "label": "Tokoin", "geo": [1.21, 6.17] },
                     { "kind": "dest", "label": "Aéroport", "geo": [1.25, 6.16] } ],
          "quotes": [ { "id": "…", "class_key": "eco", "fare_xof": 2500 } ],
          "unresolved": [], "when": "" }
```

⚠️ **UN PLAN NE COMMANDE RIEN.** Il se confirme par la route du métier —
`POST /food/orders` avec les lignes, `POST /vtc/rides { quote_id }` avec un
devis — exactement comme s'il avait été composé à la main. **Jamais** de texte
libre qui devient une commande.

⚠️ **`unresolved` SE DIT, IL NE SE TAIT PAS.** Ce que le catalogue ou la carte
n'a pas trouvé est rendu à part : proposez une recherche. Le taire ferait une
commande qui ne contient pas ce qu'on a demandé.

### ⚠️ Quand le serveur NE SAIT PAS — et il a le droit

```jsonc
{ "reply": "Vous voulez une course, ou quelque chose à manger ?",
  "choices": ["vtc", "food"] }      // et PAS de `vertical`
```

**`vertical` absent = personne n'a répondu.** Posez la question : deux boutons
sur `choices`, et renvoyez le **même message** avec `vertical` renseigné.

⚠️ **ON NE DEVINE PAS, ET C'EST DÉLIBÉRÉ.** Envoyer « commande-moi quelque
chose » au hasard ferait livrer un repas à quelqu'un qui attend un taxi — et il
le découvrirait au prix. Une application qui choisirait elle-même « au pif »
pour éviter une question referait exactement cette erreur.

⚠️ **`reply` EST TOUJOURS REMPLI**, même sans `vertical` : si vous n'avez pas
prévu `choices`, affichez-le et vous aurez déjà une question sensée.

### `routed_by` — comment on a tranché

| Valeur | Ce qui a décidé |
|---|---|
| `app` | **vous** l'avez dit (`vertical` dans la requête) |
| `lexical` | le **vocabulaire** du message : un seul métier y était nommé |
| `model` | le **modèle**, parce que le vocabulaire ne suffisait pas |

⚠️ **SERVI POUR QU'UN ACHEMINEMENT FAUTIF SOIT EXPLICABLE.** Joignez-le à un
rapport de bogue avec `X-Request-ID` : « j'ai été envoyé au mauvais métier » ne
se diagnostique pas autrement.

⚠️ **ENVOYEZ `vertical` DÈS QUE VOUS LE SAVEZ.** C'est gratuit, c'est exact, et
cela évite un appel de modèle — donc une dépense, une latence, et un risque
d'erreur. Ne l'envoyez **pas** depuis un accueil où les deux métiers cohabitent.

### `origin`

La position du téléphone. Elle sert de **départ** aux courses quand le passager
n'en nomme pas — « je vais à l'aéroport » part d'ici. La livraison l'ignore :
elle a les adresses du compte.

### Les refus

| Refus | Ce que ça veut dire | Ce que vous affichez |
|---|---|---|
| `403 ai_disabled` | l'exploitation a coupé l'assistant | **masquez la boîte de dialogue** — ne la laissez pas échouer |
| `429 ai_quota_reached` | cette personne a épuisé son quota du jour | « réessayez demain », et proposez la composition à la main |
| `429 ai_budget_reached` | la plateforme a atteint son plafond du jour | idem, sans accuser la personne |
| `503 assistant_unavailable` | aucun métier joignable | masquez la boîte |

⚠️ **UN QUOTA ATTEINT N'EST PAS UNE PANNE.** Le refus du métier est reporté
**tel quel**, code et phrase déjà traduite : affichez la phrase, et laissez
toujours un chemin manuel.

### 🎙️ Le vocal

Il n'y a **pas** de route de transcription combinée : utilisez celle du métier
que vous visez (`POST /food/ai/voice` ou `POST /vtc/ai/voice`), ou celle de
l'onglet d'où vient la personne.

⚠️ **UN VOCAL N'EST JAMAIS EXÉCUTÉ DIRECTEMENT.** La transcription revient à
l'application, qui l'**affiche** et laisse **corriger** avant de l'envoyer à
`POST /analytics/ai/chat`. Les accents d'ici ne pardonnent pas : « riz sauce »
transcrit « riz gras » se répare avant la commande, pas après.

---

## 9. 🆕 LE FIL D'ACTIVITÉ — courses et commandes, par semaine (v4.43.0)

```
GET /analytics/activity?weeks=4&before=2026-10-05T00:00:00Z
```

```jsonc
{
  "timezone": "Africa/Libreville",
  "sources": { "vtc": "ok", "food": "ok" },
  "weeks": [
    { "start": "2026-10-05T00:00:00+01:00",
      "end":   "2026-10-11T23:59:59+01:00",
      "iso_year": 2026, "iso_week": 41,
      "totals": { "vtc":  { "count": 2, "amount": 4500 },
                  "food": { "count": 1, "amount": 5400 } },
      "items": [
        { "vertical": "vtc", "id": "…", "at": "2026-10-07T18:00:00Z",
          "status": "completed", "amount": 2500, "currency": "XAF",
          "details": { "mode": "pool", "pickup": "Tokoin",
                       "dropoff": "Aéroport", "distance_m": 9800 } },
        { "vertical": "food", "id": "…", "at": "2026-10-06T12:00:00Z",
          "status": "completed", "amount": 5400, "currency": "XAF",
          "details": { "first_item": "Poulet braisé", "lines": 2,
                       "items_count": 3, "payment_method": "wallet" } }
      ] }
  ],
  "next_before": "2026-10-05T00:00:00+01:00",
  "has_more": true
}
```

### ⚠️ LE PAS DE PAGINATION EST LA SEMAINE, jamais la ligne

Redonnez **`next_before`** pour la page suivante. `weeks` dit combien de
semaines vous voulez (défaut **4**, plafond **12** — au-delà, plafonné **en
silence**, et `has_more` vous dit de continuer).

⚠️ **UNE SEMAINE N'EST JAMAIS COUPÉE ENTRE DEUX PAGES**, et c'est pour ça que
le pas n'est pas la ligne : un en-tête « 3 courses · 12 000 F » servi sur la
moitié d'une semaine mentirait.

⚠️ **LES SEMAINES VIDES SONT SAUTÉES.** Entre deux commandes espacées d'un
mois, vous recevez **deux** semaines, pas les cinq qui les séparent. Une
application qui compterait les semaines pour afficher un calendrier doit lire
`start` et `iso_week`, pas l'index dans la liste.

### ⚠️ LES SEMAINES SONT DÉCOUPÉES DANS LE FUSEAU DU PAYS

Lundi 00:00 → dimanche 23:59:59, **dans `timezone`** — pas en UTC.

Le Gabon et le Tchad sont à UTC+1 : une course prise **lundi 00 h 30** à
Libreville a eu lieu dimanche 23 h 30 en UTC, et un découpage en UTC la
rangerait dans la semaine **précédente**. `start` et `end` sont servis **avec
leur décalage** ; `timezone` est là pour que vous affichiez les mêmes bornes
que nous.

⚠️ **`iso_year` N'EST PAS TOUJOURS L'ANNÉE DE LA DATE.** Le 29 décembre 2025
appartient à la **semaine 1 de 2026**. Afficher l'année du calendrier à côté du
numéro ISO donnerait « semaine 1 de 2025 ».

### ⚠️ `sources` — ce qui a répondu, et ce qui n'a pas répondu

`"ok"` ou `"unavailable"`, par métier.

Un métier muet **ne vide pas** l'historique de l'autre : vous recevez ce qui
existe. Mais **dites-le** — « vos commandes n'ont pas pu être chargées » — car
un fil où la moitié manque sans le dire se lit comme un fil **complet**, et le
client croit avoir perdu ses commandes.

⚠️ **UN JETON REFUSÉ REFUSE LE FIL ENTIER** (`401`), et c'est voulu : servir la
moitié d'un historique à quelqu'un qu'on n'a pas pu identifier serait pire que
de refuser.

### ⚠️ C'est un FIL, pas l'objet

Une ligne porte de quoi **dessiner une ligne** : le métier, l'instant, le
statut **du métier** (verbatim), le montant, et des **faits** dans `details`.

| `vertical` | `details` |
|---|---|
| `vtc` | `mode`, `pickup`, `dropoff`, `distance_m` |
| `food` | `first_item`, `lines`, `items_count`, `payment_method` |

⚠️ **DES FAITS, PAS UNE PHRASE, ET C'EST À VOUS D'ÉCRIRE LA PHRASE.** Fabriquer
« Course vers l'aéroport » côté serveur aurait demandé de traduire, de choisir
une casse et une longueur — trois décisions d'écran prises loin de l'écran.

⚠️ **`details` PEUT ÊTRE ABSENT OU INCOMPLET.** Une course sans adresse tapée
n'a pas de `pickup`. N'affichez pas un tiret là où il n'y a rien à dire.

⚠️ **POUR LE DÉTAIL, ALLEZ CHEZ LE MÉTIER** — `GET /vtc/rides/{id}`,
`GET /food/orders/{id}`. Ils sont la **seule** source de vérité : le fil ne
porte ni la répartition de l'argent, ni le parcours, ni la conversation.

---

---

## 9 bis. 🧾 LES REÇUS ET LES RELEVÉS, EN PDF (v4.52.0)

Quatre routes, deux de chaque côté, et une seule entrée dans l'application :

| | Reçu d'une opération | Relevé d'une période |
|---|---|---|
| courses | `GET /rides/{id}/receipt.pdf` | `GET /rides/statement.pdf?from=&to=` |
| commandes | `GET /orders/{id}/receipt.pdf` | `GET /orders/statement.pdf?from=&to=` |

`application/pdf`, `attachment`, `no-store`, nom de fichier déjà daté. **Ne les
ouvrez pas dans une WebView** : laissez le système les enregistrer ou les
partager.

Les deux documents portent la **distance réellement parcourue** — mesurée sur le
trajet de l'agent — et **nomment leur source** : `tracked` mesurée, `planned`
estimée quand le suivi n'a rien gardé. Le second cas est **courant**, pas rare.

> ⚠️ **LE DÉTAIL S'ADDITIONNE JUSQU'AU TOTAL, ou le serveur ne sert rien** (500).
> Un reçu dont les lignes ne tombent pas juste prouve une erreur, sous notre nom,
> devant le comptable du client.

> ⚠️ **ET DEUX RELEVÉS, PAS UN.** C'est le seul endroit où l'application unifiée
> ne peut pas réunir les deux métiers, et il vaut mieux le savoir avant de dessiner
> l'écran : un relevé mêlé exigerait un document unique calculé par un service qui
> voit les deux, et il n'existe pas. Proposez donc **deux exports** dans l'écran
> d'activité (§9) — « mes courses » et « mes commandes » — plutôt qu'un bouton qui
> promettrait un seul fichier.

> ⚠️ **`from` ET `to` OBLIGATOIRES, SANS DÉFAUT**, des deux côtés : un défaut
> implicite produirait une somme sur une autre période que celle demandée. Et les
> deux **refusent au lieu de tronquer** — `409 statement_too_large` au delà de 500
> opérations ou de 366 jours, avec `meta.max_rows`. Proposez de découper la
> période.

Détail de ce que chaque document contient, et des pièges propres à chaque
métier : `VTC-CLIENT` et `FOOD-CLIENT`, section « Le reçu et le relevé ».

## 10. Les deux métiers — l'inventaire, et où chacun est décrit

Rien ne change dans ces parcours : les routes, les formes et les refus sont
ceux des deux specs métier, qui restent à jour.

### Livraison → **`FOOD-CLIENT.md`** (base `…/api/v1/food`)

| Ce que vous faites | Routes |
|---|---|
| Découvrir | `GET /stores` · `GET /stores/{id}` · `GET /dishes/{id}` · `GET /feed` |
| Commander | `POST /orders` · `GET /orders` · `GET /orders/{id}` · `POST /orders/{id}/cancel` |
| | 🎟️ **LE CODE PROMO SE SAISIT À LA COMMANDE (v4.51.0)** — `{ "code": "…" }`. ⚠️ **Un code refusé fait ÉCHOUER la commande** plutôt que de la créer au prix plein : gardez le panier à l'écran. `promo_code_xof` est **distinct de `discount`**, qui porte un crédit de tombola. Détail : §6 quater et `FOOD-CLIENT` §4 |
| Suivre | `GET /deliveries/{id}` · socket du suivi (§11) |
| Reçu, relevé | 🧾 `GET /orders/{id}/receipt.pdf` · `GET /orders/statement.pdf?from=&to=` **(v4.52.0)** — et `delivery.actual_distance_m` + `distance_source`, enfin servis au client ; `country` aussi, qui **dit la monnaie** des montants |
| Parler | `GET/POST /orders/{id}/messages` |
| Noter | `POST /orders/{id}/rating` |
| Nutrition, tombola | `GET /nutrition/profile` · `GET /nutrition/plan` · `GET /tombola/draws` · `GET /tombola/me` |

### Courses → **`VTC-CLIENT.md`** (base `…/api/v1/vtc`)

| Ce que vous faites | Routes |
|---|---|
| Choisir | `GET /classes?near=` · `GET /settings/modes?near=` |
| | 🛵 **Le MOTO-TAXI est un mode du catalogue (v4.48.0)**, ouvert dans tous les pays et **en tête** de la liste : `seats: 1`, pas de course partagée (`modes` n'a pas de clé `pool`), `icon_url` `null` le temps que l'exploitation pose son image — dessinez la silhouette `map_icon` (`moto`). ⚠️ **Aucune voiture ne répond à un appel de moto** : ne promettez pas « une voiture si aucune moto n'est libre ». Détail : `VTC-CLIENT` §2 |
| Chiffrer | `POST /rides/quote` · `POST /rides/pool/quote` · `POST /rides/rental/quote` |
| | 🎟️ **LE CODE PROMO SE SAISIT AU DEVIS (v4.51.0)** — `{ "code": "…" }`, et la remise revient **par classe** : 20 % d'une moto et 20 % d'un van ne font pas le même nombre de francs. ⚠️ `promo_code_ignored` dit qu'un code **valable** a été battu par une offre automatique : ce n'est **pas** un refus, et le dire évite l'appel au support. Détail : §6 quater et `VTC-CLIENT` §3 |
| | ⚠️ **La course PARTAGÉE cherche le CO-PASSAGER *avant* le chauffeur** — c'est le seul mode qui le fasse, et l'écran d'attente en dépend : pendant la première étape (`dispatch_state: "pooling"`, jusqu'à 5 min) **aucun chauffeur n'est appelé**. Principe complet, chronologie des deux passagers, conditions d'appariement, ordre de route et les quatre fins possibles : `VTC-CLIENT` §4 quater, **à lire avant de câbler un écran** |
| Commander | `POST /rides` · `GET /rides` · `GET /rides/{id}` · `POST /rides/{id}/cancel` · `POST /rides/{id}/relaunch` |
| | 💸 **ANNULER PEUT COÛTER (v4.50.0)** — `cancellation` voyage avec la course : `fee_xof`, `step`, `why`, `grace_left_s`. ⚠️ **Retenir de l'argent à quelqu'un qui n'a pas pu le lire AVANT est indéfendable** : l'écran de confirmation dit le montant, ou dit que c'est gratuit **et jusqu'à quand**. Jamais de bouton « Annuler » nu quand `fee_xof > 0`. Détail : `VTC-CLIENT` §5 |
| En route | `PATCH /rides/{id}/stops` · socket du suivi (§11) |
| Reçu, relevé | 🧾 `GET /rides/{id}/receipt.pdf` · `GET /rides/statement.pdf?from=&to=` **(v4.52.0)** — distance **réellement parcourue** et sa source ; une course annulée porte désormais `cancel_fee_xof` |
| Parler, noter | `GET/POST /rides/{id}/messages` · `POST /rides/{id}/rating` |
| | 🔒 **CE QUE VOUS AVEZ LE DROIT D'AFFICHER du chauffeur ou du livreur est réglé par l'exploitation, pays par pays et métier par métier (v4.49.0).** Le serveur **n'envoie pas** ce qu'on n'a pas le droit de montrer : un champ absent n'est pas une panne, c'est un champ fermé. ⚠️ `phone` présent ne veut **pas** dire affichable — lisez `show_phone` ; `direct_call` sans `show_phone` veut dire « un bouton qui appelle, et le numéro nulle part ». ⚠️ **N'écrivez aucune règle en dur** : ces décisions changent sans redéploiement. Détail : `VTC-CLIENT` et `FOOD-CLIENT`, section Confidentialité |
| Programmer, s'abonner | `POST /schedules` · `POST /subscriptions` |

⚠️ **`?near=` SUR LES DEUX ROUTES DE RÉGLAGE DES COURSES.** Passez le point de
départ : sans lui, vous lisez les modes et le catalogue du pays du **compte**,
et vous recevez les prix du pays où la course se fera. Deux pays dans un seul
écran.

---

## 11. ⚠️ Temps réel : DEUX sockets, et il faut les deux

| Canal | Base | Ce qui arrive | Portée |
|---|---|---|---|
| **Socket des commandes** | `wss://api…/api/v1/food/ws/orders?token=` | `order_created`, `order_status { from, status }`, `order_message` | application ouverte, **toutes** vos commandes |
| **Socket du suivi** | `wss://tracking…/track/subscribe/{id}?token=` | `position`, et `status` à chaque passage | application ouverte, **une** opération à la fois — `{id}` est l'identifiant de la **course** ou de la **livraison** |
| **Push** | socle, `POST /me/devices` | un gabarit **et** des données (`type`, l'identifiant, `status`) | application fermée ou en arrière-plan |

⚠️ **DEUX SOCKETS, DEUX SERVICES, DEUX CYCLES DE VIE. NE LES FACTORISEZ PAS**
sous prétexte que ce sont deux WebSockets. Celui des commandes est global et
vit tant que l'application est ouverte ; celui du suivi est **par opération**
et se referme quand l'écran se ferme. Une couche unique « le socket » finit par
fermer le mauvais.

⚠️ **LES COURSES N'ONT PAS DE SOCKET GLOBAL.** Il n'existe rien qui annonce
« une de vos courses a changé » : pour une course, c'est le socket du suivi de
**cette** course, ou le push. Une application unifiée qui attendrait la
symétrie des deux métiers attendrait une trame qui n'arrive jamais.

### Le flux, toujours le même — **un signal, un `GET`**

1. **À l'ouverture d'un écran** : `GET` la ressource. C'est l'état de
   référence — jamais ce que dit le socket.
2. **Trame d'état reçue** : comparez à ce que vous affichez ; si ça diffère,
   `GET` et redessinez. Un `from` qui n'est pas votre état signale une trame
   manquée — relisez.
3. **Push reçu** (arrière-plan) : ouvrir l'écran, `GET`.
4. **Reconnexion** (back-off 1 s → 2 s → 4 s … 30 s) : `GET` **immédiatement**,
   avant d'appliquer la moindre trame.
5. **Sans socket** : sondez toutes les **10 s** tant que l'opération n'est ni
   terminée ni annulée, 30 s au bout de cinq minutes sans changement. Jamais
   sur une opération terminée.

⚠️ **AUCUN CANAL NE GARANTIT L'ORDRE, L'UNICITÉ NI LA LIVRAISON.** Deux trames
pour le même passage (socket **et** push) sont normales : le second `GET` répond
la même chose. Une application qui ferait du socket sa source de vérité verrait,
un jour, une course « en route » qu'un `GET` dit terminée.

---

## 12. ⚠️ Une seule boîte, deux vocabulaires

`GET /notifications` (socle) rend **tout** : les commandes et les courses, dans
une seule liste. Chaque entrée porte une **clé de gabarit** et des **données**.

| Métier | Clés | Données |
|---|---|---|
| Livraison | `order_confirmed` · `order_preparing` · `order_ready` · `order_assigned` · `order_delivered` · `order_cancelled` | `type: "order_status"`, `order_id`, `status` |
| Courses | `ride_accepted` · `ride_driver_on_the_way` · `ride_driver_arrived` · `ride_driver_honked` · `ride_cancelled` · `ride_search_exhausted` · `ride_fare_adjusted` · `ride_rate_prompt` | `type: "ride_status"`, `ride_id`, `status` |
| Courses partagées | `ride_pool_matched` · `ride_pool_no_match` · `ride_pool_partner_left` | `type: "ride_status"`, `ride_id`, `dispatch_reason` |
| Transverse | `ticket_reply` · `ticket_resolved` · `session_superseded` | selon le gabarit |

⚠️ **ROUTEZ SUR `data.type`, PAS SUR LA CLÉ.** `type` dit quel écran ouvrir,
l'identifiant dit lequel. La clé du gabarit est du **texte** : elle peut être
renommée par l'exploitation, et une application qui décide d'après elle ouvrira
un jour le mauvais écran. ⚠️ Et une clé **inconnue** doit s'afficher quand même,
dans la boîte, sans rien ouvrir : une application qui ignore ce qu'elle ne
connaît pas fait disparaître des messages.

⚠️ **UNE CLÉ NE DIT PAS SON MÉTIER.** `ticket_reply` peut concerner une
commande ou une course. Ne devinez pas le métier d'après la clé ; lisez
`data.type`.

---

## 13. Codes d'erreur à traiter nommément

**Transverses (socle)**

| Code | Geste |
|---|---|
| `401 invalid_token` | rafraîchir, puis reconnecter |
| `401 session_superseded` | le compte a été ouvert ailleurs — **déconnecter et le dire** |
| `403 wrong_app` | `error.reason` nomme l'application à ouvrir |
| `429 rate_limited` | réessayer plus tard, sans boucler |
| `422 validation_failed` | souligner `fields` |
| `403 account_closed` | ce compte a été **supprimé** — « se réinscrire », ou le support si c'est une erreur |

**La porte par code (§6 bis)**

| Code | Geste |
|---|---|
| `401 otp_invalid` | « code incorrect » — garder la saisie |
| `401 otp_expired` | « demandez un nouveau code » |
| `401 otp_too_many_attempts` | le code est **mort** : revenir à l'écran du numéro |
| `429 otp_too_soon` · `429 otp_too_many_requests` | « renvoyer » grisé jusqu'à `resend_after` ; au plafond horaire, proposer le support |
| `503 otp_delivery_failed` | **rien n'a été consommé** : redemander tout de suite est légitime |
| `403 otp_not_available` | ce compte se connecte par mot de passe — nommer l'application à ouvrir |

**Le combiné (§8, §9)**

| Code | Geste |
|---|---|
| `403 ai_disabled` · `503 assistant_unavailable` | masquer la boîte de dialogue |
| `429 ai_quota_reached` · `429 ai_budget_reached` | proposer la composition à la main |
| `503 verticals_unavailable` | aucun métier joignable — réessayer |
| `502 vertical_unreachable` | ce métier ne répond pas — réessayer |

**Les métiers** : leurs refus sont décrits dans leurs specs, et ils arrivent
**tels quels** à travers l'assistant combiné — traitez-les comme s'ils venaient
du métier.

---

## 14. Liste de contrôle d'intégration

- [ ] **Quatre** bases configurées, et une vérification à la construction que chaque route part de la bonne.
- [ ] `app: "client"` à la connexion ; `403 wrong_app` traité avec `error.reason`.
- [ ] Porte par code : compte à rebours rendu depuis `expires_at`, « renvoyer » gouverné par `resend_after`, nom demandé **seulement** si `created: true`, `dev_code` jamais nécessaire au fonctionnement.
- [ ] Verrou : `app_lock` relu à **chaque** réponse qui le porte (y compris le rafraîchissement) et appliqué à chaud ; code de secours toujours possible ; au-delà de `max_attempts`, **déconnexion** — jamais blocage.
- [ ] `X-Dira-Country` sur **chaque** requête ; monnaie formatée depuis le pays de l'**opération**.
- [ ] `X-Request-ID` affiché sur les écrans d'erreur et joint aux rapports.
- [ ] Fond de carte : choix de la personne → pays où elle opère → `dira` ; la carte **renaît** au changement.
- [ ] `vertical` envoyé depuis un onglet, **omis** depuis l'accueil.
- [ ] `choices` traité : deux boutons, puis le même message avec `vertical`.
- [ ] Un plan ne commande rien : confirmation par la route du métier.
- [ ] `unresolved` affiché, pas tu.
- [ ] Fil d'activité : `next_before` redonné, semaines vides sautées, `timezone` utilisé, `sources` affiché quand un métier manque.
- [ ] Deux sockets distincts, cycles de vie séparés ; `GET` à chaque reconnexion.
- [ ] Notifications routées sur `data.type` ; clé inconnue affichée sans ouvrir.
- [ ] Un seul carnet d'adresses, un seul solde, une seule boîte.
- [ ] Suppression de compte : écran de conséquences **avant** la preuve d'identité, `erase_at` affiché, « les courses et commandes passées restent » dit **avant** le bouton, `409 wallet_not_empty` renvoyé vers le solde, `403 account_closed` traité à la connexion **et** à l'inscription.

---

## 15. Journal

### 4.52.0 — 8 octobre 2026

🧾 **REÇUS ET RELEVÉS EN PDF**, deux routes par métier (§9 bis), avec la
**distance réellement parcourue** et sa source — `tracked` mesurée, `planned`
estimée, et le second cas est courant. ⚠️ `attachment` + `no-store` : **pas de
WebView, pas de cache disque** — ces documents portent une adresse, un trajet et
un montant. ⚠️ **Le détail s'additionne jusqu'au total ou rien n'est servi**
(500). ⚠️ `from`/`to` **obligatoires sans défaut**, et les relevés **refusent au
lieu de tronquer** (`409 statement_too_large`, 500 opérations / 366 jours).
⚠️ **Deux relevés, pas un** : c'est le seul endroit où l'application unifiée ne
peut pas réunir les deux métiers — proposez deux exports. Nouveaux champs :
`cancel_fee_xof` sur une course annulée (enfin servi — le bloc `cancellation`
n'est qu'un devis et disparaît), `delivery.actual_distance_m` +
`distance_source` au client, et `country` sur une commande, **qui dit la
monnaie**.


### 4.51.0 — 8 octobre 2026

🎟️ **UN CHAMP « CODE PROMO », DANS LES DEUX TUNNELS.** Un code est au **socle** :
le même mot ne peut pas valoir une chose sur une course et une autre sur une
commande. `POST /rides/quote { …, "code" }` et `POST /orders { …, "code" }` ; le
prix servi est **déjà remisé**. ⚠️ **Les deux métiers ne refusent pas au même
moment** — au devis pour une course (on corrige), à la création pour une commande
(elle n'est **pas** créée) : gardez le panier à l'écran. ⚠️ `promo_code_ignored`
(courses seulement) dit qu'un code **valable** a été battu par une offre
automatique : ce n'est pas un refus. ⚠️ `promo_code_wrong_service` arrive avec
`meta.valid_for` — vous portez les deux onglets, **proposez d'y aller** plutôt
que d'écrire « code invalide ». 🎁 **`GET /me/referral`** : un écran de compte,
sans métier. ⚠️ `enabled` à faux ⇒ **cachez l'entrée**. ⚠️ Deux montants, deux
bénéficiaires ; le parrain est payé quand la remise est **consommée**, et comme
le filleul peut la dépenser dans l'un ou l'autre onglet, le crédit arrive parfois
d'un métier que le parrain ne regardait pas — le fil d'activité (§9) est le bon
endroit pour le dire. Détail : §6 quater.


### 4.50.0 — 8 octobre 2026

💸 **ANNULER UNE COURSE PEUT COÛTER.** Un barème se règle par pays et par
véhicule, et il voyage **avec la course** dans `cancellation` : `fee_xof`,
`pct`, `step`, `why`, `grace_left_s`. ⚠️ **L'écran de confirmation doit le
dire** — retenir de l'argent à quelqu'un qui n'a pas pu le lire avant est
indéfendable. Gratuit tant qu'aucun chauffeur n'a répondu, puis gratuit pendant
`grace_left_s` secondes, puis une part du tarif — plus haute quand le chauffeur
**attend** sur place. ⚠️ Sur une course payée, le remboursement est **amputé**
des frais ; en espèces, ils deviennent une **dette** réglée à la prochaine
recharge. Détail : `VTC-CLIENT` §5.

### 4.49.0 — 8 octobre 2026

🔒 **QUI VOIT QUOI DE QUI** — ce que vous avez le droit d'afficher de l'autre
partie est désormais réglé par l'exploitation, **pays par pays et métier par
métier**. Le serveur **n'envoie pas** ce qu'on n'a pas le droit de montrer : un
champ absent n'est pas une panne. ⚠️ `phone` présent ne veut **pas** dire
affichable — lisez **`show_phone`** ; `direct_call` sans `show_phone` veut dire
« un bouton qui appelle, et le numéro nulle part ». ⚠️ **N'écrivez aucune règle
en dur** (« si c'est une course, pas de téléphone ») : elle contredirait le
serveur sans que personne ne sache laquelle croire. Détail : `VTC-CLIENT` §
Confidentialité et `FOOD-CLIENT` § Confidentialité.

### 4.48.0 — 8 octobre 2026

🛵 **LE MOTO-TAXI** — `moto` arrive dans `GET /classes`, **en tête** du
catalogue et dans **tous les pays** : `seats: 1` (ni 2 passagers, ni bagage),
pas de course partagée (`modes` n'a pas de clé `pool`), `icon_url` `null` le
temps que l'exploitation pose son image — dessinez la silhouette `map_icon`
(`moto`). ⚠️ **Aucune voiture ne répond à un appel de moto** : ne promettez pas
« une voiture si aucune moto n'est libre ». Détail : `VTC-CLIENT` §2.

### 4.47.0 — 7 octobre 2026

🗑️ **SUPPRIMER SON COMPTE** — `DELETE /me` (§6 ter), en **deux temps** : le
compte est **fermé** tout de suite (sessions coupées partout, `403
account_closed` ensuite, par mot de passe **comme par code**) et son identité
part à **`erase_at`**, trente jours plus tard. ⚠️ **Les courses et les commandes
passées restent** — ce sont des écritures comptables — mais elles ne désignent
plus personne : le nom devient « Compte supprimé » partout, d'un coup. ⚠️ Il
faut **prouver qui on est** (`password` ou `code`), le portefeuille doit être
**vide** (`409 wallet_not_empty`, `meta.reason`), et le numéro n'est **libéré**
qu'à `erase_at` — se réinscrire avant répond `403 account_closed`, pas
`phone_taken`. Pendant le délai, le support peut **annuler**.

### 4.46.0 — 7 octobre 2026

🆕 **LA PORTE PAR CODE** — `POST /auth/otp` et `POST /auth/otp/verify` (§6 bis) :
le téléphone, six chiffres, pas de mot de passe. `verify` rend le corps de
`POST /auth/login` plus **`created`**. ⚠️ Tant que `channel` vaut `echo`, **rien
n'est envoyé** et le code revient dans **`dev_code`** — un bonus de
développement qui disparaîtra sans préavis. ⚠️ Le nom ne se demande qu'après,
et seulement si `created: true` : la demande de code ne dit jamais si le numéro
est connu. ⚠️ Un compte né par code **n'a pas de mot de passe** : pas d'écran
« mot de passe oublié » pour lui.

### 4.43.0 — 6 octobre 2026

🆕 **Première version de ce document** : l'application cliente **unifiée**, qui
fait la livraison **et** les courses sur un compte, un solde et un historique.

- **L'assistant unifié** `POST /analytics/ai/chat` — un champ de saisie, deux
  métiers. `vertical` dit qui a répondu, `plan` est celui du métier **verbatim**,
  `routed_by` dit comment on a tranché. ⚠️ **`vertical` absent = on n'a pas su**,
  et `choices` demande : on ne devine pas.
- **Le fil d'activité** `GET /analytics/activity` — courses et commandes dans
  une seule liste, par **semaine**, dans le **fuseau du pays**. Le pas de
  pagination est la semaine ; les semaines vides sont sautées ; `sources` dit ce
  qui a répondu.
- **`?before=`** sur `GET /vtc/rides` et `GET /food/orders` : la fenêtre de date
  qui rend le fil combiné possible.
- ⚠️ Les **deux sockets** et la **boîte unique** : les deux pièges propres à une
  application qui réunit les deux métiers, écrits ici parce qu'aucune des deux
  specs métier ne pouvait les dire.
