# Collection `.rest` — Dira Core API

Requêtes HTTP exécutables couvrant **l'intégralité des routes du socle** (70 au
moment de l'écriture, `/internal` compris), organisées **par parcours** plutôt
que par module : on lit un fichier de haut en bas comme on suit un scénario.

Complémentaire du contrat `api/openapi.yaml` (servi sur `/docs`) : celui-ci
décrit *ce qui existe*, ces fichiers montrent *dans quel ordre s'en servir* —
et ils couvrent la surface de service, que le contrat tait volontairement.

## Outillage

Extension VS Code **REST Client** (`humao.rest-client`) :

```bash
code --install-extension humao.rest-client
```

Ouvrir un `.rest`, cliquer sur **Send Request** au-dessus d'une requête
(`⌘⌥R` / `Ctrl+Alt+R`). `###` sépare les requêtes.

> JetBrains (IntelliJ / GoLand) exécute aussi ces fichiers, mais **pas** le
> chaînage `{{requête.response.body.$.champ}}`, propre à REST Client. Sous
> JetBrains, renseigner les `@variables` à la main.

## Configuration

```bash
cp .rest/.env.example .rest/.env   # jamais versionné (cf. .gitignore)
```

Trois valeurs : le mot de passe administrateur (**aucune valeur par défaut**
côté serveur — `SEED_ADMIN_PASSWORD` du `.env` de la stack), le secret des
routes de service (`CORE_SERVICE_TOKEN`) et celui du prestataire simulé
(`MOCK_PAYMENT_SECRET`). Les deux derniers ont leurs valeurs de dev préremplies.

L'hôte est déclaré en tête de chaque fichier — une ligne à changer :

| Environnement | `@host` | Note |
|---|---|---|
| Stack de dev, core-api en direct | `http://localhost:8092` | **la seule où `/internal` répond** |
| Stack de dev, via la passerelle | `http://localhost:8095` | mêmes chemins ; `/internal` → 404 |
| Staging | `https://api-staging.dira.llc` | idem passerelle |
| `go run ./cmd/api` en direct | `http://localhost:8082` | |

> Derrière la passerelle, le socle est servi à la **racine** `/api/v1/…` — pas
> de préfixe de verticale, contrairement à `/api/v1/food/…` et `/api/v1/vtc/…`.
> L'authentification et le portefeuille sont les mêmes pour les deux métiers.

## Fichiers

| Fichier | Qui | Contenu |
|---|---|---|
| `00-public.rest` | anonyme, tous rôles | santé, `/docs`, inscription, connexion (téléphone / email), rotation, déconnexion, notes publiques |
| `10-account.rest` | tout compte | profil, préférences, carnet d'adresses, appareils, boîte de réception |
| `20-wallet-payments.rest` | livreur, client, admin | jetons du livreur, Dira Cash du client, opérateurs, initiation, statut, **webhook signé**, confirmation sandbox |
| `40-admin.rest` | `admin` | annuaire et fiches (ouvrir, corriger, suspendre, supprimer), portefeuilles, gabarits, flottes, staff |
| `50-service.rest` | **secret de service** | `/internal/*` : comptes, portefeuilles, notifications, paiements, back-office, notes, flottes |

## Chaînage

Chaque fichier est **autonome** : il ouvre sa propre session, puis réutilise
les identifiants renvoyés par les réponses précédentes.

```http
# @name login
POST {{baseUrl}}/auth/login
…

###
@token = {{login.response.body.$.access_token}}
```

Conséquence : **exécuter dans l'ordre à la première passe**. Une requête qui
répond `404` ou `422` sur un identifiant `{{…}}` non résolu signifie que la
requête qui l'alimente n'a pas encore été jouée.

## Comptes de démonstration

Le socle sème **l'administrateur seul** (`make seed-core ENV=dev`) ; les autres
comptes viennent du seed de la livraison (`make seed-food ENV=dev`), qui les
ouvre via `/internal/accounts/ensure`. Mot de passe commun : **`dira12345`**.

| Rôle | Identifiant | Notes |
|---|---|---|
| admin | `dev@dira.llc` (`+22890000100`) | connexion **par email**, mot de passe = `SEED_ADMIN_PASSWORD`, portées `core · food · vtc` |
| client | `+22890200001` → `+22890200005` | Dira Cash ouvert à la première lecture |
| livreur | `+22890100001` → `+22890100004` | portefeuille de jetons |
| marchand | `+22890300001` → `+22890300004` | le portefeuille est par point de vente (`?store_id=`) |

## Points d'attention

- **Coordonnées** en `[lng, lat]` dans les corps JSON (GeoJSON).
- **Montants** en francs CFA **entiers** ; les jetons en unités.
- **Rien n'est crédité sur la réponse d'une initiation.** Seul le rappel
  signé du prestataire (`POST /webhooks/payment/{provider}`) ou, hors
  production, `POST /admin/payments/{id}/confirm` fait passer un paiement en
  `succeeded` et crédite — jetons ou Dira Cash selon l'objet.
- **Le webhook est signé sur les octets bruts** : la commande `openssl` est
  dans `20-wallet-payments.rest`, et `curl` y est plus fiable que l'éditeur.
- **Les codes métier traversent** la surface de service tels quels
  (`insufficient_tokens`, `insufficient_funds`, `wallet_not_found`,
  `phone_taken`) : c'est ce qu'une verticale rend à son tour au client.
- **Suspendre ne ferme pas les sessions en cours** : les verticales vérifient
  le jeton localement. Le refresh est refusé, la porte se referme à
  l'expiration de l'accès (15 min).
- **`scopes` vide n'accorde rien** sur une fiche staff ; couvrir toute la
  plateforme s'écrit en énumérant `core`, `food`, `vtc`.
- Les requêtes **destructives** portent un `⚠️` : suppression d'un compte,
  retrait d'une fiche staff.

## Vérification

La collection a été rejouée de bout en bout contre la stack de dev
(`dira-devops`, `ENV=dev`) : **108 requêtes, 108 réponses conformes** —
statuts nominaux, contrôles négatifs (401 / 403 / 404 / 409 / 422 / 402),
et garde-fous de la passerelle. Le script qui l'a fait est un `curl` par
ligne ; ces fichiers en sont la version lisible.
