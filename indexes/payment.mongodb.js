// MongoDB indexes for the payment module.
// Run with mongosh against the application database.
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés
// sont déclarés dans internal/platform/db/indexes.go et créés au démarrage
// de l'API ; un test refuse tout écart entre les deux. Toute ligne ajoutée
// ici doit l'être aussi là-bas.

// payments
db.payments.createIndex({ provider_ref: 1 }, { unique: true }); // webhook idempotency
db.payments.createIndex({ user_id: 1 });
db.payments.createIndex({ status: 1 });
