# Dira Chauffeur — remédiation de l'audit du 12 septembre 2026

> Périmètre : application Android (`dira-driver-android`, branche `feat/settings-and-online-confirm`), service de suivi (`dira-tracking`), socle (`dira-core-api`), déploiement (`dira-devops`).
> Statut : **côté serveur, livré** (12 septembre 2026, soir) — voir §6. Chaque lot est livrable seul ; l'ordre est celui du risque.
>
> Ce document vit dans `docs/audits/`, pas dans `docs/frontend/` : ce dossier-là
> n'embarque que les contrats de rôle, à une seule version, et un test le garde.

---

## 1. Ce que l'audit a mesuré

| Sujet | Constat |
|---|---|
| Plantages | Aucun `!!` non gardé, pas de `runBlocking`/`GlobalScope`. `TokenStore` replie en clair si le Keystore est cassé. Le socket a un `pingInterval` de 25 s. La carte suit le cycle de vie. Deux pertes de fonction silencieuses (§2, P0). |
| Lint | 27 erreurs, 76 avertissements ; `lintVitalRelease` passe, le build release n'est pas bloqué. |
| Bundle | Debug 25 Mo. **Release 3,25 Mo** non signé (R8 + `shrinkResources`) : dex 4,1 Mo, `resources.arsc` 0,61 Mo, polices 0,47 Mo. Profils de démarrage fusionnés. |
| Réseau pendant les tests | Deux coupures vers `api-staging.dira.llc` (timeout de connexion), `dira.llc` en 502 Cloudflare. Voir §3.5. |

Ce document ne reprend pas ce qui est déjà corrigé dans la branche : démarrage du service de suivi sans permission de localisation (plantage d'origine), démarrage refusé en arrière-plan (relance au premier plan), mise à jour du widget.

---

## 2. Application Android — corrections

### P0-A · Sonnerie muette sur Android 8.0 / 8.1

**Où.** `service/CallAlerter.kt`, `ring()`.
**Quoi.** `Ringtone.isLooping` n'existe qu'à partir de l'API 28 ; `minSdk` est 26. Sur Android 8, `runCatching` avale le `NoSuchMethodError` **avant** `play()` : l'appel arrive sans un son. La règle du fichier dit pourquoi c'est grave : « un appel silencieux est un appel manqué, et un chauffeur qui n'a pas répondu n'est pas rappelé ».
**Correctif.**
- `Build.VERSION.SDK_INT >= 28` : `Ringtone` avec `isLooping = true` (inchangé).
- En dessous : `MediaPlayer` sur la même URI (`setDataSource(context, uri)`, `isLooping = true`, mêmes `AudioAttributes`, `prepare()`, `start()`), libéré dans `stopRinging()`.
- Un seul objet `Player` interne avec `play()` / `stop()` pour que `ring()` et `stopRinging()` ne connaissent pas la version.
**Acceptation.** Émulateur API 26 : un appel sonne en boucle jusqu'à `stopRinging()`. API 34 : inchangé.

### P0-B · Jeton expiré = socket de suivi mort en silence

**Où.** `core/tracking/TrackingSocket.kt`, `open()` / `onFailure`.
**Quoi.** Le socket lit `tokens.accessToken()` à l'ouverture ; seul `TokenAuthenticator` (REST) rafraîchit le jeton. En ligne sans course, aucun appel REST ne part. À l'expiration, la poignée de main répond 401, `scheduleReconnect()` réessaie **avec le même jeton**, indéfiniment : plus de positions, plus d'appels, l'accueil affiche « reconnexion ».
**Correctif (client).**
- `TrackingSocket` reçoit un `refreshToken: suspend () -> String?` (fourni par `AuthRepository`, qui réutilise `POST /auth/refresh` et `TokenStore.save`).
- Dans `onFailure`, si `response?.code` ∈ {401, 403}, ou si la fermeture porte le code **4401** (§3.1) : `scheduleReconnect(refreshFirst = true)` → le job appelle `refreshToken()` avant `open()`. Si le rafraîchissement échoue (refresh expiré), on déconnecte et on appelle `onSessionLost` — le même chemin que `TokenAuthenticator` — pour que l'app revienne à la connexion plutôt que de tourner.
- Un compteur d'échecs consécutifs d'authentification (max 3) évite une boucle si le serveur refuse pour une autre raison.
**Acceptation.** Test unitaire avec `MockWebServer` : 401 à la poignée de main → un appel `refresh` → reconnexion avec le nouveau jeton. Sur appareil : forcer l'expiration (§3.2, TTL court en staging) et vérifier que le widget repasse à « connecté » sans intervention.

### P1-A · Écran d'appel plein écran sur Android 14+

**Où.** `CallAlerter.ring()`, écran de permission, `HomeScreen`.
**Quoi.** `USE_FULL_SCREEN_INTENT` peut être retirée par Play aux apps hors catégorie appel/alarme ; sans elle, seule la notification s'affiche.
**Correctif.** À `Build.VERSION.SDK_INT >= 34`, vérifier `NotificationManager.canUseFullScreenIntent()` au premier passage en ligne ; si faux, une feuille `DiraSheet` explique et ouvre `Settings.ACTION_MANAGE_APP_USE_FULL_SCREEN_INTENT`. Ne pas bloquer le passage en ligne.

### P1-B · Notifications refusées

**Où.** `CallAlerter.ring()`.
**Quoi.** `POST_NOTIFICATIONS` refusable, et l'écran de permission a « Plus tard ». `notify()` échoue alors sans bruit.
**Correctif.** `NotificationManagerCompat.areNotificationsEnabled()` avant `notify()` ; si faux, une bannière sur l'accueil (« Les appels n'apparaîtront pas en arrière-plan ») avec lien vers les réglages, tant qu'on est en ligne.

