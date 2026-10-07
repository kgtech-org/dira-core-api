# App CLIENT UNIFIÉE — LIVRAISON **et** COURSES — contrat d'API

> **Version 4.45.2** · 7 octobre 2026
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

## 6. L'identité et l'argent — **PARTAGÉS** (socle, sans préfixe)

C'est tout l'intérêt d'une application unifiée : **une** inscription, **un**
solde, **une** boîte.

```
POST /auth/register · POST /auth/login · POST /auth/refresh · POST /auth/logout
GET /me · GET /me/addresses · POST /me/devices
GET /wallet · GET /wallet/transactions
POST /payments/initiate · GET /payments/{id} · GET /payments/providers
GET /notifications · GET /countries · POST /uploads
```

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

## 10. Les deux métiers — l'inventaire, et où chacun est décrit

Rien ne change dans ces parcours : les routes, les formes et les refus sont
ceux des deux specs métier, qui restent à jour.

### Livraison → **`FOOD-CLIENT.md`** (base `…/api/v1/food`)

| Ce que vous faites | Routes |
|---|---|
| Découvrir | `GET /stores` · `GET /stores/{id}` · `GET /dishes/{id}` · `GET /feed` |
| Commander | `POST /orders` · `GET /orders` · `GET /orders/{id}` · `POST /orders/{id}/cancel` |
| Suivre | `GET /deliveries/{id}` · socket du suivi (§11) |
| Parler | `GET/POST /orders/{id}/messages` |
| Noter | `POST /orders/{id}/rating` |
| Nutrition, tombola | `GET /nutrition/profile` · `GET /nutrition/plan` · `GET /tombola/draws` · `GET /tombola/me` |

### Courses → **`VTC-CLIENT.md`** (base `…/api/v1/vtc`)

| Ce que vous faites | Routes |
|---|---|
| Choisir | `GET /classes?near=` · `GET /settings/modes?near=` |
| Chiffrer | `POST /rides/quote` · `POST /rides/pool/quote` · `POST /rides/rental/quote` |
| | ⚠️ **La course PARTAGÉE cherche le CO-PASSAGER *avant* le chauffeur** — c'est le seul mode qui le fasse, et l'écran d'attente en dépend : pendant la première étape (`dispatch_state: "pooling"`, jusqu'à 5 min) **aucun chauffeur n'est appelé**. Principe complet, chronologie des deux passagers, conditions d'appariement, ordre de route et les quatre fins possibles : `VTC-CLIENT` §4 quater, **à lire avant de câbler un écran** |
| Commander | `POST /rides` · `GET /rides` · `GET /rides/{id}` · `POST /rides/{id}/cancel` · `POST /rides/{id}/relaunch` |
| En route | `PATCH /rides/{id}/stops` · socket du suivi (§11) |
| Parler, noter | `GET/POST /rides/{id}/messages` · `POST /rides/{id}/rating` |
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

---

## 15. Journal

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
