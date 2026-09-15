# Migrations de données

Des scripts `mongosh`, à lancer UNE fois par environnement, quand une
version change la forme des documents. Ils sont idempotents : relancés,
ils ne font rien.

| Script | Version | Ce qu'il fait |
|---|---|---|
| `2026-09-15-country.mongodb.js` | specs 4.2.0 | marque `country: "TG"` sur tout document qui n'en a pas (socle, livraison, courses) et retire l'index `key_1` de `vtc_classes` (une grille par pays) |

```sh
# depuis le conteneur Mongo (staging : dira-mongo)
mongosh -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin \
  --eval 'var CORE="dira_core", FOOD="dira_food", VTC="dira_vtc", CODE="TG"' \
  2026-09-15-country.mongodb.js
```
