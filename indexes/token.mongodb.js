// MongoDB indexes for the token module.
// owner_id is a user id for "driver" wallets and a store id for "merchant"
// wallets: one wallet per owner, enforced by the unique index.
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés
// sont déclarés dans internal/platform/db/indexes.go et créés au démarrage
// de l'API ; un test refuse tout écart entre les deux. Toute ligne ajoutée
// ici doit l'être aussi là-bas.

db.token_wallets.createIndex({ owner_id: 1 }, { unique: true });

db.token_transactions.createIndex({ wallet_id: 1 });
db.token_transactions.createIndex({ created_at: 1 });
db.token_transactions.createIndex({ order_id: 1 }, { sparse: true });

// wallet_operations — la garde d'idempotence des mouvements d'argent.
//
// L'unicité vient du `_id` (la clé dérivée de l'opération : order_payment:<id>,
// order_refund:<id>, earnings:<motif>:<owner>:<order>, consume:…), donc aucun
// index à créer pour elle : Mongo l'impose déjà.
//
// Le TTL efface les marqueurs au bout de 30 jours — bien au-delà de tout
// reessai plausible, et sans laisser grossir une collection que personne ne lit.
db.wallet_operations.createIndex({ at: 1 }, { expireAfterSeconds: 2592000 });
