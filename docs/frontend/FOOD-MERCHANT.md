# App / console MARCHAND — LIVRAISON — contrat d'API

> **Version 3.4.0** · 12 septembre 2026
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

## 1. Le modèle en une phrase

Une **enseigne** (`merchant`) possède plusieurs **points de vente** (`store`). Le **catalogue est au niveau de l'enseigne** ; prix et disponibilité se **surchargent** par point de vente.

> C'est la distinction qui structure tout le reste. Un plat existe une fois pour la marque ; chaque boutique décide de son prix et de sa disponibilité. Un **portefeuille de jetons par point de vente**, pas un par enseigne.

Un sélecteur de boutique en tête d'écran fixe le point de vente courant : tableau de bord, portefeuille et carte en dépendent tous.

Conventions communes (erreurs, pagination, montants, dates) : voir [`FOOD-CLIENT.md` §1](FOOD-CLIENT.md).

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

Seuls `preparing` et `ready` sont à la main du marchand. Le reste suit le livreur.

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
GET /me · PATCH /me · PATCH /me/preferences        (SOCLE, sans /food)
POST /uploads?kind=dish&entity={id}                (SOCLE, sans /food)
POST /me/devices · GET /me/notifications           (SOCLE, sans /food)
POST /tickets · POST /bug-reports
```

⚠️ **`POST /uploads` est au socle — v3.0.0** : `…/api/v1/uploads`, plus `/food/uploads` (404). Multipart, champ `file`, le **type déclaré** de la part fait foi.

`kind` ∈ `dish` · `store` · `brand` · `vehicle` · `avatar` · `feed` · `banner`. Images (jpeg, png, webp, svg) ≤ 5 MiB pour tout `kind` ; `feed` accepte **aussi** des vidéos ≤ 60 MiB — une vidéo pèse bien plus qu'une photo de plat, et rien d'autre ne profite de ce plafond. Au-delà de 64 MiB, la passerelle répond `413 payload_too_large`.

Téléversez **d'abord**, rattachez l'URL ensuite : une image qui échoue ne doit pas faire perdre la saisie.

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

### ❌ Le nom du livreur sur la carte de commande

La carte dessinée annonce « Livreur affecté · *Mamadou D.* ». L'API ne rend **aucune identité de livreur** au marchand.

> Le marchand a besoin de savoir **qu'un livreur vient** — ce que le statut `assigned` dit déjà — plus que de savoir **qui**. Affichez « Livreur affecté » sans nom tant que ce champ n'existe pas.

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

---

## 10. Points ouverts

1. **Aucune note d'enseigne** n'est calculée : seuls les points de vente en portent une.
2. **Aucune modération** des commentaires du feed ni des avis côté marchand.
3. **Pas d'horaires structurés** : `is_open` est un booléen, pas un calendrier d'ouverture.
4. **Aucune gestion de stock** : `available` est binaire, sans quantité.
5. **Le catalogue est plat** : pas de menus composés ni de formules.
