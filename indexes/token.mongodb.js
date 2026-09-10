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
// La référence d'un mouvement : le COUPLE (genre, identifiant). Deux
// verticales peuvent porter le même identifiant sans se confondre.
db.token_transactions.createIndex({ ref_kind: 1, ref_id: 1 }, { sparse: true });

// --- REPRISE DES LIGNES ANTÉRIEURES ---------------------------------------
//
// Le champ s'appelait `order_id`, du temps où une seule verticale existait.
// Sans cette reprise, les mouvements déjà écrits perdent leur rattachement :
// le solde reste juste, mais un support qui remonte « pourquoi ce débit ? »
// ne trouve plus la commande.
//
// Idempotente : elle ne touche que les lignes qui portent encore l'ancien
// champ. À passer une fois, puis à laisser — la relancer ne coûte rien.
//
//   db.token_transactions.updateMany(
//     { order_id: { $exists: true } },
//     [{ $set: { ref_id: "$order_id", ref_kind: "order" } }, { $unset: "order_id" }],
//   );


// wallet_operations — la garde d'idempotence des mouvements d'argent.
//
// L'unicité vient du `_id` (la clé dérivée de l'opération : order_payment:<id>,
// order_refund:<id>, earnings:<motif>:<owner>:<order>, consume:…), donc aucun
// index à créer pour elle : Mongo l'impose déjà.
//
// Le TTL efface les marqueurs au bout de 30 jours — bien au-delà de tout
// reessai plausible, et sans laisser grossir une collection que personne ne lit.
db.wallet_operations.createIndex({ at: 1 }, { expireAfterSeconds: 2592000 });
