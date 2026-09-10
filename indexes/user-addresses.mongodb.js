// Index du carnet d'adresses (collection user_addresses).
// Run with: mongosh <uri> indexes/user-addresses.mongodb.js
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/platform/db/indexes.go et créés au démarrage de
// l'API ; un test refuse tout écart entre les deux.

// Le carnet d'un compte, l'adresse PAR DÉFAUT en tête : c'est celle que
// l'application présélectionne, et la faire chercher dans la liste serait
// absurde.
db.user_addresses.createIndex({ user_id: 1, is_default: -1, created_at: 1 });