### P1-C · Cache de tuiles osmdroid

**Où.** `DiraApp.onCreate()`.
**Quoi.** Valeurs par défaut : 600 Mo max, purge à 500 Mo, dans `cacheDir`.
**Correctif.** `tileFileSystemCacheMaxBytes = 150L * 1024 * 1024`, `tileFileSystemCacheTrimBytes = 100L * 1024 * 1024`.

### P1-D · Cadence GPS

**Où.** `core/location/LocationClient.kt`, `updates(intervalMs)`.
**Quoi.** `PRIORITY_HIGH_ACCURACY` toutes les 3 s en permanence. Le serveur appelle « les chauffeurs les plus proches », pas au mètre près.
**Correctif.** Deux cadences pilotées par `OnlineService` selon `session.activeRide` : **6 s** en attente, **3 s** en course (`collectLatest` sur `activeRide` → réabonnement). Décision produit à confirmer ; le fichier disait déjà « à mesurer sur le terrain ».

### P2 · Au fil de l'eau

| Item | Où | Correctif |
|---|---|---|
| `StateFlow.value` en composition | `ProfileScreen.kt:50`, `DiraWidget.kt:204` | `collectAsStateWithLifecycle` ; pour le widget, lire `navApp` dans `provideGlance` et le passer en paramètre |
| `context.getString()` en composition (24) | écrans | `stringResource` |
| `startActivityAndCollapse(Intent)` déprécié | `OnlineTileService.kt:93` | `@SuppressLint("StartActivityAndCollapseDeprecated")` sur la branche < 34 (déjà correcte) |
| `getParcelableExtra` déprécié | `AccountScreen` | `IntentCompat.getParcelableExtra` |
| Dépendances sans usage | `app/build.gradle.kts` | retirer `glance-material3`, `compose-ui-text-google-fonts` |
| 19 ressources inutilisées | `strings.xml` fr/en | purger (`account_help`, `account_section_account`, `account_profile*`, …) |
| 12 dépendances à mettre à jour | `libs.versions.toml` | par lot, test appareil |
| Attribut `windowLayoutInDisplayCutoutMode` (API 27) | `themes.xml` | déplacer dans `values-v27` |

### Bundle

| Prio | Action | Gain |
|---|---|---|
| P1 | `android { androidResources { localeFilters += listOf("fr", "en") } }` | −0,3 à −0,4 Mo (`resources.arsc`) |
| P1 | Livrer en **AAB** (`bundleRelease`) ; `versionCode` et `signingConfig` injectés par la CI (`DIRA_VERSION_CODE`, keystore en secret) | −10 à −20 % à l'installation ; livrabilité |
| P2 | JetBrains Mono → `FontFamily.Monospace` pour les plaques | −0,18 Mo |

---

## 3. Côté serveur — instructions

### 3.1 Suivi (`dira-tracking`) — `WS /track/agent`

1. **Fermer proprement à l'expiration du jeton.** Quand le jeton de la poignée de main expire pendant la connexion (ou est révoqué), fermer le socket avec le code **4401** et la raison `token_expired`, plutôt que de laisser la connexion vivre avec une identité périmée ou de couper sans code. Le client (P0-B) rafraîchit et se reconnecte sur ce code.
2. **Répondre 401 à la poignée de main** avec un jeton invalide (déjà le cas, à confirmer), jamais 403 ni 200-puis-fermeture : le client distingue « rafraîchir » (401) de « pas le droit » (403 : chauffeur suspendu, mauvaise verticale).
3. **Chauffeur en ligne sans positions = hors du vivier.** Un chauffeur `online=true` dont la dernière position date de plus de **90 s** ne doit plus être appelé (cas rencontré : service de suivi refusé par Android, socket mort, téléphone en veille profonde). Exposer sur le profil chauffeur (`GET /vtc/drivers/me`) un champ `last_seen_at` et un booléen dérivé `tracking_stale`, pour que l'app et le back-office montrent la même chose que le widget (« suivi arrêté »).
4. **Passage hors ligne automatique** après **10 min** sans position, avec `offline_reason: "stale"` sur le profil, pour que l'app puisse l'expliquer (« Vous avez été mis hors ligne : position non reçue depuis 10 min ») au lieu d'afficher un état incohérent.
5. **Ping/pong.** Le client envoie un ping OkHttp toutes les 25 s ; répondre au pong et fermer côté serveur après 60 s sans trafic, pour libérer les connexions mortes.

