# Brief de design — ce qui manque aux maquettes

> **Version 1.6** · 10 septembre 2026 · pour `.resources/dira-mobile-apps` (maquette du 10/09) · contrat **v2.1.0**

> ⚠️ **v2.0.0 — deux bases d'URL.** Les routes de la livraison prennent le préfixe `/food` ; celles du socle (connexion, profil, portefeuille, paiements, notifications) restent à la racine. Rien d'autre ne change : mêmes champs, mêmes codes, mêmes écrans. Voir [`README.md`](README.md).
> À lire avec [`FOOD-CLIENT.md`](FOOD-CLIENT.md), [`FOOD-MERCHANT.md`](FOOD-MERCHANT.md), [`FOOD-DELIVERY.md`](FOOD-DELIVERY.md), qui portent les contrats.

Ce document ne décrit pas des écrans à créer, mais **ce que les écrans existants doivent montrer et ne montrent pas**. Chaque point dit *où*, *quoi*, et surtout *pourquoi* — sans le pourquoi, un designer arbitrera au jugé, et c'est là que ces informations disparaissent.

> **Passe du 10 septembre 2026.** La maquette a été reprise : elle porte désormais **49 écrans** et **cinq rôles**, dont deux de VTC. Ce qui suit est à jour de cette version.

Trois niveaux :

| | |
|---|---|
| 🔴 | **Quelqu'un perd de l'argent** si ça manque. À traiter d'abord. |
| 🟠 | L'écran induit en erreur, ou une action échoue après coup. |
| 🟡 | Confort, cohérence, ou décision produit à prendre. |

---

## 🔴 1. L'ARGENT LIQUIDE — le trou le plus coûteux

J'ai cherché « espèces » et « cash » dans toute la maquette : **toutes** les occurrences sont dans le VTC ou le portefeuille client. Le modèle de paiement en espèces de la livraison n'apparaît **nulle part**.

Or il fonctionne ainsi :

```
Client paie en espèces
  → le LIVREUR avance l'argent au marchand, de la main à la main, au comptoir
  → le MARCHAND encaisse du livreur avant de remettre la commande
  → le livreur se rembourse sur ce que le client lui donne à l'arrivée
```

### 1.1 — Écran livreur : « Aperçu de la mission »

**Ajouter, à côté du coût en jetons :**

> **À AVANCER · 4 200 FCFA** — vous payez les restaurants, vous vous remboursez à la livraison

**Où** : dans le bloc qui porte déjà « Coût en jetons », avant le bouton *Accepter la mission*.

**Pourquoi c'est critique** : rien ne vérifie côté serveur que le livreur a l'argent sur lui. C'est une **condition d'utilisation**, pas un contrôle. L'écran d'acceptation est donc le **seul** endroit où cela se joue — s'il n'y figure pas, un livreur découvre la somme devant le restaurant, et la commande s'arrête là.

Le champ existe : `cash_required_xof`. **Zéro sur une commande prépayée** — n'affichez alors rien, pas « 0 FCFA ».

### 1.2 — Écran livreur : « Étape de mission », au point de collecte

**Ajouter** le montant dû **à cette boutique-là**, pas le total. Une commande peut traverser trois restaurants.

### 1.3 — Écran livreur : à la livraison

**Ajouter** ce qu'il doit **réclamer au client** : `cash_to_collect_xof`. C'est le nouveau champ que je viens d'ajouter à votre demande.

⚠️ **Deux montants qui se ressemblent et vont en sens inverse.** Ne les nommez pas pareil, ne les mettez pas côte à côte sans distinction visuelle :

| | |
|---|---|
| `cash_required_xof` | ce qui **sort** de sa poche, aux restaurants |
| `cash_to_collect_xof` | ce qui **entre**, chez le client |

### 1.4 — Écran marchand : au moment de « Marquer prêt »

**Ajouter** un avertissement, pas une ligne discrète :

