# Côté serveur — ce qui est réglé, ce qui reste, et ce que le client a décidé sans vous

> Relevé au curl sur `https://api-staging.dira.llc/api/v1` le **29 septembre 2026**,
> compte chauffeur de recette `+228 91000103`.
> Client : **app CHAUFFEUR Android**, à jour du contrat **VTC-DRIVER v4.35.0**.
>
> Ce document REMPLACE celui du 26 septembre, auquel vous avez répondu. Tout ce
> que vous aviez corrigé est vérifié et clos ici — pour ne pas y revenir.

---

## A. Clos depuis votre réponse — revérifié le 29/09

| Point | Attendu | Observé |
|---|---|---|
| `tariff_version` sur le profil | l'empreinte de la grille | `GET /vtc/drivers/me` → `cec1c2074297ffdf`, **identique** à `tariffs/snapshot` ✅ |
| Course libre à 0 F | véhicule inexistant refusé | `403 vehicle_not_yours` ✅ |
| Images de classe | trois fichiers distincts, zénithaux | trois URLs différentes servies ✅ |
| `speed_limit_kmh` | servi, `0` = pas d'alerte | `GET /vtc/settings/dispatch` → `0` ✅ |
| `tiers` / `surge` | formes décrites | décrites au contrat, typées côté client ✅ |
| Cadence `normal` | 30 s / 40 m | servi ✅ — **notre plancher local est retiré** |

**Rien à faire de votre côté sur ces six points.**

---

## B. Ce que nous avons vérifié des v4.34.0 et v4.35.0 — **tout marche**

C'est écrit ici pour que vous sachiez que le client s'y conforme, et pour que
ces comportements ne soient pas changés sans préavis : ils sont désormais
**épinglés par des tests** chez nous.

### B1 · `X-Request-ID` — les quatre comportements du contrat sont exacts

```
X-Request-ID: a1f9c2-1874                    → repris TEL QUEL
X-Request-ID: pas valide/avec espaces        → REFUSÉ, le vôtre : e5600612dcd7fef2
(aucun en-tête)                              → le vôtre : 73d8a27209461c76
X-Request-ID: test-erreur-42 + 401           → repris TEL QUEL sur l'erreur aussi
```

⚠️ Le dernier cas est celui qui compte : **le fil survit au refus**. C'est ce
qui permet de citer un `401` ou un `500` dans un ticket sans rien chercher.

**Ce que le client fait** : il émet le sien (`<installation raccourcie>-<compteur>`,
jamais de donnée personnelle), le **garde identique au rejeu** après
rafraîchissement de jeton — les deux tentatives se relisent comme une seule
histoire — et enregistre avec l'erreur **le fil que vous avez retenu**, pas
celui qu'il a envoyé.

### B2 · `device_id` — la règle est exacte, y compris le refus

```jsonc
// connexion A
"session": { "device_id": "aaaa-…", "device_name": "Tecno Spark 10 · Android 13",
             "single_device": true }                      // pas de superseded_* ✅

// connexion B, autre appareil
"session": { "device_id": "bbbb-…", "device_name": "Itel A70", "single_device": true,
             "superseded_device_id": "aaaa-…",
             "superseded_device_name": "Tecno Spark 10 · Android 13" }   // ✅

// puis, avec le jeton de A
401 { "error": { "code": "session_superseded",
                 "message": "this device is no longer the one signed in to this account",
                 "reason": "Itel A70" } }                  // `reason` = le nom ✅
```

⚠️ **`reason` porte bien le NOM**, et c'est ce que notre écran affiche mot pour
mot. Un identifiant brut y serait inutilisable.

**Ce que le client fait** : il n'**enchaîne pas** de reconnexion, ne
**rafraîchit pas** (le rafraîchissement rendrait le même code et la boucle
tournerait jusqu'à vider la batterie), traite `4409` **différemment** de
`4401`, et **ne vide jamais sa file hors ligne** sur ce refus.

---

## C. Ce qui reste ouvert

### C1 · `message` en anglais sur `session_superseded`

```
"message": "this device is no longer the one signed in to this account"
```

Toutes les autres enveloppes de refus sont traduites (`Ce véhicule n'est pas le
vôtre`, `Authentification requise`, `Ressource introuvable`). Celle-ci ne l'est
pas, alors que le contrat dit d'afficher le message tel quel quand l'application
n'a pas le sien.

**Sans gravité pour nous** — notre écran a sa propre phrase, et c'est ce que le
contrat recommande pour ce refus précis. **Mais une application qui suivrait la
règle générale afficherait de l'anglais à un chauffeur togolais.**

### C2 · `nav_icon_url` est vide partout — et nous n'en avons plus besoin

Vous l'aviez noté comme « reste ouvert ». **C'est clos de notre côté** : la
navigation dessine désormais un **indicateur au sol** de notre fabrication, pas
une silhouette de véhicule. Le champ n'est plus lu ni téléchargé.

Deux conséquences pour vous : **ne produisez pas ces images**, et sachez que
c'est une divergence assumée si une autre application les utilise un jour.

### C3 · Ce que personne n'a pu éprouver

- **`POST /rides/sync` avec de vrais éléments** : jamais appelé autrement qu'avec
  une liste vide (`422`, la route existe). Toute la mécanique hors ligne —
  idempotence par `client_ref`, les quatre `outcome`, le recadrage de
  `server_time`, `auto_closed` — est écrite et testée **contre le contrat**, pas
  contre le serveur. C'est le seul gros morceau dans ce cas.
- **`backfill: true`** sur le socket de suivi : même remarque.
- La raison est la même pour les deux : les éprouver demande de **créer des
  courses** sur la recette, ce que l'exploitation nous a interdit.

**Ce dont nous aurions besoin** : soit un feu vert pour créer quelques courses
de test, soit un jeu de données que vous poussez de votre côté et que nous
resynchronisons.

---

## D. Les décisions du client, à valider ou à refuser

| | Décision | Pourquoi |
|---|---|---|
| **D1** | Le fil de requête est gardé **à côté de la file hors ligne**, pas seulement dans les journaux de l'appareil | Un envoi remonte plusieurs courses ; si l'une est refusée et que le relevé arrive trois jours plus tard, seul ce fil mène à ce que vous avez vu |
| **D2** | Une issue de `/rides/sync` **inconnue est gardée**, pas jetée | Garder coûte une tentative, jeter coûte une journée de travail. ⚠️ Corollaire : un nouvel `outcome` terminal doit venir **avec préavis** |
| **D3** | Un compteur hors ligne abandonné est **fermé par le client** à `max_hours`, avec `auto_closed: true` | Vous ne pouvez fermer que ce que vous connaissez ; celui-là n'existe nulle part chez vous |
| **D4** | L'estimation du compteur est calculée localement et **étiquetée « estimation »** | Le prix qui fait foi est le vôtre, à la fermeture |
| **D5** | Les majorations (`surge`) **ne sont pas appliquées** au compteur ni à la location | Le contrat le dit ; c'est noté ici pour que ça ne change pas en silence |

---

## E. Ce que nous attendons de vous, par ordre

1. **C3** — un moyen d'éprouver la resynchronisation pour de vrai. C'est le seul
   endroit où du travail réel peut disparaître, et il n'a jamais rencontré votre
   serveur.
2. **C1** — traduire le `message` de `session_superseded`.
3. **D2** — confirmer que tout nouvel `outcome` terminal viendra avec un préavis.
4. **C2** — acter que `nav_icon_url` n'a pas à être produit.
