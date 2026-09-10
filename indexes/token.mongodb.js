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
