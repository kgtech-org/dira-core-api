// Index de la PORTE PAR CODE (collection auth_otp_codes).
// Run with: mongosh <uri> indexes/auth-otp.mongodb.js
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/indexes/indexes.go et créés au démarrage de l'API ;
// un test refuse tout écart entre les deux.

// Un seul code vivant par numéro : une nouvelle demande REMPLACE la
// précédente. Sans l'unicité, les codes s'empileraient et chaque demande
// multiplierait les chances d'en deviner un.
db.auth_otp_codes.createIndex({ phone: 1 }, { unique: true });

// La purge suit la FENÊTRE de cadence (une heure), pas l'échéance du code
// (cinq minutes) : effacer à l'expiration du code remettrait le compteur de
// demandes à zéro toutes les cinq minutes, et le plafond horaire ne
// plafonnerait rien.
db.auth_otp_codes.createIndex({ purge_at: 1 }, { expireAfterSeconds: 0 });
