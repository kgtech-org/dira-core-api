// MongoDB indexes for the user module.
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés
// sont déclarés dans internal/platform/db/indexes.go et créés au démarrage
// de l'API ; un test refuse tout écart entre les deux. Toute ligne ajoutée
// ici doit l'être aussi là-bas.

// Phone (E.164) is the primary identifier.
db.users.createIndex({ phone: 1 }, { unique: true });
db.users.createIndex({ role: 1 });
// La borne PAYS des listes de la console — voir pkg/country.
db.users.createIndex({ country: 1, role: 1, _id: 1 });

// Les comptes dont la suppression est demandée — le balayage de l'effacement
// (voir internal/user/erasure.go). PARTIEL : les comptes fermés sont une
// poignée, un index complet aurait indexé toute la base pour eux.
db.users.createIndex(
  { deletion_requested_at: 1 },
  { partialFilterExpression: { deletion_requested_at: { $exists: true } } },
);

// Refresh tokens are stored as sha256 hashes and rotated on every refresh.
db.refresh_tokens.createIndex({ token_hash: 1 }, { unique: true });
// Les sessions d'un compte, la plus récemment active en tête — l'ordre dans
// lequel la borne d'appareils évince la plus silencieuse.
db.refresh_tokens.createIndex({ user_id: 1, created_at: -1 });
// TTL: expired refresh tokens are purged automatically.
db.refresh_tokens.createIndex({ expires_at: 1 }, { expireAfterSeconds: 0 });

// email is optional but unique WHEN PRESENT AND NON-EMPTY (back-office login).
//
// A partial filter, not `sparse`: sparse only skips MISSING fields, never an
// empty string. Signup is by phone, so most accounts have no email — two of
// them carrying `email: ""` were enough to stop the index from building, and
// the service started without it.
//
// `$gt: ""` selects non-empty strings; `$ne` is not allowed in a partial filter.
db.users.createIndex(
  { email: 1 },
  { unique: true, partialFilterExpression: { email: { $gt: "" } } },
);