> ⚠️ **Encaissez 4 200 FCFA du livreur** avant de lui remettre la commande.

**Pourquoi ici et pas ailleurs** : personne n'atteste côté serveur que l'argent a changé de main. C'est au marchand de s'en assurer. **Après le retrait, l'information ne rattrape plus rien.**

Le champ est `cash_to_collect` sur `GET /stores/{id}/orders`. **Zéro sur une commande prépayée** — le marchand est alors crédité sur son portefeuille au retrait, et il ne doit rien réclamer.

---

## 🟠 2. Ce que l'écran promet et que l'API refusera

### 2.1 — Livreur : la distance qui rémunère peut être ESTIMÉE

À la complétion, la réponse porte `distance_source` : `tracked` (positions réellement poussées) ou `planned` (repli, quand le tampon GPS était vide).

**Ajouter**, sur l'écran « Livraison terminée » quand c'est `planned` :

> Distance **estimée** — le GPS n'a pas suivi la course

**Pourquoi** : c'est cette distance qui rémunère. Le livreur doit pouvoir la contester **avant** de la découvrir sur sa paie.

### 2.2 — Livreur : une course peut dépasser sa capacité

Le mot « capacité » n'apparaît nulle part dans la maquette. Un véhicule déclare combien de courses il porte à la fois ; au-delà, `POST /deliveries/{id}/accept` renvoie `409 driver_at_capacity`.

**Ajouter** : griser les courses inaccessibles **dans la liste**, avec la raison. Un refus après le clic est un refus qu'on aurait pu éviter.

Même logique pour **`409 no_active_vehicle`** : le dire avant que le livreur coure vers une course qu'il ne peut pas prendre.

### 2.3 — Marchand : « Marquer prêt » n'est pas un statut de plus

C'est **le geste qui déclenche la recherche d'un livreur**. Les cinq plus proches sont appelés dans la seconde.

**Ajouter** un retour visible après le clic : « Recherche d'un livreur en cours… », puis l'état de l'appel. Aujourd'hui la maquette le traite comme n'importe quelle transition — or c'est le moment le plus important du cycle, et le marchand doit comprendre que le compte à rebours a commencé.

### 2.4 — Client : le paiement au solde Dira n'est pas proposé

Le panier propose mobile money et espèces. Il manque **`payment_method: wallet`** — payer sur son solde Dira, qui existe pourtant comme écran de compte.

Solde insuffisant → `402`, et la commande est **annulée**. L'écran doit donc afficher `spendable_xof` **avant** de proposer ce moyen, et le griser s'il ne couvre pas le total.

---

## 🟡 3. Ce que la maquette demandait — cinq points sur six sont CONSTRUITS

> **v1.1 du brief.** À la v1.0, ces six écrans n'avaient rien derrière eux. Les décisions produit ont été prises, et **cinq sont désormais servis par l'API**. Ce qui suit dit ce qui a été tranché, parce que la décision change le dessin.

### ✅ 3.1 — Marchand : « Personnel » et « Attribution de store »

**Construit.** `GET · POST /me/staff`, `PATCH · DELETE /me/staff/{id}`.

Ce qui a été tranché, et qui se voit à l'écran :

