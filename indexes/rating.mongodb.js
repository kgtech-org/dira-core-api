// Indexes for the rating module (ratings collection).
// Run with: mongosh <uri> indexes/rating.mongodb.js
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/platform/db/indexes.go et créés au démarrage de
// l'API ; un test refuse tout écart entre les deux.

// UNE note par commande et par cible. C'est cet index qui garantit l'unicité —
// pas un contrôle applicatif, que deux envois simultanés franchiraient tous
// les deux, faisant compter la note double dans la moyenne.
db.ratings.createIndex({ order_id: 1, target_type: 1, target_id: 1 }, { unique: true });

// Avis laissés sur un livreur ou une boutique, du plus récent au plus ancien.
db.ratings.createIndex({ target_type: 1, target_id: 1, _id: -1 });