### 3.2 Socle (`dira-core-api`) — jetons

1. **Documenter la durée de vie** de l'access token et du refresh token dans `docs/frontend/VTC-DRIVER.md` (aujourd'hui non écrite ; le client l'a découverte par l'invalidation en cours de session).
2. **Staging : access token court (10 min)** pour que l'expiration en ligne soit testable ; production selon la politique existante.
3. `POST /auth/refresh` doit rester utilisable **pendant qu'une connexion socket est ouverte** avec l'ancien jeton (pas de rotation qui invalide instantanément l'ancien) : le client reconnecte le socket après le rafraîchissement, avec un court chevauchement.
4. `preferences.locale` : accepter `fr` et `en` uniquement (le client n'envoie que ces deux valeurs) ; `preferences.sounds` n'est plus écrit par l'app chauffeur (réglage devenu local au téléphone, avec le vibreur).

### 3.3 Appel de course : le socket ne suffit pas — ajouter la poussée FCM

**Pourquoi.** Tout appel passe par le socket. Si le socket est mort (jeton, réseau, service refusé, veille profonde), l'appel n'arrive pas et le chauffeur ne sait même pas qu'il a raté quelque chose. L'audit a rencontré trois de ces quatre cas en une matinée.

**Quoi.**
1. Le socle expose déjà `POST /me/devices` (TASKS.md §FCM, non fait côté app). L'app enregistrera son jeton FCM à la connexion et à chaque rotation.
2. À chaque `call`, le suivi envoie **en plus** un message FCM **data-only, priorité haute**, au chauffeur appelé : `{type: "call", call_id, ref, expires_at}` — le même contenu que la trame socket, sans `notification` (l'app construit elle-même la notification plein écran via `CallAlerter`).
3. À réception, l'app : reconnecte le socket si nécessaire, puis traite la trame comme si elle venait du socket (`DriverSession.onCallFrame`). Idempotence par `call_id` : une trame reçue deux fois (socket + push) ne sonne qu'une fois.
4. `call_closed` : même chose, pour couper la sonnerie si le socket est tombé entre-temps.

**Prérequis.** `google-services.json` dans le dépôt Android (hors dépôt aujourd'hui) ; clé serveur FCM dans `dira-devops` (`stacks/30-tracking`), secret `FCM_SERVICE_ACCOUNT`.

### 3.4 Statistiques du jour (`dira-vtc`) — facultatif

L'app calcule « gains du jour / courses du jour » depuis `GET /vtc/rides?limit=50` (jour local, courses terminées). Au-delà de 50 courses par jour, ou pour éviter de télécharger l'historique au démarrage, exposer `GET /vtc/drivers/me/stats?date=YYYY-MM-DD&tz=Africa/Lome` → `{rides: n, driver_xof: n, online_s: n}`. Le temps en ligne côté serveur (somme des périodes `online`) remplacerait le compteur local du service, plus fiable après un redémarrage du téléphone.

### 3.5 Passerelle et déploiement (`dira-devops`)

1. **Santé exposée** : `GET https://api-staging.dira.llc/api/v1/healthz` répond 404 ; `api-edge` n'a `/healthz` que pour le healthcheck Compose. Exposer une route de santé publique (`/api/v1/healthz` → socle, courses, suivi) pour la supervision et pour que le client puisse distinguer « pas de réseau » de « service en panne » (§P1 : l'écran « Quelque chose n'a pas marché » ne fait pas la différence aujourd'hui).
2. **`dira.llc` en 502** pendant toute la matinée : aucun service derrière l'hôte dans Nginx Proxy Manager. Soit le stack `60-web` du site (spec séparée), soit une page de maintenance statique en attendant — jamais une erreur Cloudflare sur le domaine racine.
3. **Coupures `api-staging`** : deux timeouts de connexion (10 s) observés depuis le téléphone et depuis un poste, à 10:16 et 10:17. Vérifier la santé de l'upstream `api-edge` dans NPM (healthcheck sur `/healthz`, § ci-dessus) et les journaux Cloudflare (erreurs 52x) sur cette plage.

---

## 4. Lots et ordre

| Lot | Contenu | Dépend de |
|---|---|---|
| **1 — Appels fiables** (app) | P0-A sonnerie Android 8 · P0-B rafraîchissement du jeton socket · P1-A plein écran · P1-B notifications | §3.1.1–2, §3.2.2 pour tester |
| **2 — Suivi honnête** (serveur) | §3.1.3–5 `last_seen_at` / `tracking_stale` / hors ligne automatique · §3.2.1 TTL documentés | — |
| **3 — Bundle et hygiène** (app) | `localeFilters` · AAB + CI · cache osmdroid · cadence GPS · dépendances et ressources inutiles · P2 | — |
| **4 — Poussée FCM** (app + serveur) | §3.3 en entier | `google-services.json`, secret FCM |
| **5 — Stats serveur** (facultatif) | §3.4 | — |

Le lot 1 se teste sur le Pixel 7 Pro et un émulateur API 26 ; il ne modifie aucun contrat d'API.

---

## 5. Critères d'acceptation

- Émulateur API 26 : un appel entrant **sonne** ; API 34 : inchangé.
- Staging avec access token à 10 min : chauffeur en ligne, sans toucher au téléphone pendant 12 min → le widget reste « En ligne · connecté », les positions continuent (visible dans `tracking-web`), un appel de test arrive.
- Refresh token révoqué côté serveur → l'app revient à l'écran de connexion en moins d'une minute, sans boucle de reconnexion.
- Socket coupé volontairement côté serveur avec 4401 → reconnexion avec un nouveau jeton en moins de 5 s.
- Chauffeur en ligne dont le service de suivi est tué (`am force-stop` puis réveil par le widget) : après 90 s il n'est plus appelé (§3.1.3) ; après 10 min il est hors ligne côté serveur et l'app l'affiche avec la raison (§3.1.4).
- `lintDebug` : 0 erreur (les avertissements P2 peuvent rester dans une baseline).
- APK release ≤ 3 Mo ; AAB produit par la CI avec `versionCode` injecté.
- Android 14 sans autorisation plein écran : la feuille d'explication s'affiche au passage en ligne, une fois.

---

## 6. Ce qui est livré côté serveur (12 septembre 2026)

| Instruction | Où | État |
|---|---|---|
| §3.1.1 fermeture **4401 `token_expired`** à l'échéance du jeton | `dira-tracking` `wsx.closeAtExpiry` | ✅ testé |
| §3.1.2 **401** jeton invalide / **403** rôle interdit à la poignée de main | `dira-tracking` `wsx.Ingest` | ✅ testé |
| §3.1.3 hors du vivier sans position ; `last_seen_at` + `tracking_stale` (90 s) sur `GET /vtc/drivers/me` | `dira-vtc-api` `driver.IsStale`, `Me()` lit le suivi à l'instant | ✅ testé |
| §3.1.4 hors ligne automatique avec `offline_reason` | `dira-vtc-api` balayage de présence — **5 min** (décision d'exploitation du 12/09, `PRESENCE_SILENCE_LIMIT`), raison **`stale`** | ✅ testé |
| §3.1.5 ping 25 s / fermeture 60 s | `dira-tracking` `keepAlive` + `pongWait` | ✅ déjà en place, documenté |
| §3.2.1 durées de vie documentées | `openapi.yaml` `/auth/refresh`, `VTC-DRIVER.md` v3.1.0 | ✅ |
| §3.2.2 access token **10 min** en staging | `dira-devops` `CORE_JWT_ACCESS_TTL` (stack 45-core) + `.env` staging | ✅ |
| §3.2.3 refresh sans invalider l'access en cours | déjà le cas (JWT sans état), documenté | ✅ |
| §3.2.4 `locale` ∈ fr · en | `dira-core-api` `PATCH /me/preferences` | ✅ |
| §3.3 poussée FCM `call` / `call_closed` | socle `POST /internal/push/data` (data-only, TTL) ; suivi `dispatch` → socle pour chaque appelé connu | ✅ serveur — reste `google-services.json` + `FCM_SERVICE_ACCOUNT` (socle) côté déploiement |
| §3.4 `GET /vtc/drivers/me/stats` | `dira-vtc-api` `driver/stats.go` (périodes en ligne persistées) | ✅ testé |
| §3.5.1 `GET /api/v1/healthz` public | `dira-devops` passerelle | ✅ |
| §3.5.2 `dira.llc` en 502 | NPM — pas de service derrière l'hôte racine | ⏳ décision : page de maintenance ou stack `60-web` |
| §3.5.3 coupures `api-staging` 10:16–10:17 | à corréler côté Cloudflare ; `api-edge` est **healthy** depuis la correction du healthcheck (`localhost` → `127.0.0.1`) | ⏳ |