- **Un membre est un COMPTE**, pas une fiche d'annuaire. L'écran d'ajout demande donc un **téléphone**, un nom, et **un mot de passe quand le compte n'existe pas encore**. Prévoyez ce champ conditionnel : sans lui, la personne ne peut pas se connecter.
- **Deux rôles seulement** : `manager` (commandes, carte, portefeuille) et `staff` (**les commandes, et rien d'autre**). Pas de grille de cases à cocher — chaque rôle de plus est une case dont personne ne se souvient du sens six mois plus tard.
- **Un membre affecté ne voit que sa boutique.** L'affectation est donc une information de premier plan sur la carte du membre, pas un détail replié.
- **Le propriétaire n'apparaît pas dans la liste** et **lui seul voit ce menu**. Un gérant reçoit `403` sur les quatre routes : n'affichez pas l'entrée « Personnel » dans son application.
- **Retirer ≠ supprimer.** La confirmation doit dire : « Cette personne perdra l'accès à votre enseigne. Son compte reste ouvert. »

> Lisez `capabilities` sur chaque membre plutôt que de recoder la table des rôles dans l'application. Deux tables divergent, et l'écran finit par montrer des boutons que l'API refuse.

### 🟠 3.2 — Marchand : « Nouvelle commande » en appel de 30 s

**À moitié construit — et la moitié qui manque change l'écran.**

✅ **Refuser existe** : `POST /stores/{id}/orders/{order_id}/refuse`, **jusqu'à `ready` inclus**.

- ⚠️ **Le refus annule la commande ENTIÈRE**, multi-boutiques comprise. **Écrivez-le dans la confirmation** — « y compris pour les autres boutiques ». Un marchand qui l'apprend après coup croira à un bug.
- **Grisez le bouton dès que la commande est partie** avec un livreur : l'API répond `409 order_not_refusable`, et un bouton actif qui échoue est pire qu'un bouton absent.
- Le motif est **facultatif**. Ne le rendez pas obligatoire : on tape n'importe quoi, et le champ ne dit plus rien.

🟡 **Le dispatch marchand existe depuis la v1.4.0 — mais ce n'est PAS un appel.** Le serveur retient le point de vente **le plus proche de l'adresse de livraison** qui a le plat, et le lui **attribue**. Le sous-titre « Vous êtes le point de vente sélectionné » est donc devenu **vrai**.

❌ **Le compte à rebours de 30 s, lui, n'a toujours rien derrière.** La sélection se fait à la **création** de la commande, parce que le prix et les frais de livraison en dépendent et doivent être connus avant le paiement. Le marchand n'a aucun délai pour « prendre » la commande : elle est déjà à lui.

> **Retirez le chronomètre, gardez le sous-titre.** « Nouvelle commande — vous êtes le point de vente sélectionné », avec Accepter et Refuser. Un chronomètre qui ne décide de rien apprend à ignorer les chronomètres, et celui-ci ne décide de rien.

> ⚠️ **Un refus n'envoie pas la commande à la boutique voisine** — il l'annule entièrement. Ne laissez pas la maquette suggérer le contraire : ré-acheminer changerait le montant après paiement, puisque les prix se surchargent par point de vente.

### ✅ 3.3 — Livreur : détails du colis (taille, fragile)

**Construit.** Tranché : **c'est le PLAT qui les porte**, au catalogue — un gâteau est toujours fragile, et le redemander à chaque préparation ferait oublier de le cocher au moment où le marchand est le plus pressé.

L'écran livreur les reçoit **deux fois**, et les deux servent :

| Niveau | Ce que c'est |
|---|---|
| ligne d'article | ce plat-ci |
| point de collecte | la **synthèse** — `fragile` dès qu'une ligne l'est, `pack_size` = le plus encombrant |

> **La synthèse est celle qui doit être visible avant d'ouvrir le détail.** Un livreur décide de son équipement avant de partir ; l'obliger à parcourir les lignes, c'est le lui faire découvrir devant le comptoir.

> ⚠️ **`pack_size` absent = NON RENSEIGNÉ, pas « petit ».** N'affichez rien plutôt qu'une icône de petit sac — ce serait rassurer à tort.

### ✅ 3.4 — Livreur : temps estimé

**Construit.** Tranché : **appel à dira-maps**, plutôt qu'une vitesse moyenne par véhicule qui aurait été une fausse précision en heure de pointe.

`planned_duration_s` est calculée **une fois, à la création**, et figée sur la course : la redemander à chaque affichage ferait un appel externe par ouverture d'écran, et la durée changerait sous les yeux du livreur sans qu'il ait bougé.

> **Elle peut être absente** quand le SIG n'a pas répondu. N'affichez rien, et **ne calculez pas de repli maison** : une durée devinée serait indiscernable d'une durée mesurée.

### 🟡 3.5 — Livreur : profil agent complet

**Toujours rien.** N° de pièce d'identité, ville, contact d'urgence, double authentification, taux d'acceptation, heures en ligne — aucun n'existe. La note et le nombre de courses, eux, existent.

Le contact d'urgence et la pièce d'identité relèvent de la **conformité**, un chantier avec ses propres règles (conservation, accès, suppression). **C'est le seul des six qui reste à arbitrer.**

### ✅ 3.6 — Marchand : recherche et ventes par plat

**Construit.** `GET /stores/{id}/orders?q=` cherche dans les **noms de plats** ; `GET /stores/{id}/sales?days=30` rend le classement.

Deux décisions qui se dessinent :

- **Fenêtre de 30 jours par défaut** — l'horizon sur lequel on décide quoi garder à la carte. Une semaine suit le hasard d'un week-end, un an lisse les saisons. Si l'écran propose un sélecteur, que 30 jours soit la valeur par défaut.
- **Articles vendus ≠ commandes.** Chaque plat porte les deux (`qty` et `orders`) : **affichez-les côte à côte**. C'est ce qui distingue un plat que tout le monde prend d'un plat qu'un seul client commande par dix — et le second ne doit pas passer pour un succès.

> Les commandes **annulées** sont exclues du classement. Ne proposez pas de bascule « inclure les annulées » : compter une vente qui n'a jamais eu lieu ferait garder à la carte un plat que personne n'a mangé.

---

## ✅ 4. Ce qui vient d'être ajouté à l'API pour vous

Livrable de cette passe — les maquettes peuvent s'appuyer dessus :

| Champ | Où | Ce que c'est |
|---|---|---|
| `customer.name` · `customer.phone` | course **attribuée** | le client du livreur |
| `order_total` | course attribuée | montant de la commande |
| `cash_to_collect_xof` | course attribuée | ce qu'il réclame au client |
| `?status=` | `GET /stores/{id}/orders` | les onglets marchand |
| `merchant_new_order` | notification | le marchand est prévenu |
| `POST …/orders/{id}/refuse` | marchand | refuser — **annule tout** |
| `/me/staff` | marchand | le personnel, réservé au propriétaire |
| `GET /stores/{id}/sales` | marchand | ventes par plat, 30 jours |
| `?q=` | `GET /stores/{id}/orders` | recherche dans les noms de plats |
| `fragile` · `pack_size` | plat, ligne, **point de collecte** | comment porter le paquet |
| `planned_duration_s` | course | durée par le réseau routier |
| `store_id` **facultatif** | `POST /orders` | le serveur choisit le point de vente le plus proche |
| `GET /merchants/{id}/menu` | client | la carte d'une enseigne, aux prix du plus proche |

> ⚠️ **Le contact du client n'apparaît QUE sur une course attribuée**, jamais dans la liste des courses disponibles. Un livreur qui parcourt les offres ne doit pas moissonner des numéros ; celui qui porte la commande voit son client. **Ne montrez pas le client sur une carte de la liste.**

---

## 🟠 4 bis. Ce que le dispatch marchand change dans la maquette CLIENTE

La navigation actuelle est *boutique → carte → panier*. Le dispatch ouvre une seconde entrée : *enseigne → carte du plus proche → panier*. Les deux coexistent, et l'API ne force ni l'une ni l'autre.

Si vous ajoutez l'entrée par enseigne, trois choses doivent apparaître à l'écran :

- **Quelle boutique a été retenue, et à quelle distance.** `GET /merchants/{id}/menu` la rend. Le client doit savoir d'où part son repas — c'est aussi ce qui rend « la plus proche » vérifiable plutôt qu'affirmé.
- **Que l'adresse de livraison décide.** Changer d'adresse peut changer de boutique, donc de prix. Un panier constitué avant le choix de l'adresse doit être **revalidé** après.
- **Que le `store_id` de la réponse fait foi.** Affichez le suivi sur celui-là, pas sur celui que l'écran de carte avait montré.

> ⚠️ **Ne mélangez pas les deux entrées dans un même panier sans le dire.** Un plat ajouté depuis une fiche boutique garde SA boutique ; un plat ajouté depuis une enseigne se résoudra à la commande. Deux plats de la même marque peuvent donc partir de deux comptoirs différents — c'est correct, mais il faut que le récapitulatif le montre.

---

## 🟡 4 ter. La passe du 10 septembre — ce qui a changé dans la maquette

### Le VTC entre dans la maquette, et pas dans cette API

Treize écrans (`vtc_*`, `ch_*`) décrivent une plateforme de course : composition d'un trajet à étapes, classes de véhicule, gains du chauffeur, assistant de course.

> **Ils n'ont aucun back-end, et n'en auront pas ici.** `dira-food-api` couvre la livraison de repas. Le VTC fera l'objet d'un service distinct, non commencé. Ce n'est pas un manque à combler — c'est un périmètre à ne pas confondre : un designer qui les voit côte à côte supposera qu'ils partagent le même réseau, et ils ne le partagent pas.
>
> Ce qu'ils partagent utilement : les **tokens**, les **composants**, l'appel de 30 s en tant que *motif d'interface*. Pas les contrats.

### Deux écrans marchand sont apparus : `m_staff` et `m_assign`

Ils tombent bien — l'API du personnel a été livrée entre-temps (contrat v1.3.0). Deux écarts subsistent :

- **Quatre rôles dessinés, deux servis.** `roleKitchen` et `roleTill` accordent *exactement* les mêmes droits : gardez deux libellés si c'est utile à lire, mais ne suggérez aucune différence de permissions. **`roleDriver` n'a rien derrière et n'aura rien** : sur cette plateforme un livreur est indépendant, il achète ses jetons et n'appartient à aucune enseigne.
- **Le téléphone d'un membre n'est pas rendu.** Il sert à ouvrir le compte, pas à peupler un annuaire.

### ✅ Les documents de conformité du livreur (`ag_docs`) — servis depuis la v1.7.0

Quatre pièces, comme la maquette les dessine. Une différence de **modèle** à reporter à l'écran : le permis et la pièce d'identité appartiennent au **livreur**, la carte grise et l'assurance à **chaque véhicule**.

> Un livreur avec deux motos a **deux assurances**. Si l'écran liste quatre lignes à plat, il ne saura pas dire **quelle** moto est couverte. Groupez : « Mes pièces » puis une section par véhicule.

Les cinq états sont servis. Deux pièges de dessin :

- **`expiring` n'est pas un défaut** — la pièce est encore valable. Un rappel, pas une alerte rouge.
- **`pending` reste `pending` quelle que soit la date.** Une pièce jamais regardée n'a jamais compté ; ne l'affichez pas « expirée ».

> ⚠️ **RIEN N'EST BLOQUÉ CÔTÉ SERVEUR — et c'est contraire à ce que demande le handoff.** Une pièce expirée n'empêche ni l'appel ni l'acceptation. C'est une décision produit assumée : l'exploitation suspend depuis sa file de conformité.
>
> **Conséquence pour le design : votre bannière est la SEULE barrière qui existe.** En tête d'écran, bloquante, pas un badge discret au fond d'un onglet. C'est exactement ce que le handoff réclamait, et c'est encore plus vrai maintenant que le serveur ne double pas la garde.

### 🟡 Ce qui reste ouvert sur la conformité

C'était déjà le point 3.5 de la v1.0 de ce brief ; la maquette l'a précisé — quatre pièces, quatre états, une date d'expiration — et **l'a durci** : le handoff demande que l'expiration **bloque le dispatch côté serveur**.

> C'est la bonne exigence, et c'est ce qui en fait un chantier plutôt qu'un écran : quelles pièces par pays, qui valide, quelle durée de conservation, qui relit, comment on supprime.
>
> ⚠️ **N'affichez aucun état par défaut.** Une pièce marquée « valide » parce que rien ne la contredit est exactement ce qu'un audit ne pardonne pas.

### ✅ La carte livreur du suivi client — servie depuis la v1.6.0

L'écran `tracking` propose **Appeler** et **Message** à côté du nom et de la note du livreur. **Les quatre existent** : `courier.name`, `courier.phone`, `courier.rating_avg` / `rating_count`, `courier.vehicle`.

> ⚠️ **DÉCISION PRODUIT, et elle tranche autrement que le handoff.** Celui-ci suppose « les deux numéros masqués derrière le bouton d'appel ». Il n'y a **pas de relais masqué** : chacun voit le numéro de l'autre sur une course attribuée. La plateforme répond de l'éthique de ses livreurs.
>
> Ce qui reste à dessiner correctement :
>
> - **`courier` est absent tant que personne n'a pris la course** — affichez l'étape « recherche d'un livreur », pas une carte vide.
> - **`rating_avg` jamais sans `rating_count`** : 5,0 sur un avis ne vaut pas 4,6 sur deux cents.
> - **`vehicle` est ce qu'on reconnaît dans la rue** (« moto · TG-4417 »), pas une fiche technique. C'est celui de la course, pas celui actif aujourd'hui.
> - Côté **livreur**, l'écran doit dire que le client le voit. Laisser croire à l'anonymat pendant que le client compose son numéro ferait décrocher personne.

### Trois choses que le handoff croit manquantes et qui existent

| Le handoff dit | En réalité |
|---|---|
| « la tombola tourne sur un mock » | **API complète** — tirages, participations, gains, crédit déduit d'une commande |
| « l'assistant doit rendre un plan typé, pas de la prose » | **Déjà le cas** — `POST /ai/chat` rend des `dish_id` résolus contre le catalogue, plus `unresolved` |
| « déplacer le minuteur d'appel côté serveur » | **Déjà fait** — `expires_at`, `attempt`, cascade de vagues, `409` sur accept tardif |

> Ces trois lignes du handoff datent d'avant la livraison. Les laisser telles quelles ferait re-spécifier trois fois ce qui tourne.

### ✅ Le sélecteur d'opérateur — `GET /payments/providers`, v1.7.0

L'écran `payment` propose Orange Money · Wave · MTN · Free. Le serveur dit désormais lesquels il sait **réellement** encaisser.

> ⚠️ **Le nombre d'opérateurs n'est pas quatre, et il changera.** Dessinez le sélecteur pour une liste **variable** : un seul élément ne mérite pas de sélecteur, une liste vide veut dire qu'aucun paiement en ligne n'est possible — proposez alors les espèces ou le solde Dira, jamais un écran de paiement vide.
>
> Le `label` vient du serveur : ne le codez pas dans l'application, sinon brancher un opérateur attendrait une revue de magasin.

---

## 5. Ce que la maquette fait bien, et qu'il ne faut pas perdre

À la relecture, plusieurs subtilités sont déjà là et méritent d'être préservées :

- **L'avertissement d'allergène** sur la fiche d'un plat (`Contient un allergène de votre profil`). C'est le croisement profil × plat, et il ne protège que si les marchands déclarent leurs allergènes — mais l'écran, lui, est juste.
- **L'appel de 30 s du livreur**, avec anneau décroissant et cascade. Le compte à rebours doit venir de `expires_at`, pas d'un timer local : une application mise en arrière-plan ne doit pas pouvoir le rallonger.
- **La conversation de commande**, sans numéro affiché. ⚠️ Elle **reste** le canal principal même maintenant que le livreur a le téléphone : le numéro est le **recours** quand personne ne répond, pas le premier geste. Ne remplacez pas le bouton « écrire » par un bouton « appeler ».
- **Le portefeuille à deux soldes** côté livreur — jetons et argent, jamais additionnés.
